// yolosrv: RV1106 yolov8n-pose 推理服务（Go 版，纯 Go + purego）
// 推理: purego dlopen librknnmrt.so 直调 rknn_api（预分配 mem，支持 dma-buf fd 零拷贝输入）
// 解码: cut9 原始头输出 + DFL softmax + 锚点仿射 + sigmoid + NMS，全部 Go/fp32
// 模式:
//   ./yolosrv_rga [port]                        HTTP 服务（POST /infer?w=W&h=H body=raw BGR）
//   ./yolosrv_rga -video FILE -vw W -vh H ...   原始 BGR 视频文件，流水线跑 FPS（调试/基准）
//   ./yolosrv_rga -v4l2 /dev/videoN -vw W ...   V4L2 摄像头（EXPBUF 零拷贝，已实测）
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

func init() {
	// 忽略 SIGPIPE：向已关闭的 FIFO/管道写数据时不会导致程序崩溃
	signal.Notify(make(chan os.Signal, 1), syscall.SIGPIPE)
}

const (
	modelPathDefault = "/root/pose_deploy/yolov8n-pose/y8pose_cut9_tk160.rknn"
	libPath          = "/oem/usr/lib/librknnmrt.so"
	inputSize = 416
	padColor  = 56
	nmsThres  = 0.45
)

// rknn_tensor_attr（arm32 对齐布局，与 rknn_api.h 一致）
type tensorAttr struct {
	index          uint32
	nDims          uint32
	dims           [16]uint32
	name           [256]byte
	nElems         uint32
	size           uint32
	fmt            int32
	typ            int32
	qntType        int32
	fl             int8
	_              [3]byte
	zp             int32
	scale          float32
	wStride        uint32
	sizeWithStride uint32
	passThrough    uint8
	_              [3]byte
	hStride        uint32
}

type ioNum struct {
	nInput  uint32
	nOutput uint32
}

var (
	rkInit         func(ctx unsafe.Pointer, model unsafe.Pointer, size uint32, flag uint32, ext unsafe.Pointer) int32
	rkQuery        func(ctx uintptr, cmd int32, attr unsafe.Pointer, size uint32) int32
	rkCreateMem    func(ctx uintptr, size uint32) uintptr
	rkCreateMemFd  func(ctx uintptr, fd int32, vir unsafe.Pointer, size uint32) uintptr
	rkSetIoMem     func(ctx uintptr, mem uintptr, attr unsafe.Pointer) int32
	rkRun          func(ctx uintptr, ext unsafe.Pointer) int32
	rkDestroyMem   func(ctx uintptr, mem uintptr) int32
)

var (
	rknnCtx    uintptr
	ctxs       [2]uintptr // 双 ctx 流水线（io mem 启动时绑死，免每帧 rebind）
	dualCtx    bool
	modelBuf   []byte // 常驻，防 GC
	inAttr     tensorAttr
	outTensors map[string]*outTensor
	grids      = [3]int{52, 26, 13}
	nameRe     = regexp.MustCompile(`/(cv\d\.\d)/`)

	inputVirts [2][]byte // NPU 输入内存的 CPU 映射（零拷贝时 == rgaDstBufs[s]）
	inZeroCopy bool      // 输入为 dma-buf fd 直通，RGA 直接写 NPU 输入

	outSets [2]map[string][]byte // 双缓冲输出集（流水线用）
	outMems [2]map[string]uintptr
)

type outTensor struct {
	attr tensorAttr
	c2   int
	hh   int
	ww   int
	grid int
}

func (t *outTensor) get(raw []byte, c, y, x int) float32 {
	var off int
	if int(t.attr.dims[3]) == 0 {
		// flattened [1,C,H*W] with C2=16 inner packing (RV1126B w8a8, fmt=UNDEFINED):
		// physical layout NC1HWC2: C1=ceil(C/16) groups, each element 16 int8 slots.
		const c2w = 16
		c1 := c / c2w
		g := t.grid // square grid W==H
		off = ((c1*g+y)*g+x)*c2w + (c % c2w)
	} else {
		c1 := c / t.c2
		c2 := c % t.c2
		if t.c2 > 1 {
			if t.hh == 1 {
				off = (c1*t.ww+y*t.grid+x)*t.c2 + c2
			} else {
				off = ((c1*t.hh+y)*t.ww+x)*t.c2 + c2
			}
		} else {
			w := t.ww
			h := t.hh
			if int(t.attr.wStride) > int(t.attr.dims[3]) {
				w = int(t.attr.wStride)
			}
			if int(t.attr.hStride) > int(t.attr.dims[2]) {
				h = int(t.attr.hStride)
			}
			off = ((c1*h+y)*w + x)
		}
	}
	if off < 0 || off >= len(raw) {
		if len(raw) == 0 {
			return 0
		}
		if off < 0 {
			off = 0
		} else {
			off = len(raw) - 1
		}
	}
	q := int8(raw[off])
	return float32(int(q)-int(t.attr.zp)) * t.attr.scale
}

func attrName(a *tensorAttr) string {
	n := 0
	for n < 256 && a.name[n] != 0 {
		n++
	}
	return string(a.name[:n])
}

func memVirt(m uintptr, size uint32) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(uintptr(*(*uint64)(unsafe.Pointer(m))))), size)
}

