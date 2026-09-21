package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// 演示会话录制与云上传触发（M0 触摸闭环）：
//
//	start → recStart():  重开 pose.jsonl.gz + 记会话 t0 墙钟；raw 视频取「屏显快照」
//	stop  → recStop():   关文件 → 写 session_meta.json（A3/A5 所需全部元数据）
//	send  → launchUpload: 后台 exec demo_upload.sh（转码 + A3→PUT→A5→A6 → report.json）
//	成功/失败 → uploader 落盘 status_ok/status_err + report_verdict.txt → 屏显 + TTS 播报
//
// 视频来源：健身应用下由 writeBGRFrame 提供摄像头 BGR 帧，经 FIFO 实时喂给
// 厂商 MPP 编码器（mpi_enc_test）写成 H.264，停录时关闭 FIFO 触发 EOS；
// 上传/本地保存阶段只做 remux，不再落盘数百 MB 的 raw BGR。
// DRM 直显模式仍走 snapFrame → raw 文件（仅历史脚本使用）。
// 协议口径：t0 = 视频首帧采集时刻 ≈ 开窗时刻。

// 这些路径在主机测试中会被替换为临时目录，因此用变量而非常量。
var (
	demoUploadDir  = "/root/yolov8s-pose/upload"
	demoUploadMeta = demoUploadDir + "/session_meta.json"
	demoRawVideo   = demoUploadDir + "/video_raw.bgr"
	demoLiveH264   = demoUploadDir + "/video.h264"
	demoLiveFifo   = "/tmp/fitness_video.fifo"
	demoLiveLog    = "/tmp/fitness_encode.log"
	demoRawPose    = demoUploadDir + "/pose.jsonl.gz"
	demoReport     = demoUploadDir + "/report.json"
	demoVerdict    = demoUploadDir + "/report_verdict.txt"
	demoUploaderSh = "/root/yolov8s-pose/demo_upload.sh"
	demoStatusOK   = demoUploadDir + "/status_ok.txt"
	demoStatusErr  = demoUploadDir + "/status_err.txt"
	demoPreview    = "/tmp/fitness_preview.bgr"
	demoLocalDir   = "/userdata/fitness/sessions"
	demoLocalOK    = "/tmp/fitness_local_ok.txt"
	demoLocalErr   = "/tmp/fitness_local_err.txt"
	demoRecording  = "/tmp/fitness_recording.txt"
	demoRecordErr  = "/tmp/fitness_record_err.txt"
)

const (
	demoSnapEvery       = 1 // 保留每个显示帧；实时编码下不再受 eMMC 写入限制
	demoPreviewEvery    = 2 // 预览独立于录像抽帧，约 12.5fps（25fps 推理输入）
	demoEncodeBps       = "1200000"
	demoGOPSeconds      = 2
	demoEncodeWait      = 15 * time.Second // 等待编码器 EOS 收尾的上限
	demoEncodeWriteWait = 2 * time.Second
)

// demoEncoderBin 是板端 Rockchip MPP 编码工具；测试用确定性替身覆盖。
var demoEncoderBin = func() string {
	if p := os.Getenv("FITNESS_MPP_ENC_BIN"); p != "" {
		return p
	}
	return "/oem/usr/bin/mpi_enc_test"
}()

// demoMeta JSON（demo_upload.sh 读它做 A3/A5）
type demoMeta struct {
	ClientRequestID string  `json:"client_request_id"`
	Movement        string  `json:"movement"`
	Action          string  `json:"action"`
	View            int     `json:"view"`
	FPS             float64 `json:"fps"`
	Width           int     `json:"width"`
	Height          int     `json:"height"`
	StartedAtMs     int64   `json:"started_at_ms"`
	EndedAtMs       int64   `json:"ended_at_ms"`
	RecordCount     int64   `json:"record_count"`
	VideoFile       string  `json:"video_file"`
	VideoEncoded    bool    `json:"video_encoded,omitempty"`
	VideoFPS        float64 `json:"video_fps,omitempty"`
	VideoFrameCount int     `json:"video_frame_count,omitempty"`
	VideoGOP        int     `json:"video_gop,omitempty"`
	PoseFile        string  `json:"pose_file"`
	FirmwareVersion string  `json:"firmware_version"`
	ModelVersion    string  `json:"model_version"`
}

