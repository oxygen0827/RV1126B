package main

// 从 RV1106 yolosrv 移植：CHIFORM 协议 7.2 pose 序列写出器（pose.jsonl.gz）

import (
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

var (
	poseView   = flag.Int("pose-view", 0, "CHIFORM header view")
	poseFps    = flag.Float64("pose-fps", 25, "CHIFORM header fps")
	poseModel  = flag.String("pose-model-version", "yolov8s-pose-416-rv1126b@1.0", "CHIFORM header model_version")
	poseKpTh   = flag.Float64("pose-kp-th", 0.3, "关键点置信度阈值")
	poseRotate = flag.Int("pose-rotate", 0, "CHIFORM header rotate_deg（0/90/180/270）")
	poseOrigin = flag.String("pose-data-origin", "real", "CHIFORM header data_origin（real/simulated）")
)

var poseSeq *poseSeqWriter

type poseCoordJSON struct {
	XY string      `json:"xy"`
	Z  interface{} `json:"z"` // 恒 null（2D 姿态无 z）
}

type poseHeaderJSON struct {
	Kind            string        `json:"kind"`
	ContractVersion string        `json:"contract_version"`
	View            int           `json:"view"`
	ModelVersion    string        `json:"model_version"`
	Fps             float64       `json:"fps"`
	SrcWidth        int           `json:"src_width"`
	SrcHeight       int           `json:"src_height"`
	RotateDeg       int           `json:"rotate_deg"`
	Coord           poseCoordJSON `json:"coord"`
	DataOrigin      string        `json:"data_origin"`
}

type poseTargetJSON struct {
	Id   int                `json:"id"`
	Lock string             `json:"lock"`
	Bbox [4]float32         `json:"bbox"` // [x,y,w,h] 左上角原点，归一化 [0,1]
	Kps  [17][4]interface{} `json:"kps"`  // 检出: [x,y,null,score]；未检出: [null,null,null,null]
}

type poseFrameJSON struct {
	Kind    string           `json:"kind"`
	TMs     int64            `json:"t_ms"`
	Frame   int              `json:"frame"`
	Targets []poseTargetJSON `json:"targets"` // 空画面为空数组，该行不省略
}

// poseSeqWriter CHIFORM pose 序列写出器
type poseSeqWriter struct {
	f       *os.File
	gz      *gzip.Writer // 非 nil 时经 gzip 写出
	w       io.Writer
	t0      time.Time // 首帧写出时刻（懒设置，t_ms 基准）
	lastT   int64     // 上一帧 t_ms（强制单调不减）
	werr    error     // 首个写出错误（close 时返回）
	vw      int
	vh      int
	kpTh    float32
	records int // 已写 frame 行数（A5 record_count 用）
}

func newPoseSeqWriter(path string, vw, vh int) (*poseSeqWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	p := &poseSeqWriter{f: f, w: f, vw: vw, vh: vh, kpTh: float32(*poseKpTh)}
	if strings.HasSuffix(path, ".gz") {
		p.gz = gzip.NewWriter(f)
		p.w = p.gz
	}
	hdr := poseHeaderJSON{
		Kind:            "header",
		ContractVersion: "0.2",
		View:            *poseView,
		ModelVersion:    *poseModel,
		Fps:             *poseFps,
		SrcWidth:        vw,
		SrcHeight:       vh,
		RotateDeg:       *poseRotate,
		Coord:           poseCoordJSON{XY: "normalized[0,1]"},
		DataOrigin:      *poseOrigin,
	}
	lb, _ := json.Marshal(hdr)
	p.put(lb)
	p.put([]byte("\n"))
	if p.werr != nil {
		f.Close()
		return nil, p.werr
	}
	return p, nil
}

// put 写一行，记录首个错误（后续写静默跳过，close 统一上报）
func (p *poseSeqWriter) put(b []byte) {
	if p.werr != nil {
		return
	}
	if _, err := p.w.Write(b); err != nil {
		p.werr = err
	}
}

// writeFrame 写一帧：单人场景只保留置信度最高的人；该帧无人也写空 targets 行
func (p *poseSeqWriter) writeFrame(frameIdx int, keep []detection, scale float32, padX, padY, srcW, srcH int) {
	if p.t0.IsZero() {
		p.t0 = time.Now() // 懒设置：首帧 t_ms=0，规避协议首帧容差（≤ 2×1000/fps）风险
	}
	tMs := time.Since(p.t0).Milliseconds()
	if tMs < p.lastT {
		tMs = p.lastT // 全序列单调不减
	}
	p.lastT = tMs
	fj := poseFrameJSON{Kind: "frame", TMs: tMs, Frame: frameIdx, Targets: make([]poseTargetJSON, 0, 1)}
	if len(keep) > 0 {
		best := 0
		for i := 1; i < len(keep); i++ {
			if keep[i].conf > keep[best].conf {
				best = i
			}
		}
		d := keep[best]
		// The model transform belongs to the camera, not the smaller encoded video.
		fw, fh := float32(srcW), float32(srcH)
		// 去 letterbox（与 -jsonl 相同：(v-pad)/scale 得 vw×vh 画面像素），再除以 vw/vh 归一化；
		// clamp01 单调，保证 w,h >= 0 且 bbox 四值均在 [0,1]
		bx1 := clamp01((d.x1 - float32(padX)) / scale / fw)
		by1 := clamp01((d.y1 - float32(padY)) / scale / fh)
		bx2 := clamp01((d.x2 - float32(padX)) / scale / fw)
		by2 := clamp01((d.y2 - float32(padY)) / scale / fh)
		tg := poseTargetJSON{Id: 1, Lock: "locked", Bbox: [4]float32{bx1, by1, bx2 - bx1, by2 - by1}}
		for k := 0; k < 17; k++ {
			s := d.kp[k][2]
			if s >= p.kpTh {
				tg.Kps[k] = [4]interface{}{
					clamp01((d.kp[k][0] - float32(padX)) / scale / fw),
					clamp01((d.kp[k][1] - float32(padY)) / scale / fh),
					nil, s,
				}
			} else {
				tg.Kps[k] = [4]interface{}{nil, nil, nil, nil}
			}
		}
		fj.Targets = append(fj.Targets, tg)
	}
	lb, _ := json.Marshal(fj)
	p.put(lb)
	p.put([]byte("\n"))
	p.records++
}

// close 关闭输出（gzip footer 落盘），返回写/关过程中的首个错误
func (p *poseSeqWriter) close() error {
	var err error
	if p.gz != nil {
		err = p.gz.Close()
	}
	if cerr := p.f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = p.werr
	}
	return err
}

// closePoseSeq 关闭 CHIFORM 输出并上报错误
func closePoseSeq() {
	if poseSeq != nil {
		if err := poseSeq.close(); err != nil {
			fmt.Println("pose-seq close:", err)
		}
		poseSeq = nil
	}
}