func setup() error {
	h, err := purego.Dlopen(libPath, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return fmt.Errorf("dlopen: %w", err)
	}
	purego.RegisterLibFunc(&rkInit, h, "rknn_init")
	purego.RegisterLibFunc(&rkQuery, h, "rknn_query")
	purego.RegisterLibFunc(&rkCreateMem, h, "rknn_create_mem")
	purego.RegisterLibFunc(&rkCreateMemFd, h, "rknn_create_mem_from_fd")
	purego.RegisterLibFunc(&rkSetIoMem, h, "rknn_set_io_mem")
	purego.RegisterLibFunc(&rkRun, h, "rknn_run")
	purego.RegisterLibFunc(&rkDestroyMem, h, "rknn_destroy_mem")

	modelBuf, err = os.ReadFile(*modelFlag)
	if err != nil {
		return err
	}
	r := rkInit(unsafe.Pointer(&rknnCtx), unsafe.Pointer(&modelBuf[0]), uint32(len(modelBuf)), 0, nil)
	if r != 0 {
		return fmt.Errorf("rknn_init=%d", r)
	}
	var io ioNum
	if rkQuery(rknnCtx, 0, unsafe.Pointer(&io), 8) != 0 {
		return fmt.Errorf("query io num fail")
	}
	inAttr.index = 0
	if rkQuery(rknnCtx, 1, unsafe.Pointer(&inAttr), uint32(unsafe.Sizeof(inAttr))) != 0 {
		return fmt.Errorf("query input attr fail")
	}
	outTensors = map[string]*outTensor{}
	for i := uint32(0); i < io.nOutput; i++ {
		var a tensorAttr
		a.index = i
		if rkQuery(rknnCtx, 9, unsafe.Pointer(&a), uint32(unsafe.Sizeof(a))) != 0 {
			return fmt.Errorf("query out attr %d fail", i)
		}
		m := nameRe.FindStringSubmatch(attrName(&a))
		if m == nil {
			continue
		}
		nm := m[1]
		c2v := int(a.dims[4])
	if c2v == 0 {
		c2v = 1 // nDims=4 plain NCHW: no inner packing
	}
	t := &outTensor{attr: a, c2: c2v, hh: int(a.dims[2]), ww: int(a.dims[3])}
		t.grid = grids[nm[len(nm)-1]-'0']
		outTensors[nm] = t
	}
	for nm, t := range outTensors {
		fmt.Printf("out %s dims=%v fmt=%d zp=%d scale=%.5f wS=%d hS=%d size=%d szs=%d\n",
			nm, t.attr.dims[:t.attr.nDims], t.attr.fmt, t.attr.zp, t.attr.scale, t.attr.wStride, t.attr.hStride, t.attr.size, t.attr.sizeWithStride)
	}
	fmt.Printf("model ready, in size=%d wStride=%d sizeWithStride=%d, outputs=%d\n",
		inAttr.size, inAttr.wStride, inAttr.sizeWithStride, len(outTensors))
	// 第二个 ctx：流水线双缓冲免 rebind
	var c2 uintptr
	if rkInit(unsafe.Pointer(&c2), unsafe.Pointer(&modelBuf[0]), uint32(len(modelBuf)), 0, nil) == 0 {
		ctxs[1] = c2
		dualCtx = true
		fmt.Println("dual rknn ctx ready")
	} else {
		fmt.Println("second rknn ctx unavailable, single-ctx fallback")
	}
	return nil
}

var zcFlag = flag.Bool("zc", true, "zero-copy dma-buf fd input")
var smoothFlag = flag.Float64("smooth", 0.5, "逐帧 EMA 平滑系数（当前帧权重，0=关闭；仅视频/摄像头模式）")
var rotFlag = flag.Bool("rotate180", false, "摄像头画面旋转180度（倒装镜头）")
var modelFlag = flag.String("model", modelPathDefault, "rknn 模型路径")
var confFlag = flag.Float64("conf", 0.5, "检测置信度阈值") // CONF_FLAG_DONE

// allocNpuMem 预分配 NPU 输入/输出内存（启动时一次，io 绑定一次，之后每帧零分配零绑定）
func allocNpuMem() error {
	ctxs[0] = rknnCtx
	nctx := 1
	if dualCtx {
		nctx = 2
	}
	inZeroCopy = false
	for s := 0; s < nctx; s++ {
		mems := map[string]uintptr{}
		raws := map[string][]byte{}
		for nm, t := range outTensors {
			sz := t.attr.sizeWithStride
			if sz == 0 {
				sz = t.attr.size
			}
			m := rkCreateMem(ctxs[s], sz)
			if m == 0 {
				return fmt.Errorf("create out mem %s fail", nm)
			}
			if rkSetIoMem(ctxs[s], m, unsafe.Pointer(&t.attr)) != 0 {
				return fmt.Errorf("set out mem %s fail", nm)
			}
			mems[nm] = m
			raws[nm] = memVirt(m, sz)
		}
		outMems[s] = mems
		outSets[s] = raws
		for nm, r := range raws {
			fmt.Printf("raws[%s] len=%d\n", nm, len(r))
		}

		// 输入：先试 dma-buf fd 直通（零拷贝），失败回退普通 mem
		var inMem uintptr
		if rgaOK && *zcFlag {
			m := rkCreateMemFd(ctxs[s], rgaDstFds[s], unsafe.Pointer(&rgaDstBufs[s][0]), inAttr.size)
			if m != 0 && rkSetIoMem(ctxs[s], m, unsafe.Pointer(&inAttr)) == 0 {
				inMem = m
				inputVirts[s] = rgaDstBufs[s]
				if s == 0 {
					inZeroCopy = true
				}
			} else {
				if m != 0 {
					rkDestroyMem(ctxs[s], m)
				}
			}
		}
		if inMem == 0 {
			inMem = rkCreateMem(ctxs[s], inAttr.size)
			if inMem == 0 {
				return fmt.Errorf("create input mem fail")
			}
			if rkSetIoMem(ctxs[s], inMem, unsafe.Pointer(&inAttr)) != 0 {
				return fmt.Errorf("set input mem fail")
			}
			inputVirts[s] = memVirt(inMem, inAttr.size)
		}
	}
	if inZeroCopy {
		fmt.Println("npu input: dma-buf fd zero-copy")
	} else {
		fmt.Println("npu input: plain mem + copy")
	}
	return nil
}

// attachOutSet 仅单 ctx 回退时用：每帧重绑输出集
func attachOutSet(s int) error {
	for nm, t := range outTensors {
		if rkSetIoMem(rknnCtx, outMems[s][nm], unsafe.Pointer(&t.attr)) != 0 {
			return fmt.Errorf("set out mem %s fail", nm)
		}
	}
	return nil
}

func sigmoid(v float32) float32 {
	if v > 20 {
		v = 20
	} else if v < -20 {
		v = -20
	}
	return float32(1 / (1 + math.Exp(-float64(v))))
}