// demoRec 会话录制（decode/drm show 协程间经 active 标志与原始帧计数协调）
type demoRec struct {
	mu        sync.Mutex // protects active/files while DRM and decode goroutines overlap
	active    bool
	saving    bool
	uploading bool
	recordErr error
	operation *os.File
	rawF      *os.File // legacy DRM raw recording (snapFrame path)
	encW      *os.File // FIFO write end feeding the live MPP encoder
	encCmd    *exec.Cmd
	live      bool // live MPP encoding active for this session
	poseW     *poseSeqWriter
	first     time.Time
	snapN     int // 帧计数（抽帧节拍）
	previewN  int // 预览发布节拍（不与录像抽帧共享）
	vframe    int // 已拍视频帧数（meta/时长换算）
	vw        int // 源画面宽（meta 用）
	vh        int // 源画面高
	fps       float64
	videoFps  float64 // 实测采集帧率（编码视频 remux 用）
	videoGOP  int
}

var demoRecS = &demoRec{}

// flock is shared with the shell uploader, including across backend restarts.
func lockSessionFiles(dir string) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(dir, ".session.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("session files busy: %w", err)
	}
	return f, nil
}

func uuidHex() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// reapStaleEncoders kills encoder processes left behind when the backend was
// killed mid-recording. An orphan keeps the unlinked FIFO open and blocks on
// it forever while holding MPP/DRM buffers. Linux-only: a missing /proc (host
// tests) is a no-op.
func reapStaleEncoders() {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err != nil || len(cmdline) == 0 {
			continue
		}
		args := strings.ReplaceAll(string(cmdline), "\x00", " ")
		if strings.Contains(args, "mpi_enc_test") && strings.Contains(args, demoLiveFifo) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			fmt.Println("demo: reaped stale encoder pid", pid)
		}
	}
}

// MPP parses integer ratios, not decimal FPS; equal input/output rates retain
// every frame. GOP syntax is mode:length:vi_length, NOT ffmpeg's bare length.
func mppRecordingTiming(fps float64) (fpsArg string, gop int, err error) {
	if math.IsNaN(fps) || math.IsInf(fps, 0) || fps < 0.001 || fps > 1000 {
		return "", 0, fmt.Errorf("invalid recording fps %v", fps)
	}
	numerator := int(math.Round(fps * 1000))
	gop = max(1, int(math.Round(float64(numerator)*demoGOPSeconds/1000)))
	divisor, remainder := numerator, 1000
	for remainder != 0 {
		divisor, remainder = remainder, divisor%remainder
	}
	rate := fmt.Sprintf("%d:%d:0", numerator/divisor, 1000/divisor)
	return rate + "/" + rate, gop, nil
}

// startLiveEncoder launches the vendor MPP encoder against a FIFO so frames are
// compressed while they are captured instead of being written to eMMC raw.
// The caller must feed the returned file and later call stopLiveEncoder.
func startLiveEncoder(vw, vh int, fps float64) (*os.File, *exec.Cmd, error) {
	fpsArg, gop, err := mppRecordingTiming(fps)
	if err != nil {
		return nil, nil, err
	}
	reapStaleEncoders()
	if err := syscall.Mkfifo(demoLiveFifo, 0o600); err != nil && !os.IsExist(err) {
		return nil, nil, fmt.Errorf("live encode fifo: %w", err)
	}
	logF, err := os.OpenFile(demoLiveLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("live encode log: %w", err)
	}
	ldPath := "/oem/usr/lib:/oem/lib"
	if cur := os.Getenv("LD_LIBRARY_PATH"); cur != "" {
		ldPath += ":" + cur
	}
	cmd := exec.Command(demoEncoderBin,
		"-i", demoLiveFifo, "-o", demoLiveH264,
		"-w", strconv.Itoa(vw), "-h", strconv.Itoa(vh),
		"-f", "65543", "-t", "7", // BGR888 -> H.264
		"-fps", fpsArg,
		"-g", fmt.Sprintf("0:%d:0", gop), // normal IPPP, periodic IDR
		"-bps", demoEncodeBps,
		"-n", "0", // 0 = 编码到输入 EOF（写端关闭触发 EOS 收尾）
	)
	cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH="+ldPath)
	cmd.Stdout, cmd.Stderr = logF, logF
	if err := cmd.Start(); err != nil {
		logF.Close()
		return nil, nil, fmt.Errorf("live encode start: %w", err)
	}
	logF.Close()
	encW, err := openFifoWriter(demoLiveFifo, 5*time.Second)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, nil, fmt.Errorf("live encode fifo: %w", err)
	}
	return encW, cmd, nil
}

