package main

// 从 RV1106 yolosrv 移植应用的兼容层：
// - 演示模式 flags（LVGL 前端 + 录制/上传链路）
// - 动作键映射（原在 rule_correction.go，Aura 端规则由 edge-stream 提供）
// - DRM HUD 的 UI 状态桩（Aura 端由 LVGL 前端读状态文件，不用进程内 HUD）

import (
	"flag"
	"os"
	"time"
)

var (
	movementFlag       = flag.String("movement", "", "动作 key：air_squat/overhead_squat/strict_pullup/strict_press")
	correctionFpsFlag  = flag.Float64("correction-fps", 25, "推理帧率估计（帧时间戳 fps 换算用）")
	sessionFileFlag    = flag.String("session-file", "", "判定窗控制文件（文件存在期间判定，超时自动关）")
	sessionSecondsFlag = flag.Int("session-seconds", 30, "判定窗时长（秒）")
	demoFlag           = flag.Bool("demo", false, "演示模式：录制/序列/云上传（需 -session-file）")
	autoUploadFlag     = flag.Bool("auto-upload", false, "判定窗结束时自动触发云上传")

	// TTS（本地预录音 aplay / 命令模板 / 智谱）
	ttsCmdFlag     = flag.String("tts-cmd", "", "TTS 命令模板，使用 {text} 占位")
	ttsAudioDirFlag = flag.String("tts-audio-dir", "", "预录音 WAV 目录（ok/knee/hip/elbow/depth.wav，aplay 播放）")
	ttsCooldownFlag = flag.Duration("tts-cooldown", 6*time.Second, "同一纠正规则的语音冷却时间")
	ttsApiKeyFlag   = flag.String("tts-api-key", os.Getenv("ZHIPU_API_KEY"), "智谱 API Key（默认读环境变量 ZHIPU_API_KEY）")
	ttsApiUrlFlag   = flag.String("tts-api-url", "https://open.bigmodel.cn/api/paas/v4/audio/speech", "智谱 TTS 端点")
	ttsModelFlag    = flag.String("tts-model", "cogtts", "智谱 TTS 模型")
	ttsVoiceFlag    = flag.String("tts-voice", "tongtong", "智谱 TTS 音色")
	ttsFormatFlag   = flag.String("tts-format", "wav", "合成音频格式（wav/pcm/mp3）")
	ttsCacheDirFlag = flag.String("tts-cache-dir", "/userdata/fitness/tts", "智谱 TTS 缓存目录")
	ttsInsecureFlag = flag.Bool("tts-insecure", false, "TLS 证书校验放行（仅演示兜底）")
)

var tts *ttsPlayer

// movementKeyToAction 协议 movement_key → 动作名（云端 A3/A5 用）
var movementKeyToAction = map[string]string{
	"air_squat":      "air_squat",
	"overhead_squat": "overhead_squat",
	"strict_pullup":  "strict_pullup",
	"strict_press":   "strict_press",
}

// demoUISetState / demoUITick 在 RV1106 上驱动 DRM HUD；Aura 端 LVGL 前端
// 直接读 /tmp/fitness_recording.txt 等状态文件，这里保留为 no-op 以复用 demo_rec.go。

type uiStateT int

const (
	uiStandy uiStateT = iota
	uiRec
	uiSend
	uiUp
	uiDone
	uiErr
)

func demoUISetState(s uiStateT, rep int, note string) {}

func demoUITick(action func(kind string)) {}

// sessionTick 手动判定窗节拍（nil 时恒放行）。返回 (刚开窗, 是否允许判定)。
func sessionTick() (opened, allow bool) {
	if session == nil {
		return false, true
	}
	return session.tick()
}

// pickMain 单人口径：画面内多人时取 bbox 面积最大者（letterbox 空间面积）。
func pickMain(keep []detection) detection {
	best := keep[0]
	bestA := (best.x2 - best.x1) * (best.y2 - best.y1)
	for _, d := range keep[1:] {
		if a := (d.x2 - d.x1) * (d.y2 - d.y1); a > bestA {
			best, bestA = d, a
		}
	}
	return best
}