// letterbox: raw BGR -> inputSize x inputSize RGB（双线性，软件回退路径）
func letterbox(bgr []byte, w, h int, dst []byte) (scale float32, padX, padY int) {
	scale = float32(inputSize) / float32(w)
	if float32(inputSize)/float32(h) < scale {
		scale = float32(inputSize) / float32(h)
	}
	nw := int(float32(w) * scale)
	nh := int(float32(h) * scale)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	for i := range dst {
		dst[i] = padColor
	}
	padX = (inputSize - nw) / 2
	padY = (inputSize - nh) / 2
	for y := 0; y < nh; y++ {
		sy := (float32(y)+0.5)/scale - 0.5
		y0 := int(math.Floor(float64(sy)))
		fy := sy - float32(y0)
		if y0 < 0 {
			y0 = 0
			fy = 0
		}
		y1 := y0 + 1
		if y1 >= h {
			y1 = h - 1
		}
		row0 := y0 * w * 3
		row1 := y1 * w * 3
		drow := (y + padY) * inputSize * 3
		for x := 0; x < nw; x++ {
			sx := (float32(x)+0.5)/scale - 0.5
			x0 := int(math.Floor(float64(sx)))
			fx := sx - float32(x0)
			if x0 < 0 {
				x0 = 0
				fx = 0
			}
			x1 := x0 + 1
			if x1 >= w {
				x1 = w - 1
			}
			p00 := row0 + x0*3
			p01 := row0 + x1*3
			p10 := row1 + x0*3
			p11 := row1 + x1*3
			for ch := 0; ch < 3; ch++ {
				v0 := float32(bgr[p00+ch])*(1-fx) + float32(bgr[p01+ch])*fx
				v1 := float32(bgr[p10+ch])*(1-fx) + float32(bgr[p11+ch])*fx
				v := v0*(1-fy) + v1*fy
				dst[drow+(x+padX)*3+(2-ch)] = byte(v + 0.5)
			}
		}
	}
	return scale, padX, padY
}

// prepFrame 帧预处理：优先 RGA（零拷贝直通 NPU 输入），失败回退软件 letterbox
func prepFrame(bgr []byte, w, h, set int) (scale float32, padX, padY int) {
	if sc, px, py, ok := rgaLetterbox(bgr, w, h, set); ok {
		return sc, px, py
	}
	return letterbox(bgr, w, h, inputVirts[set])
}

type detection struct {
	conf           float32
	x1, y1, x2, y2 float32
	kp             [17][3]float32
}

// smoothDets 逐帧 EMA 平滑（抗 int8 量化/网格离散化抖动）：
// 按框中心最近匹配上一帧检出（416 空间 60px 阈值），box 与可见关键点做指数滑动平均。
// alpha 为当前帧权重：0.5 实测抖动 -46%（mean 3.6→2.0px），更小更稳但滞后更大。
func smoothDets(prev, cur []detection, alpha float32) []detection {
	if len(prev) == 0 {
		return cur
	}
	used := make([]bool, len(prev))
	for ci := range cur {
		c := &cur[ci]
		cx, cy := (c.x1+c.x2)/2, (c.y1+c.y2)/2
		best, bd := -1, float32(60*60)
		for pi := range prev {
			if used[pi] {
				continue
			}
			p := &prev[pi]
			px, py := (p.x1+p.x2)/2, (p.y1+p.y2)/2
			d := (cx-px)*(cx-px) + (cy-py)*(cy-py)
			if d < bd {
				bd, best = d, pi
			}
		}
		if best < 0 {
			continue
		}
		used[best] = true
		p := &prev[best]
		c.x1 = alpha*c.x1 + (1-alpha)*p.x1
		c.y1 = alpha*c.y1 + (1-alpha)*p.y1
		c.x2 = alpha*c.x2 + (1-alpha)*p.x2
		c.y2 = alpha*c.y2 + (1-alpha)*p.y2
		for k := 0; k < 17; k++ {
			if c.kp[k][2] > 0.3 && p.kp[k][2] > 0.3 {
				c.kp[k][0] = alpha*c.kp[k][0] + (1-alpha)*p.kp[k][0]
				c.kp[k][1] = alpha*c.kp[k][1] + (1-alpha)*p.kp[k][1]
			}
		}
	}
	return cur
}

var dflIdx = [16]float32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}

// decode 从输出集 raws 解码检测框 + 17 关键点（纯 Go fp32）
func decode(raws map[string][]byte) []detection {
	var dets []detection
	for s, G := range grids {
		stride := float32(inputSize) / float32(G)
		bt := outTensors[fmt.Sprintf("cv2.%d", s)]
		ct := outTensors[fmt.Sprintf("cv3.%d", s)]
		kt := outTensors[fmt.Sprintf("cv4.%d", s)]
		braw, craw, kraw := raws[fmt.Sprintf("cv2.%d", s)], raws[fmt.Sprintf("cv3.%d", s)], raws[fmt.Sprintf("cv4.%d", s)]
		for gy := 0; gy < G; gy++ {
			for gx := 0; gx < G; gx++ {
				lg := ct.get(craw, 0, gy, gx)
				if sigmoid(lg) < float32(*confFlag) {
					continue
				}
				cf := sigmoid(lg)
				var e [4]float32
				for j := 0; j < 4; j++ {
					var d [16]float32
					mx := float32(-1e30)
					for i := 0; i < 16; i++ {
						d[i] = bt.get(braw, j*16+i, gy, gx)
						if d[i] > mx {
							mx = d[i]
						}
					}
					var sum float32
					for i := 0; i < 16; i++ {
						d[i] = float32(math.Exp(float64(d[i] - mx)))
						sum += d[i]
					}
					var ev float32
					for i := 0; i < 16; i++ {
						ev += dflIdx[i] * d[i]
					}
					e[j] = ev / sum
				}
				cxp := (float32(gx) + 0.5) * stride
				cyp := (float32(gy) + 0.5) * stride
				d := detection{
					conf: cf,
					x1:   cxp - e[0]*stride,
					y1:   cyp - e[1]*stride,
					x2:   cxp + e[2]*stride,
					y2:   cyp + e[3]*stride,
				}
				for k := 0; k < 17; k++ {
					kx := kt.get(kraw, k*3, gy, gx)
					ky := kt.get(kraw, k*3+1, gy, gx)
					ks := kt.get(kraw, k*3+2, gy, gx)
					d.kp[k][0] = (kx*2 + float32(gx)) * stride
					d.kp[k][1] = (ky*2 + float32(gy)) * stride
					d.kp[k][2] = sigmoid(ks)
				}
				dets = append(dets, d)
			}
		}
	}
	return dets
}