// openFifoWriter opens the FIFO without blocking forever when the encoder
// process failed to start. Keep O_NONBLOCK so Go's poller can enforce write
// deadlines; File.Write still waits for the complete frame without dropping it.
func openFifoWriter(path string, timeout time.Duration) (*os.File, error) {
	deadline := time.Now().Add(timeout)
	for {
		fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			return os.NewFile(uintptr(fd), path), nil
		}
		if err != syscall.ENXIO && err != syscall.EAGAIN {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("encoder did not open %s within %s", path, timeout)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// stopLiveEncoder closes the FIFO so the encoder sees EOF, flushes its EOS
// packet and exits. The wait is bounded so a stuck vendor process cannot hang
// the recording loop forever.
func stopLiveEncoder(encW *os.File, cmd *exec.Cmd, timeout time.Duration) error {
	if encW != nil {
		_ = encW.Close()
	}
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		<-done
		return fmt.Errorf("encoder did not exit within %s", timeout)
	}
}

// recStart 开窗时调用（decode 协程）：重建序列文件 + 记录会话 t0 + 清状态文件。
// live=true 时启动 MPP 实时编码（fitness 应用）；live=false 保留 DRM raw 路径。
func (r *demoRec) recStart(vw, vh int, fps float64, live bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active {
		return nil
	}
	if r.saving || r.uploading {
		return fmt.Errorf("previous session is still being saved or uploaded")
	}
	if err := os.MkdirAll(demoUploadDir, 0o755); err != nil {
		return err
	}
	operation, err := lockSessionFiles(demoUploadDir)
	if err != nil {
		return err
	}
	defer func() {
		if !r.active {
			operation.Close()
		}
	}()
	for _, path := range []string{demoRawVideo, demoLiveH264, demoLiveFifo, demoLiveLog,
		demoRawPose, demoStatusOK, demoStatusErr, demoReport, demoVerdict,
		demoLocalOK, demoLocalErr, demoUploadMeta, demoRecordErr} {
		os.Remove(path)
	}
	r.recordErr = nil

	pw, err := newPoseSeqWriter(demoRawPose, vw, vh)
	if err != nil {
		return fmt.Errorf("pose seq: %w", err)
	}
	var rf, encW *os.File
	var encCmd *exec.Cmd
	if live {
		encW, encCmd, err = startLiveEncoder(vw, vh, fps)
		if err != nil {
			pw.close()
			return err
		}
	} else {
		rf, err = os.Create(demoRawVideo)
		if err != nil {
			pw.close()
			return fmt.Errorf("raw video: %w", err)
		}
	}
	r.poseW, r.rawF, r.encW, r.encCmd, r.live = pw, rf, encW, encCmd, live
	r.active = true
	r.first, r.snapN, r.previewN, r.vframe = time.Now(), 0, 0, 0
	r.vw, r.vh, r.fps, r.videoFps = vw, vh, fps, 0
	r.videoGOP = 0
	if live {
		_, r.videoGOP, _ = mppRecordingTiming(fps) // validated before encoder start
	}
	if err := os.WriteFile(demoRecording, []byte("recording"), 0o644); err != nil {
		r.active = false
		pw.close()
		if rf != nil {
			rf.Close()
		}
		if encCmd != nil {
			_ = stopLiveEncoder(encW, encCmd, 5*time.Second)
		}
		r.poseW, r.rawF, r.encW, r.encCmd = nil, nil, nil, nil
		return err
	}
	r.operation = operation
	// A new session must be allowed to announce its own report.  This flag is
	// intentionally process-local and is only read/written by the decode loop.
	verdictSpoken = false
	if live {
		fmt.Println("demo: recording started", demoLiveH264)
	} else {
		fmt.Println("demo: recording started", demoRawVideo)
	}
	return nil
}

// writePoseFrame 开窗期每帧调用（decode 协程）：写协议 7.2 序列行。
func (r *demoRec) writePoseFrame(frameIdx int, keep []detection, scale float32, padX, padY, srcW, srcH int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.active || r.poseW == nil {
		return
	}
	r.poseW.writeFrame(frameIdx, keep, scale, padX, padY, srcW, srcH)
}

// snapFrame DRM show 协程调用：每 demoSnapEvery 帧把 back buffer 图像区
// （720×480 显示区，BGRX 内存序）抽帧转 BGR 写入 rawF。
func (r *demoRec) snapFrame(mapped []byte, pitch, x0, y0, w, h int, flipN int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.active || r.rawF == nil {
		return
	}
	r.snapN++
	if r.snapN%demoSnapEvery != 0 {
		return
	}
	// 图像区坐标（letterbox 后显示区），行 stride=pitch 字节
	buf := make([]byte, w*h*3)
	for y := 0; y < h; y++ {
		row := (y0 + y) * pitch
		for x := 0; x < w; x++ {
			o := row + (x0+x)*4
			// 内存序 B,G,R,X
			buf[(y*w+x)*3+0] = mapped[o+0]
			buf[(y*w+x)*3+1] = mapped[o+1]
			buf[(y*w+x)*3+2] = mapped[o+2]
		}
	}
	if _, err := r.rawF.Write(buf); err != nil && r.recordErr == nil {
		r.recordErr = err
	}
	r.vframe++
}

// writeBGRFrame records camera pixels when the frontend owns the display.
// This keeps the recording path independent from DRM/LVGL framebuffer output.
func (r *demoRec) writeBGRFrame(bgr []byte, w, h int, keep []detection, scale float32, padX, padY int, srcW, srcH int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(bgr) < w*h*3 {
		return
	}
	r.snapN++
	r.previewN++
	// Publish a preview independently from the raw recording.
	if r.previewN%demoPreviewEvery == 0 {
		preview := make([]byte, w*h*3)
		copy(preview, bgr[:w*h*3])
		mapX := func(v float32) int {
			return int((v - float32(padX)) / scale * float32(w) / float32(srcW))
		}
		mapY := func(v float32) int {
			return int((v - float32(padY)) / scale * float32(h) / float32(srcH))
		}
		for _, d := range keep {
			for _, pair := range skeletonPairs {
				a, b := d.kp[pair[0]], d.kp[pair[1]]
				if a[2] > 0.5 && b[2] > 0.5 {
					drawLineBGR(preview, w, h, mapX(a[0]), mapY(a[1]), mapX(b[0]), mapY(b[1]), 60, 60, 255)
				}
			}
			for _, kp := range d.kp {
				if kp[2] <= 0.5 {
					continue
				}
				cx, cy := mapX(kp[0]), mapY(kp[1])
				for dy := -2; dy <= 2; dy++ {
					for dx := -2; dx <= 2; dx++ {
						putPxBGR(preview, w, h, cx+dx, cy+dy, 0, 255, 255)
					}
				}
			}
		}
		previewTmp, err := os.CreateTemp("/tmp", ".fitness_preview-")
		if err == nil {
			_, err = previewTmp.Write(preview)
			if cerr := previewTmp.Close(); err == nil {
				err = cerr
			}
			if err == nil {
				err = os.Rename(previewTmp.Name(), demoPreview)
			}
			if err != nil {
				_ = os.Remove(previewTmp.Name())
			}
		}
	}
	// LVGL owns the panel in the fitness launcher. Publish a complete latest
	// frame through an atomic rename so the UI never reads a partial image.
	if r.snapN%demoSnapEvery == 0 && r.active {
		switch {
		case r.encW != nil:
			// Bound a stalled encoder without skipping frames or falling back
			// to raw storage. A partial write invalidates the whole recording.
			err := r.encW.SetWriteDeadline(time.Now().Add(demoEncodeWriteWait))
			written := 0
			if err == nil {
				written, err = r.encW.Write(bgr[:w*h*3])
			}
			if err == nil && written != w*h*3 {
				err = io.ErrShortWrite
			}
			if err != nil {
				r.recordErr = fmt.Errorf("live encode: %w", err)
				_ = r.encW.Close()
				r.encW = nil
				if r.encCmd != nil && r.encCmd.Process != nil {
					_ = r.encCmd.Process.Kill()
				}
				return
			}
			r.vframe++
		case r.rawF != nil:
			if _, err := r.rawF.Write(bgr[:w*h*3]); err != nil && r.recordErr == nil {
				r.recordErr = err
			}
			r.vframe++
		}
	}
}

// encodeLocalVideo produces an MP4 for local archiving. The source may be a
// live-encoded H.264 stream (remux only), an MP4 (copy), or legacy raw BGR
// (MPP/ffmpeg encode).
func encodeLocalVideo(videoPath, outputPath string, w, h int, fps float64) error {
	if w <= 0 || h <= 0 || w > 4096 || h > 4096 {
		return fmt.Errorf("invalid raw video dimensions %dx%d", w, h)
	}
	if fps <= 0 {
		fps = 25
	}
	lower := strings.ToLower(videoPath)
	raw := !strings.HasSuffix(lower, ".h264") && !strings.HasSuffix(lower, ".264") && !strings.HasSuffix(lower, ".mp4")
	if raw {
		info, err := os.Stat(videoPath)
		if err != nil {
			return fmt.Errorf("raw video unavailable: %w", err)
		}
		frameBytes := int64(w) * int64(h) * 3
		if info.Size() < frameBytes || info.Size()%frameBytes != 0 {
			return fmt.Errorf("raw video size %d is not a whole %dx%d BGR frame sequence", info.Size(), w, h)
		}
	}
	// Reuse the board uploader's MPP-first encoder when available.  It also
	// remuxes pre-encoded H.264, so local saves share the cloud path's logic.
	if _, err := os.Stat(demoUploaderSh); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "sh", demoUploaderSh)
		cmd.Env = append(os.Environ(),
			"UPLOAD_DIR="+filepath.Dir(videoPath),
			"FITNESS_ENCODE_ONLY=1",
			"FITNESS_ENCODE_OUTPUT="+outputPath,
			"FITNESS_UPLOAD_LOCKED=1",
		)
		if out, runErr := cmd.CombinedOutput(); runErr == nil {
			if encoded, statErr := os.Stat(outputPath); statErr == nil && encoded.Size() > 0 {
				return nil
			}
			if !raw {
				return fmt.Errorf("video remux produced no output")
			}
		} else if ctx.Err() != nil {
			return fmt.Errorf("video encode timed out after 4 minutes")
		} else if !raw {
			// Do not bypass the uploader's keyframe/frame-count validation.
			return fmt.Errorf("video remux/validation failed: %v %s", runErr, strings.TrimSpace(string(out)))
		} else if len(out) > 0 {
			fmt.Printf("local MPP encode unavailable: %s\n", strings.TrimSpace(string(out)))
		}
	}
	if !raw {
		if strings.HasSuffix(lower, ".mp4") {
			return copyPath(videoPath, outputPath)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
		defer cancel()
		args := []string{"-y", "-loglevel", "error", "-r", strconv.FormatFloat(fps, 'f', 3, 64),
			"-i", videoPath, "-c:v", "copy", "-an", "-movflags", "+faststart", outputPath}
		if out, err := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput(); err != nil {
			_ = os.Remove(outputPath)
			return fmt.Errorf("video remux failed: %v %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	_, gop, err := mppRecordingTiming(fps)
	if err != nil {
		return err
	}
	common := []string{"-y", "-loglevel", "error", "-framerate", strconv.FormatFloat(fps, 'f', 3, 64), "-f", "rawvideo", "-pix_fmt", "bgr24",
		"-s", fmt.Sprintf("%dx%d", w, h), "-i", videoPath}
	encoders := [][]string{
		{"-vf", "scale=640:480,format=nv12", "-c:v", "h264_v4l2m2m", "-b:v", "1200k"},
		{"-vf", "scale=640:480", "-c:v", "libx264", "-preset", "ultrafast", "-crf", "30", "-pix_fmt", "yuv420p"},
		{"-vf", "scale=640:480", "-c:v", "mpeg4", "-q:v", "7", "-pix_fmt", "yuv420p"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	var failures []string
	for _, encoder := range encoders {
		args := append(append(append([]string{}, common...), encoder...), "-g", strconv.Itoa(gop), "-an", "-movflags", "+faststart", outputPath)
		out, err := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput()
		if err == nil {
			if info, statErr := os.Stat(outputPath); statErr == nil && info.Size() > 0 {
				return nil
			}
		}
		msg := strings.TrimSpace(string(out))
		if len(msg) > 240 {
			msg = msg[len(msg)-240:]
		}
		failures = append(failures, fmt.Sprintf("%v: %v %s", encoder, err, msg))
		_ = os.Remove(outputPath)
		if ctx.Err() != nil {
			return fmt.Errorf("video encode timed out after 100 seconds")
		}
	}
	return fmt.Errorf("video encode failed: %s", strings.Join(failures, "; "))
}

func copyPath(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func pruneLocalSessions(root string, max int) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	var dirs []string
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".saving-") {
			dirs = append(dirs, entry.Name())
		}
	}
	sort.Slice(dirs, func(i, j int) bool {
		a, _ := strconv.ParseInt(strings.SplitN(dirs[i], "-", 2)[0], 10, 64)
		b, _ := strconv.ParseInt(strings.SplitN(dirs[j], "-", 2)[0], 10, 64)
		if a == b {
			return dirs[i] > dirs[j]
		}
		return a > b
	})
	if len(dirs) <= max {
		return nil
	}
	for _, name := range dirs[max:] {
		if err := os.RemoveAll(filepath.Join(root, name)); err != nil {
			return err
		}
	}
	return nil
}

// launchLocalSave reserves the completed session before returning, then runs
// the relatively slow ffmpeg/copy work outside the pose decode loop.
func (r *demoRec) launchLocalSave() error {
	r.mu.Lock()
	if r.active {
		r.mu.Unlock()
		return fmt.Errorf("session is still recording")
	}
	if r.saving {
		r.mu.Unlock()
		return fmt.Errorf("session save is already running")
	}
	if r.uploading {
		r.mu.Unlock()
		return fmt.Errorf("session upload is already running")
	}
	operation, err := lockSessionFiles(demoUploadDir)
	if err != nil {
		r.mu.Unlock()
		return err
	}
	r.saving = true
	r.mu.Unlock()
	go func() {
		err := archiveLocalSession(demoUploadDir, demoLocalDir)
		r.mu.Lock()
		defer r.mu.Unlock()
		operation.Close()
		r.saving = false
		if err != nil {
			fmt.Println("fitness local save:", err)
			_ = os.WriteFile(demoLocalErr, []byte(err.Error()), 0o644)
			return
		}
		_ = os.WriteFile(demoLocalOK, []byte("saved"), 0o644)
		fmt.Println("fitness local save: archived session")
	}()
	return nil
}

// archiveLocalSession archives one completed session on eMMC and keeps the newest two.
func archiveLocalSession(uploadDir, localDir string) error {
	b, err := os.ReadFile(filepath.Join(uploadDir, "session_meta.json"))
	if err != nil {
		return fmt.Errorf("session metadata unavailable: %w", err)
	}
	var meta demoMeta
	if err := json.Unmarshal(b, &meta); err != nil {
		return fmt.Errorf("invalid session metadata: %w", err)
	}
	if meta.ClientRequestID == "" {
		return fmt.Errorf("session metadata has no id")
	}
	for _, ch := range meta.ClientRequestID {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')) {
			return fmt.Errorf("session metadata has invalid id")
		}
	}
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		return err
	}
	id := meta.ClientRequestID
	// Offline boards may boot without a valid RTC or have their clock corrected
	// backwards. Preserve save order so a newly saved video cannot prune itself.
	archiveOrder := meta.StartedAtMs
	entries, err := os.ReadDir(localDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".saving-") {
			continue
		}
		if strings.HasSuffix(entry.Name(), "-"+id) {
			if _, err := os.Stat(filepath.Join(localDir, entry.Name(), "session_meta.json")); err == nil {
				return pruneLocalSessions(localDir, 2)
			}
		}
		order, err := strconv.ParseInt(strings.SplitN(entry.Name(), "-", 2)[0], 10, 64)
		if err == nil && order >= archiveOrder {
			archiveOrder = order + 1
		}
	}
	finalDir := filepath.Join(localDir, fmt.Sprintf("%d-%s", archiveOrder, id))
	tmpDir, err := os.MkdirTemp(localDir, ".saving-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	videoPath := filepath.Join(tmpDir, "video.mp4")
	source := meta.VideoFile
	if source == "" {
		source = filepath.Join(uploadDir, "video_raw.bgr")
	}
	if _, err := os.Stat(source); err != nil {
		source = filepath.Join(uploadDir, "video_raw.bgr")
	}
	if err := encodeLocalVideo(source, videoPath, meta.Width, meta.Height, meta.VideoFPS); err != nil {
		return err
	}
	for _, item := range []struct{ src, name string }{
		{filepath.Join(uploadDir, "pose.jsonl.gz"), "pose.jsonl.gz"},
	} {
		if err := copyPath(item.src, filepath.Join(tmpDir, item.name)); err != nil {
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(uploadDir, "report.json")); err == nil {
		_ = copyPath(filepath.Join(uploadDir, "report.json"), filepath.Join(tmpDir, "report.json"))
	}
	meta.VideoFile = filepath.Join(finalDir, "video.mp4")
	meta.PoseFile = filepath.Join(finalDir, "pose.jsonl.gz")
	mb, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(filepath.Join(tmpDir, "session_meta.json"), mb, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpDir, finalDir); err != nil {
		return err
	}
	return pruneLocalSessions(localDir, 2)
}

// recStop 关窗时调用（decode 协程）：关文件 + 写 meta（含序列行数 / 视频时长）。
func (r *demoRec) recStop() {
	r.mu.Lock()
	if !r.active {
		r.mu.Unlock()
		return
	}
	// EOS flushing is not capture time; including it slows remuxed playback.
	ended := time.Now()
	r.active = false
	operation := r.operation
	r.operation = nil
	defer func() {
		if operation != nil {
			operation.Close()
		}
	}()
	defer os.Remove(demoRecording)
	recCount := int64(0)
	if r.poseW != nil {
		recCount = int64(r.poseW.records)
		if err := r.poseW.close(); err != nil {
			fmt.Println("demo: pose seq close:", err)
			r.recordErr = err
		}
		r.poseW = nil
	}
	if r.encCmd != nil || r.encW != nil {
		if err := stopLiveEncoder(r.encW, r.encCmd, demoEncodeWait); err != nil {
			fmt.Println("demo: live encode stop:", err)
			r.recordErr = err
		}
		r.encW, r.encCmd = nil, nil
		os.Remove(demoLiveFifo)
	}
	if r.rawF != nil {
		if err := r.rawF.Close(); err != nil {
			r.recordErr = err
		}
		r.rawF = nil
	}
	elapsed := ended.Sub(r.first).Seconds()
	if r.live {
		if fi, err := os.Stat(demoLiveH264); err != nil || fi.Size() == 0 {
			r.recordErr = fmt.Errorf("live encode produced no video")
		} else if elapsed >= 1 {
			// 用实测平均帧率 remux，保证 MP4 时长与墙钟一致（meta.fps 仍为协议帧率）。
			r.videoFps = float64(r.vframe) / elapsed
		} else {
			r.videoFps = r.fps
		}
	}
	if r.vframe == 0 || recCount == 0 {
		r.recordErr = fmt.Errorf("recording contains no video or pose frames")
	}
	if r.recordErr != nil {
		_ = os.WriteFile(demoRecordErr, []byte(r.recordErr.Error()), 0o644)
		r.mu.Unlock()
		return
	}
	videoFile := demoRawVideo
	if r.live {
		videoFile = demoLiveH264
	}
	meta := demoMeta{
		ClientRequestID: uuidHex(),
		Movement:        *movementFlag,
		Action:          movementKeyToAction[*movementFlag],
		View:            0,
		FPS:             r.fps,
		Width:           r.vw,
		Height:          r.vh,
		StartedAtMs:     r.first.UnixMilli(),
		EndedAtMs:       ended.UnixMilli(),
		RecordCount:     recCount,
		VideoFile:       videoFile,
		VideoEncoded:    r.live,
		VideoFPS:        r.videoFps,
		VideoFrameCount: r.vframe,
		VideoGOP:        r.videoGOP,
		PoseFile:        demoRawPose,
		FirmwareVersion: "demo-0.1",
		ModelVersion:    *poseModel,
	}
	b, _ := json.MarshalIndent(meta, "", "  ")
	// Publish metadata with a same-filesystem rename. The USB bridge polls for
	// this path and must never observe a partially written JSON document.
	tmp, err := os.CreateTemp(demoUploadDir, ".session_meta.json.tmp-")
	if err == nil {
		if err = tmp.Chmod(0o644); err == nil {
			_, err = tmp.Write(b)
		}
		if err == nil {
			err = tmp.Sync()
		}
		if cerr := tmp.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			err = os.Rename(tmp.Name(), demoUploadMeta)
		}
		if removeErr := os.Remove(tmp.Name()); err == nil && removeErr != nil && !os.IsNotExist(removeErr) {
			err = removeErr
		}
	}
	if err != nil {
		fmt.Println("demo: write meta:", err)
		_ = os.WriteFile(demoRecordErr, []byte(err.Error()), 0o644)
		r.mu.Unlock()
		return
	}
	fmt.Printf("demo: recording stopped (%d poses, %d vframes) meta=%s\n", recCount, r.vframe, demoUploadMeta)
	autoUpload := *autoUploadFlag
	if operation != nil {
		operation.Close()
		operation = nil
	}
	r.mu.Unlock()
	// 无按钮模式：录完即传（屏幕按钮版本的「发送」由用户手动触发）
	if autoUpload {
		if err := r.launchUpload(); err != nil {
			fmt.Println("demo: auto upload:", err)
		}
	}
}

// launchUpload 用户点「发送」：板端后台执行上传脚本。脚本只有在视频和
// 姿态序列 PUT、finalize、报告轮询全部成功后才发布 status_ok.txt。
func (r *demoRec) launchUpload() error {
	if _, err := os.Stat(demoUploadMeta); err != nil {
		return fmt.Errorf("meta 未就绪（先录一组）: %v", err)
	}
	r.mu.Lock()
	if r.active {
		r.mu.Unlock()
		return fmt.Errorf("session is still recording")
	}
	if r.saving {
		r.mu.Unlock()
		return fmt.Errorf("session save is already running")
	}
	if r.uploading {
		r.mu.Unlock()
		return fmt.Errorf("session upload is already running")
	}
	r.uploading = true
	r.mu.Unlock()
	clearUploading := func() {
		r.mu.Lock()
		r.uploading = false
		r.mu.Unlock()
	}
	os.Remove(demoStatusOK)
	os.Remove(demoStatusErr)
	logFile, err := os.OpenFile(demoUploadDir+"/upload-launch.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		clearUploading()
		return fmt.Errorf("upload log: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	cmd := exec.CommandContext(ctx, "/bin/sh", demoUploaderSh)
	appDir := filepath.Dir(demoUploaderSh)
	cmd.Env = append(os.Environ(),
		"UPLOAD_DIR="+demoUploadDir,
		"APP_DIR="+appDir,
		"UPLOADER_ENV="+filepath.Join(appDir, "uploader.env"),
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		cancel()
		logFile.Close()
		clearUploading()
		return fmt.Errorf("start uploader: %w", err)
	}
	logFile.Close()
	fmt.Printf("demo: upload started on device (pid=%d)\n", cmd.Process.Pid)
	go func() {
		defer cancel()
		err := cmd.Wait()
		r.mu.Lock()
		defer r.mu.Unlock()
		r.uploading = false
		if err != nil {
			if _, statErr := os.Stat(demoStatusErr); os.IsNotExist(statErr) {
				_ = os.WriteFile(demoStatusErr, []byte(err.Error()), 0o644)
			}
			fmt.Println("demo: uploader exited:", err)
		}
	}()
	return nil
}

// pollUploadState 解码循环低频调用（每 ~20 帧）：上传完成/失败 → 更新 UI 状态。
func (r *demoRec) pollUploadState() bool {
	if _, err := os.Stat(demoStatusOK); err == nil {
		demoUISetState(uiDone, -1, "")
		return true
	}
	if _, err := os.Stat(demoStatusErr); err == nil {
		demoUISetState(uiErr, -1, "UPLOAD FAILED")
		return true
	}
	return false
}

// speakVerdict 报告就绪（status_ok 出现）后播报一次总评文本。
var verdictSpoken bool

func speakVerdictOnOK() bool {
	if _, err := os.Stat(demoStatusOK); err != nil {
		return false
	}
	b, err := os.ReadFile(demoVerdict)
	if err != nil || len(b) == 0 {
		return false
	}
	if verdictSpoken {
		return true
	}
	verdictSpoken = true
	text := trimRunecount(string(b), 120)
	if tts != nil {
		tts.speak("report", text)
	}
	return true
}

func trimRunecount(s string, n int) string {
	rs := []rune(s)
	if len(rs) > n {
		return string(rs[:n])
	}
	return s
}