func iou(a, b *detection) float32 {
	ix1 := float32(math.Max(float64(a.x1), float64(b.x1)))
	iy1 := float32(math.Max(float64(a.y1), float64(b.y1)))
	ix2 := float32(math.Min(float64(a.x2), float64(b.x2)))
	iy2 := float32(math.Min(float64(a.y2), float64(b.y2)))
	iw := ix2 - ix1
	ih := iy2 - iy1
	if iw <= 0 || ih <= 0 {
		return 0
	}
	inter := iw * ih
	return inter / ((a.x2-a.x1)*(a.y2-a.y1) + (b.x2-b.x1)*(b.y2-b.y1) - inter)
}

func nms(dets []detection) []detection {
	sort.Slice(dets, func(i, j int) bool { return dets[i].conf > dets[j].conf })
	var keep []detection
	sup := make([]bool, len(dets))
	for i := range dets {
		if sup[i] {
			continue
		}
		keep = append(keep, dets[i])
		for j := i + 1; j < len(dets); j++ {
			if !sup[j] && iou(&dets[i], &dets[j]) > nmsThres {
				sup[j] = true
			}
		}
	}
	return keep
}

func clamp01(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// ---------------------------------------------------------------------------
// HTTP 服务模式
// ---------------------------------------------------------------------------

type kpJSON struct {
	X     float32 `json:"x"`
	Y     float32 `json:"y"`
	Score float32 `json:"score"`
}
type personJSON struct {
	Score     float32    `json:"score"`
	Box       [4]float32 `json:"box"`
	Keypoints []kpJSON   `json:"keypoints"`
}

func buildPersons(keep []detection, scale float32, padX, padY, w, h int) []personJSON {
	persons := make([]personJSON, 0, len(keep))
	for _, d := range keep {
		p := personJSON{Score: d.conf}
		p.Box = [4]float32{
			clamp01((d.x1 - float32(padX)) / scale / float32(w)),
			clamp01((d.y1 - float32(padY)) / scale / float32(h)),
			clamp01((d.x2 - float32(padX)) / scale / float32(w)),
			clamp01((d.y2 - float32(padY)) / scale / float32(h)),
		}
		p.Keypoints = make([]kpJSON, 17)
		for k := 0; k < 17; k++ {
			p.Keypoints[k] = kpJSON{
				X:     clamp01((d.kp[k][0] - float32(padX)) / scale / float32(w)),
				Y:     clamp01((d.kp[k][1] - float32(padY)) / scale / float32(h)),
				Score: d.kp[k][2],
			}
		}
		persons = append(persons, p)
	}
	return persons
}

func inferHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	wid, _ := strconv.Atoi(q.Get("w"))
	hei, _ := strconv.Atoi(q.Get("h"))
	body, err := io.ReadAll(r.Body)
	if err != nil || wid <= 0 || hei <= 0 || len(body) != wid*hei*3 {
		w.WriteHeader(400)
		w.Write([]byte(`{"ok":false,"error":"bad frame"}`))
		return
	}
	t0 := time.Now()
	scale, padX, padY := prepFrame(body, wid, hei, 0)
	tPrep := time.Since(t0)
	tr := time.Now()
	if rr := rkRun(ctxs[0], nil); rr != 0 {
		w.WriteHeader(500)
		b, _ := json.Marshal(map[string]interface{}{"ok": false, "error": fmt.Sprintf("rknn_run=%d", rr)})
		w.Write(b)
		return
	}
	runMs := float64(time.Since(tr).Microseconds()) / 1000.0
	keep := nms(decode(outSets[0]))
	persons := buildPersons(keep, scale, padX, padY, wid, hei)
	resp := map[string]interface{}{
		"ok": true, "model_w": inputSize, "model_h": inputSize,
		"infer_ms": math.Round(runMs*10) / 10, "persons": persons,
	}
	b, _ := json.Marshal(resp)
	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
	fmt.Printf("prep %.1f run %.1f | total %.1fms persons %d\n",
		float64(tPrep.Microseconds())/1000.0, runMs,
		float64(time.Since(t0).Microseconds())/1000.0, len(persons))
}

// ---------------------------------------------------------------------------
// 视频/摄像头流水线模式
// ---------------------------------------------------------------------------

type frameBuf struct {
	data []byte
	bgr  []byte // 原始 BGR 副本（V4L2 标注视频用）
	idx  int
	aux  int   // v4l2 buffer index 等源私有数据
	fd   int32 // dma-buf fd（文件源直读 buffer；-1 表示普通内存）
	rfmt int32 // RGA 像素格式
	w, h int
}

type inferJob struct {
	set          int
	frameIdx     int
	scale        float32
	padX, padY   int
	prepMs       float64
	runMs        float64
	frame        *frameBuf
}

var jsonlFile *os.File
var saveRawBGR bool

func detectAction(kp [17][3]float32) string {
	if kp[5][2] < 0.5 || kp[6][2] < 0.5 || kp[11][2] < 0.5 || kp[12][2] < 0.5 {
		return "NONE"
	}
	shoulderY := (kp[5][1] + kp[6][1]) / 2
	hipY := (kp[11][1] + kp[12][1]) / 2
	torso := hipY - shoulderY
	if torso < 8 {
		return "NONE"
	}

	// 蹲下：两个膝盖都明显低于髋部
	if kp[13][2] > 0.5 && kp[14][2] > 0.5 {
		leftKneeY := kp[13][1]
		rightKneeY := kp[14][1]
		if leftKneeY > hipY+torso*0.25 && rightKneeY > hipY+torso*0.25 {
			return "SQUAT"
		}
	}

	// 抬手：任一腕部在肩部上方
	if (kp[9][2] > 0.5 && kp[9][1] < shoulderY-torso*0.35) ||
		(kp[10][2] > 0.5 && kp[10][1] < shoulderY-torso*0.35) {
		return "HAND"
	}

	// 站立：髋、膝、踝从上到下依次增大
	if kp[13][2] > 0.5 && kp[14][2] > 0.5 && kp[15][2] > 0.5 && kp[16][2] > 0.5 {
		if kp[13][1] > hipY+torso*0.1 && kp[14][1] > hipY+torso*0.1 &&
			kp[15][1] > kp[13][1]+torso*0.1 && kp[16][1] > kp[14][1]+torso*0.1 {
			return "STAND"
		}
	}

	return "NONE"
}

func nv12ToBGR(nv12 []byte, w, h int) []byte {
	bgr := make([]byte, w*h*3)
	uvOff := w * h
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			yVal := int(nv12[y*w+x])
			uVal := int(nv12[uvOff+(y/2)*w+(x/2)*2]) - 128
			vVal := int(nv12[uvOff+(y/2)*w+(x/2)*2+1]) - 128

			r := yVal + ((vVal * 1436) >> 10)
			g := yVal - ((uVal * 352 + vVal * 731) >> 10)
			b := yVal + ((uVal * 1814) >> 10)

			if r < 0 { r = 0 } else if r > 255 { r = 255 }
			if g < 0 { g = 0 } else if g > 255 { g = 255 }
			if b < 0 { b = 0 } else if b > 255 { b = 255 }

			idx := (y*w + x) * 3
			bgr[idx] = byte(b)
			bgr[idx+1] = byte(g)
			bgr[idx+2] = byte(r)
		}
	}
	return bgr
}

func yuyvToBGR(yuyv []byte, w, h int) []byte {
	bgr := make([]byte, w*h*3)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x += 2 {
			base := y*w*2 + x*2
			y0 := int(yuyv[base])
			u := int(yuyv[base+1]) - 128
			y1 := int(yuyv[base+2])
			v := int(yuyv[base+3]) - 128

			for i, yVal := range []int{y0, y1} {
				r := yVal + ((v * 1436) >> 10)
				g := yVal - ((u*352 + v*731) >> 10)
				b := yVal + ((u * 1814) >> 10)
				if r < 0 { r = 0 } else if r > 255 { r = 255 }
				if g < 0 { g = 0 } else if g > 255 { g = 255 }
				if b < 0 { b = 0 } else if b > 255 { b = 255 }
				idx := (y*w + x + i) * 3
				bgr[idx] = byte(b)
				bgr[idx+1] = byte(g)
				bgr[idx+2] = byte(r)
			}
		}
	}
	return bgr
}

type frameJSON struct {
	Frame  int       `json:"frame"`
	FPS    float64   `json:"fps"`
	RunMs  float64   `json:"run_ms"`
	Person []perJSON `json:"persons"`
}
type perJSON struct {
	Score float32      `json:"score"`
	Box   [4]float32   `json:"box"` // 像素坐标 x1,y1,x2,y2
	Kp    [17][3]float32 `json:"kp"` // 像素坐标 x,y + score
}

// runPipeline 三级流水线：取帧(source) -> 预处理+推理(infer) -> 解码+统计(decode)
// NPU 推理第 N 帧时 CPU 并行解码第 N-1 帧，吞吐 = max(各阶段)
func runPipeline(src frameSource, frames int, annoPath, rawAnnoPath string, brightness float32, vw, vh int) error {
	saveRawBGR = rawAnnoPath != ""
	frameCh := make(chan *frameBuf, 2)
	freeBufs := make(chan *frameBuf, 2)
	freeBufs <- src.newBuf()
	freeBufs <- src.newBuf()
	jobs := make(chan inferJob, 2)
	freeSets := make(chan int, 2)
	freeSets <- 0
	freeSets <- 1
	done := make(chan struct{})

	var rawAnnoFile *os.File
	if rawAnnoPath != "" {
		f, err := os.Create(rawAnnoPath)
		if err != nil {
			return fmt.Errorf("raw-annotated: %w", err)
		}
		rawAnnoFile = f
	}

	// 源协程
	srcErr := make(chan error, 1)
	go func() {
		for i := 0; i < frames; i++ {
			fb := <-freeBufs
			if err := src.next(fb); err != nil {
				srcErr <- err
				close(frameCh)
				return
			}
			fb.idx = i
			frameCh <- fb
		}
		close(frameCh)
	}()

	// 推理协程：letterbox(零拷贝) + run（io 启动时已绑定）
	go func() {
		for fb := range frameCh {
			set := <-freeSets
			tp := time.Now()
			scale, padX, padY := src.prep(fb, set)
			src.release(fb) // v4l2: QBUF 还 buffer；文件源: no-op
			prepMs := float64(time.Since(tp).Microseconds()) / 1000.0
			if !dualCtx {
				if err := attachOutSet(set); err != nil {
					fmt.Println("attach out:", err)
					break
				}
			}
			ctx := ctxs[0]
			if dualCtx {
				ctx = ctxs[set]
			}
			tr := time.Now()
			if rr := rkRun(ctx, nil); rr != 0 {
				fmt.Println("rknn_run=", rr)
				break
			}
			runMs := float64(time.Since(tr).Microseconds()) / 1000.0
			jobs <- inferJob{set: set, frameIdx: fb.idx, scale: scale, padX: padX, padY: padY,
				prepMs: prepMs, runMs: runMs, frame: fb}
		}
		close(jobs)
	}()

	// 解码协程（与下一帧推理并行）
	go func() {
		start := time.Now()
		var sumPrep, sumRun, sumDec float64
		var sumPersons, n int
		annotated := false
		var prevDets []detection
		for j := range jobs {
			td := time.Now()
			keep := nms(decode(outSets[j.set]))
			if *smoothFlag > 0 {
				keep = smoothDets(prevDets, keep, float32(*smoothFlag))
				prevDets = keep
			}
			decMs := float64(time.Since(td).Microseconds()) / 1000.0
			freeSets <- j.set
			n++
			sumPrep += j.prepMs
			sumRun += j.runMs
			sumDec += decMs
			sumPersons += len(keep)
			if v4l2Src, ok2 := src.(*v4l2Source); ok2 {
				if rgaOK && len(rgaAnnoBuf) > 0 {
					if rgaConvertToBGR2(v4l2Src.lastFd(nil), v4l2Src.rgaFmt, v4l2Src.w, v4l2Src.h) {
						fpsNow := float64(n) / time.Since(start).Seconds()
						pushAnnotated(rgaAnnoBuf, 640, 480, keep, j.scale, j.padX, j.padY, fpsNow)
					}
				}
			}
			if n == 1 || n%30 == 0 {
				el := time.Since(start).Seconds()
				fmt.Printf("frame %4d | prep %5.1f run %5.1f dec %4.1f | persons %d | avg %.1f FPS\n",
					j.frameIdx, j.prepMs, j.runMs, decMs, len(keep), float64(n)/el)
			}
		if annoPath != "" && !annotated && len(keep) > 0 && n >= 5 && len(j.frame.data) >= vw*vh*3 {
			annotate(j.frame.data, vw, vh, keep, j.scale, j.padX, j.padY, annoPath)
			annotated = true
		}
		if rawAnnoFile != nil && len(j.frame.bgr) >= vw*vh*3 {
			annotateToRaw(j.frame.bgr, vw, vh, keep, j.scale, j.padX, j.padY, rawAnnoFile, brightness)
		}
			if jsonlFile != nil {
				fj := frameJSON{Frame: j.frameIdx, FPS: float64(n) / time.Since(start).Seconds(), RunMs: j.runMs}
				for _, d := range keep {
					pj := perJSON{Score: d.conf}
					pj.Box = [4]float32{(d.x1 - float32(j.padX)) / j.scale, (d.y1 - float32(j.padY)) / j.scale,
						(d.x2 - float32(j.padX)) / j.scale, (d.y2 - float32(j.padY)) / j.scale}
					for k := 0; k < 17; k++ {
						pj.Kp[k] = [3]float32{(d.kp[k][0] - float32(j.padX)) / j.scale, (d.kp[k][1] - float32(j.padY)) / j.scale, d.kp[k][2]}
					}
					fj.Person = append(fj.Person, pj)
				}
				lb, _ := json.Marshal(fj)
				jsonlFile.Write(lb)
				jsonlFile.Write([]byte("\n"))
			}
			freeBufs <- j.frame
		}
		el := time.Since(start).Seconds()
		fmt.Printf("== summary: %d frames in %.1fs -> %.1f FPS | avg prep %.1f run %.1f dec %.1f ms | avg persons %.1f ==\n",
			n, el, float64(n)/el, sumPrep/float64(n), sumRun/float64(n), sumDec/float64(n), float64(sumPersons)/float64(n))
		close(done)
	}()

	select {
	case err := <-srcErr:
		if rawAnnoFile != nil {
			rawAnnoFile.Close()
		}
		return err
	case <-done:
	}
	<-done
	if rawAnnoFile != nil {
		rawAnnoFile.Close()
		fmt.Println("raw-annotated ->", rawAnnoPath)
	}
	return nil
}

// ---------------------------------------------------------------------------
// 标注（PNG 可视化验证）
// ---------------------------------------------------------------------------

func putPx(img *image.NRGBA, x, y int, c color.NRGBA) {
	if x >= 0 && y >= 0 && x < img.Bounds().Dx() && y < img.Bounds().Dy() {
		img.SetNRGBA(x, y, c)
	}
}

func drawLine(img *image.NRGBA, x0, y0, x1, y1 int, c color.NRGBA) {
	dx := int(math.Abs(float64(x1 - x0)))
	dy := -int(math.Abs(float64(y1 - y0)))
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx + dy
	for {
		putPx(img, x0, y0, c)
		putPx(img, x0+1, y0, c)
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

var skeletonPairs = [][2]int{
	{0, 1}, {0, 2}, {1, 3}, {2, 4}, {5, 6}, {5, 7}, {7, 9}, {6, 8}, {8, 10},
	{5, 11}, {6, 12}, {11, 12}, {11, 13}, {13, 15}, {12, 14}, {14, 16},
}

func annotate(bgr []byte, w, h int, keep []detection, scale float32, padX, padY int, path string) {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			o := (y*w + x) * 3
			img.SetNRGBA(x, y, color.NRGBA{bgr[o+2], bgr[o+1], bgr[o], 255})
		}
	}
	green := color.NRGBA{0, 255, 0, 255}
	red := color.NRGBA{255, 60, 60, 255}
	yellow := color.NRGBA{255, 255, 0, 255}
	mapX := func(v float32) int { return int((v - float32(padX)) / scale) }
	mapY := func(v float32) int { return int((v - float32(padY)) / scale) }
	for _, d := range keep {
		x1, y1, x2, y2 := mapX(d.x1), mapY(d.y1), mapX(d.x2), mapY(d.y2)
		drawLine(img, x1, y1, x2, y1, green)
		drawLine(img, x2, y1, x2, y2, green)
		drawLine(img, x2, y2, x1, y2, green)
		drawLine(img, x1, y2, x1, y1, green)
		for _, pr := range skeletonPairs {
			a, b := d.kp[pr[0]], d.kp[pr[1]]
			if a[2] > 0.5 && b[2] > 0.5 {
				drawLine(img, mapX(a[0]), mapY(a[1]), mapX(b[0]), mapY(b[1]), red)
			}
		}
		for _, kp := range d.kp {
			if kp[2] > 0.5 {
				cx, cy := mapX(kp[0]), mapY(kp[1])
				for dy := -2; dy <= 2; dy++ {
					for dx := -2; dx <= 2; dx++ {
						putPx(img, cx+dx, cy+dy, yellow)
					}
				}
			}
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	png.Encode(f, img)
	fmt.Println("annotated ->", path)
}

// ---------------------------------------------------------------------------
// 实时标注视频（每帧写 raw BGR，可选亮度增强）
// ---------------------------------------------------------------------------

func whiteBalance(bgr []byte, w, h int, maxGain float32) {
	n := w * h
	if n == 0 || len(bgr) < n*3 {
		return
	}
	var sumR, sumG, sumB int64
	for i := 0; i < len(bgr); i += 3 {
		sumB += int64(bgr[i])
		sumG += int64(bgr[i+1])
		sumR += int64(bgr[i+2])
	}
	avgR := float32(sumR) / float32(n)
	avgG := float32(sumG) / float32(n)
	avgB := float32(sumB) / float32(n)
	avgGray := (avgR + avgG + avgB) / 3.0

	rGain := avgGray / avgR
	gGain := avgGray / avgG
	bGain := avgGray / avgB
	if rGain > maxGain {
		rGain = maxGain
	}
	if gGain > maxGain {
		gGain = maxGain
	}
	if bGain > maxGain {
		bGain = maxGain
	}

	for i := 0; i < len(bgr); i += 3 {
		b := float32(bgr[i]) * bGain
		g := float32(bgr[i+1]) * gGain
		r := float32(bgr[i+2]) * rGain
		if b > 255 {
			b = 255
		}
		if g > 255 {
			g = 255
		}
		if r > 255 {
			r = 255
		}
		bgr[i] = uint8(b)
		bgr[i+1] = uint8(g)
		bgr[i+2] = uint8(r)
	}
}

func putPxBGR(bgr []byte, w, h, x, y int, cb, cg, cr uint8) {
	if x >= 0 && y >= 0 && x < w && y < h {
		o := (y*w + x) * 3
		bgr[o] = cb
		bgr[o+1] = cg
		bgr[o+2] = cr
	}
}

func drawLineBGR(bgr []byte, w, h, x0, y0, x1, y1 int, cb, cg, cr uint8) {
	dx := int(math.Abs(float64(x1 - x0)))
	dy := -int(math.Abs(float64(y1 - y0)))
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx + dy
	for {
		putPxBGR(bgr, w, h, x0, y0, cb, cg, cr)
		putPxBGR(bgr, w, h, x0+1, y0, cb, cg, cr)
		if x0 == x1 && y0 == y1 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

// 5x7 点阵字体（仅 A-Z 和 _）
var font5x7 = map[byte][5]byte{
	'A': {0x7E, 0x09, 0x09, 0x09, 0x7E},
	'B': {0x7F, 0x49, 0x49, 0x49, 0x36},
	'C': {0x3E, 0x41, 0x41, 0x41, 0x22},
	'D': {0x7F, 0x41, 0x41, 0x22, 0x1C},
	'E': {0x7F, 0x49, 0x49, 0x49, 0x41},
	'F': {0x7F, 0x09, 0x09, 0x09, 0x01},
	'G': {0x3E, 0x41, 0x49, 0x49, 0x7A},
	'H': {0x7F, 0x08, 0x08, 0x08, 0x7F},
	'I': {0x00, 0x41, 0x7F, 0x41, 0x00},
	'J': {0x20, 0x40, 0x41, 0x3F, 0x01},
	'K': {0x7F, 0x08, 0x14, 0x22, 0x41},
	'L': {0x7F, 0x40, 0x40, 0x40, 0x40},
	'M': {0x7F, 0x02, 0x0C, 0x02, 0x7F},
	'N': {0x7F, 0x04, 0x08, 0x10, 0x7F},
	'O': {0x3E, 0x41, 0x41, 0x41, 0x3E},
	'P': {0x7F, 0x09, 0x09, 0x09, 0x06},
	'Q': {0x3E, 0x41, 0x51, 0x21, 0x5E},
	'R': {0x7F, 0x09, 0x19, 0x29, 0x46},
	'S': {0x46, 0x49, 0x49, 0x49, 0x31},
	'T': {0x01, 0x01, 0x7F, 0x01, 0x01},
	'U': {0x3F, 0x40, 0x40, 0x40, 0x3F},
	'V': {0x1F, 0x20, 0x40, 0x20, 0x1F},
	'W': {0x3F, 0x40, 0x38, 0x40, 0x3F},
	'X': {0x63, 0x14, 0x08, 0x14, 0x63},
	'Y': {0x07, 0x08, 0x70, 0x08, 0x07},
	'Z': {0x61, 0x51, 0x49, 0x45, 0x43},
	'_': {0x00, 0x40, 0x40, 0x40, 0x00},
}

func drawActionText(bgr []byte, w, h int, text string, x0, y0 int, c color.RGBA) {
	cx := x0
	for i := 0; i < len(text); i++ {
		ch := text[i]
		if ch >= 'a' && ch <= 'z' {
			ch -= 32
		}
		bits, ok := font5x7[ch]
		if !ok {
			cx += 12
			continue
		}
		for col := 0; col < 5; col++ {
			colBits := bits[col]
			px := cx + col*2
			for row := 0; row < 7; row++ {
				if (colBits>>uint(6-row))&1 != 0 {
					py := y0 - 12 + row*2
					for dy := 0; dy < 2; dy++ {
						for dx := 0; dx < 2; dx++ {
							putPxBGR(bgr, w, h, px+dx, py+dy, c.B, c.G, c.R)
						}
					}
				}
			}
		}
		cx += 12
	}
}

func annotateToRaw(src []byte, w, h int, keep []detection, scale float32, padX, padY int, out *os.File, brightness float32) {
	buf := make([]byte, len(src))
	copy(buf, src)

	// 自动白平衡（灰度世界假设）
	whiteBalance(buf, w, h, 3.5)

	// 自动亮度 + gamma：把平均亮度拉到目标范围，改善暗光
	n := w * h
	var sumLum int64
	for i := 0; i < len(buf); i += 3 {
		b := int(buf[i])
		g := int(buf[i+1])
		r := int(buf[i+2])
		maxVal := r
		if g > maxVal {
			maxVal = g
		}
		if b > maxVal {
			maxVal = b
		}
		sumLum += int64(maxVal)
	}
	meanLum := float64(sumLum) / float64(n)
	gamma := 0.55
	targetMean := 110.0
	meanAfterGamma := 255.0 * math.Pow(meanLum/255.0, gamma)
	autoScale := targetMean / meanAfterGamma
	if autoScale < 1.0 {
		autoScale = 1.0
	}
	if autoScale > 4.0 {
		autoScale = 4.0
	}
	finalScale := autoScale * float64(brightness)
	for i := 0; i < len(buf); i += 3 {
		for j := 0; j < 3; j++ {
			v := float64(buf[i+j]) / 255.0
			v = math.Pow(v, gamma)
			v *= finalScale * 255.0
			if v > 255 {
				v = 255
			}
			if v < 0 {
				v = 0
			}
			buf[i+j] = uint8(v + 0.5)
		}
	}

	// 顶部状态条（全局参考）
	barH := 8
	for y := 0; y < barH && y < h; y++ {
		for x := 0; x < w; x++ {
			o := (y*w + x) * 3
			buf[o] = 0
			buf[o+1] = 0
			buf[o+2] = 0
		}
	}

	mapX := func(v float32) int { return int((v - float32(padX)) / scale) }
	mapY := func(v float32) int { return int((v - float32(padY)) / scale) }

	for _, d := range keep {
		x1, y1, x2, y2 := mapX(d.x1), mapY(d.y1), mapX(d.x2), mapY(d.y2)
		// green box
		drawLineBGR(buf, w, h, x1, y1, x2, y1, 0, 255, 0)
		drawLineBGR(buf, w, h, x2, y1, x2, y2, 0, 255, 0)
		drawLineBGR(buf, w, h, x2, y2, x1, y2, 0, 255, 0)
		drawLineBGR(buf, w, h, x1, y2, x1, y1, 0, 255, 0)
		// red bones
		for _, pr := range skeletonPairs {
			a, b := d.kp[pr[0]], d.kp[pr[1]]
			if a[2] > 0.5 && b[2] > 0.5 {
				drawLineBGR(buf, w, h, mapX(a[0]), mapY(a[1]), mapX(b[0]), mapY(b[1]), 60, 60, 255)
			}
		}
		// yellow keypoints
		for _, kp := range d.kp {
			if kp[2] > 0.5 {
				cx, cy := mapX(kp[0]), mapY(kp[1])
				for dy := -2; dy <= 2; dy++ {
					for dx := -2; dx <= 2; dx++ {
						putPxBGR(buf, w, h, cx+dx, cy+dy, 0, 255, 255)
					}
				}
			}
		}

		// 多目标动作：在每个人体框上方画动作文字（2x 放大）
		action := detectAction(d.kp)
		textW := len(action) * 12
		tx := x1
		ty := y1 - 4
		if ty < barH+14 {
			ty = barH + 14
		}
		// 黑色背景
		for yy := ty - 14; yy <= ty+2 && yy < h; yy++ {
			for xx := tx - 2; xx < tx+textW+2 && xx < w; xx++ {
				putPxBGR(buf, w, h, xx, yy, 0, 0, 0)
			}
		}
		var tc color.RGBA
		switch action {
		case "STAND":
			tc = color.RGBA{0, 255, 0, 255} // green
		case "SQUAT":
			tc = color.RGBA{0, 0, 255, 255} // red
		case "HAND":
			tc = color.RGBA{0, 255, 255, 255} // yellow
		default:
			tc = color.RGBA{255, 255, 255, 255} // white
		}
		drawActionText(buf, w, h, action, tx, ty, tc)
	}
	out.Write(buf)
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	var (
		videoFile = flag.String("video", "", "raw BGR video file path")
		v4l2Dev   = flag.String("v4l2", "", "v4l2 device path (reserved, untested without camera)")
		vw        = flag.Int("vw", 640, "frame width")
		vh        = flag.Int("vh", 360, "frame height")
		frames    = flag.Int("frames", 240, "frames to process")
		anno      = flag.String("anno", "", "save annotated PNG of a mid frame")
		rawAnno   = flag.String("raw-annotated", "", "write annotated raw BGR video (every frame)")
		bright    = flag.Float64("brightness", 1.0, "brightness multiplier for raw-annotated output (1.0=original)")
		jsonl     = flag.String("jsonl", "", "write per-frame detections JSON lines")
	)
	flag.Parse()
	rgaRot180 = *rotFlag
	// 方案A: 任何模式(含v4l2/文件)下, 只要带端口参数就起 HTTP+/stream
	httpServeFlag := ""
	if flag.NArg() > 0 {
		httpServeFlag = flag.Arg(0)
	}
	if httpServeFlag != "" {
		registerHTTPHandlers()
		go http.ListenAndServe(":"+httpServeFlag, nil)
	}

	if err := setup(); err != nil {
		fmt.Println("setup fail:", err)
		os.Exit(1)
	}
	if err := rgaSetup(); err != nil {
		fmt.Println("rga disabled:", err)
	}
	if err := allocNpuMem(); err != nil {
		fmt.Println("alloc npu mem fail:", err)
		os.Exit(1)
	}

	switch {
	case *videoFile != "":
		if *jsonl != "" {
			f, err := os.Create(*jsonl)
			if err != nil {
				fmt.Println("jsonl:", err)
				os.Exit(1)
			}
			jsonlFile = f
			defer f.Close()
		}
		src, err := newFileSource(*videoFile, *vw, *vh)
		if err != nil {
			fmt.Println("open video:", err)
			os.Exit(1)
		}
		if err := runPipeline(src, *frames, *anno, *rawAnno, float32(*bright), *vw, *vh); err != nil {
			fmt.Println("pipeline:", err)
			os.Exit(1)
		}
	case *v4l2Dev != "":
		if *jsonl != "" {
			f, err := os.Create(*jsonl)
			if err != nil {
				fmt.Println("jsonl:", err)
				os.Exit(1)
			}
			jsonlFile = f
			defer f.Close()
		}
		src, err := newV4l2Source(*v4l2Dev, *vw, *vh)
		if err != nil {
			fmt.Println("open v4l2:", err)
			os.Exit(1)
		}
		defer src.close()
		if err := runPipeline(src, *frames, *anno, *rawAnno, float32(*bright), *vw, *vh); err != nil {
			fmt.Println("pipeline:", err)
			os.Exit(1)
		}
	default:
		if httpServeFlag != "" {
			select {}
		}
		port := 18080
		registerHTTPHandlers()
		fmt.Printf("serving :%d\n", port)
		http.ListenAndServe(fmt.Sprintf("0.0.0.0:%d", port), nil)
	}
}

var httpHandlersOnce sync.Once

func registerHTTPHandlers() {
	httpHandlersOnce.Do(func() {
		http.HandleFunc("/stream", mjpegHandler)
		http.HandleFunc("/infer", inferHandler)
		http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"status":"ok"}`))
		})
	})
}

// mjpegHandler: 方案A 上位机显示端点 multipart/x-mixed-replace
func mjpegHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
	var last uint64
	for {
		mjMu.Lock()
		jpg, seq := mjJPEG, mjSeq
		mjMu.Unlock()
		if seq == last || len(jpg) == 0 {
			time.Sleep(20 * time.Millisecond)
			select {
			case <-r.Context().Done():
				return
			default:
			}
			continue
		}
		last = seq
		w.Write([]byte("\r\n--frame\r\nContent-Type: image/jpeg\r\nContent-Length: " + fmt.Sprint(len(jpg)) + "\r\n\r\n"))
		if _, err := w.Write(jpg); err != nil {
			return
		}
		w.Write([]byte("\r\n"))
		select {
		case <-r.Context().Done():
			return
		default:
		}
	}
}
