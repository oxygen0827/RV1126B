// rga.go: RGA 硬件加速 letterbox（dma_heap 分配 + c_RkRgaBlit 缩放+BGR->RGB）
// 零拷贝链路：文件/摄像头帧(读进 dma-buf) -> RGA letterbox -> NPU 输入 dma-buf（fd 直通）
// 双 dst buffer 对应双 NPU ctx，流水线免 rebind
package main

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"

	"github.com/ebitengine/purego"
)

const (
	rkFmtRGB888 = 0x2 << 8
	rkFmtBGR888 = 0x7 << 8
	rkFmtNV12   = 0xa << 8  // RK_FORMAT_YCbCr_420_SP
	rkFmtYUYV   = 0x1c << 8 // RK_FORMAT_YUYV_422 (board rga.h)

	// rga_info_t 字段偏移（Linux aarch64；板端 drmrga.h 核验）
	riFd        = 0
	riFormat    = 28
	riRect      = 32 // rga_rect(32B): x=0 y=4 w=8 h=12 ws=16 hs=20 fmt=24 size=28
	riBufSize   = 68
	riMmuFlag   = 84
	riScaleMode = 104
	riInFence   = 180
	riOutFence  = 184
	riSize      = 704

	dmaHeapPath       = "/dev/rk_dma_heap/rk-dma-heap-cma"
	dmaHeapIoctlAlloc = 0xC0184800
	dmaFdFlags        = 0o2000002 // O_RDWR|O_CLOEXEC

	rgaMaxW = 1920
	rgaMaxH = 1088
)

var (
	rgaOK      bool
	rgaHeapFd  int
	rgaLibInit func() int32
	rgaLibBlit func(src, dst, pat unsafe.Pointer) int32

	rgaSrcBuf   []byte // mmap 的源 dma-buf（HTTP 模式帧暂存）
	rgaSrcFd    int32
	rgaDstBufs  [2][]byte // mmap 的目标 dma-buf（416x416x3，零拷贝时即 NPU 输入）
	rgaDstFds   [2]int32
	rgaAnnoBuf  []byte // mmap 的预览 BGR 输出（横屏 640x360 / 竖屏 360x640）
	rgaAnnoFd   int32
	rgaMu       sync.Mutex
	rgaPreviewW = 640
	rgaPreviewH = 360
)

func rgaPutI(b []byte, off int, v int32) { *(*int32)(unsafe.Pointer(&b[off])) = v }

var rgaRotation int32 // Android HAL_TRANSFORM_*，写入源描述符

func orientedDims(w, h int) (int, int) {
	if rgaRotation == 0x04 || rgaRotation == 0x07 { // ROT_90 / ROT_270
		return h, w
	}
	return w, h
}

func applySourceRotation(info []byte) {
	if rgaRotation != 0 {
		rgaPutI(info, 72, rgaRotation)
	}
}

func rgaMakeInfo(fd int32, format int32, x, y, w, h, ws, hs int32, sizeBytes int32) []byte {
	b := make([]byte, riSize)
	rgaPutI(b, riFd, fd)
	rgaPutI(b, riFormat, format)
	rgaPutI(b, riRect+0, x)
	rgaPutI(b, riRect+4, y)
	rgaPutI(b, riRect+8, w)
	rgaPutI(b, riRect+12, h)
	rgaPutI(b, riRect+16, ws)
	rgaPutI(b, riRect+20, hs)
	rgaPutI(b, riRect+24, format)
	rgaPutI(b, riRect+28, sizeBytes)
	rgaPutI(b, riBufSize, sizeBytes)
	rgaPutI(b, riMmuFlag, 1)
	rgaPutI(b, riScaleMode, 1) // bilinear
	rgaPutI(b, riInFence, -1)
	rgaPutI(b, riOutFence, -1)
	return b
}

func dmaAlloc(size int) (int32, []byte, error) {
	size = (size + 4095) &^ 4095 // 页对齐（fd 直通 NPU 需要）
	var req [24]byte
	*(*uint64)(unsafe.Pointer(&req[0])) = uint64(size)
	*(*uint32)(unsafe.Pointer(&req[12])) = dmaFdFlags
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(rgaHeapFd),
		dmaHeapIoctlAlloc, uintptr(unsafe.Pointer(&req[0])))
	if errno != 0 {
		return 0, nil, fmt.Errorf("dma_heap alloc: %v", errno)
	}
	fd := int(*(*uint32)(unsafe.Pointer(&req[8])))
	data, err := syscall.Mmap(fd, 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		syscall.Close(fd)
		return 0, nil, fmt.Errorf("mmap: %v", err)
	}
	return int32(fd), data, nil
}

func rgaSetup() error {
	fd, err := syscall.Open(dmaHeapPath, syscall.O_RDWR, 0)
	if err != nil {
		return err
	}
	rgaHeapFd = fd
	lib, err := purego.Dlopen("/oem/usr/lib/librga.so", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return err
	}
	purego.RegisterLibFunc(&rgaLibInit, lib, "c_RkRgaInit")
	purego.RegisterLibFunc(&rgaLibBlit, lib, "c_RkRgaBlit")
	if r := rgaLibInit(); r != 0 {
		return fmt.Errorf("RkRgaInit=%d", r)
	}
	sfd, sbuf, err := dmaAlloc(rgaMaxW * rgaMaxH * 3)
	if err != nil {
		return fmt.Errorf("src buf: %w", err)
	}
	rgaSrcFd, rgaSrcBuf = sfd, sbuf
	for s := 0; s < 2; s++ {
		dfd, dbuf, err := dmaAlloc(inputSize * inputSize * 3)
		if err != nil {
			return fmt.Errorf("dst buf %d: %w", s, err)
		}
		rgaDstFds[s], rgaDstBufs[s] = dfd, dbuf
	}
	rgaPreviewW, rgaPreviewH = 640, 360
	if w, h := orientedDims(rgaPreviewW, rgaPreviewH); w != rgaPreviewW {
		rgaPreviewW, rgaPreviewH = w, h
	}
	// 横屏和竖屏像素数相同，分配一块预览/录像输出 buffer。
	afd, abuf, err := dmaAlloc(640 * 360 * 3)
	if err != nil {
		return fmt.Errorf("anno buf: %w", err)
	}
	rgaAnnoFd, rgaAnnoBuf = afd, abuf
	rgaOK = true
	fmt.Println("rga ready (dma_heap + librga, 2 dst bufs + anno buf)")
	return nil
}

// fillPads 只填充 letterbox 灰边区域（比整帧填充省 ~0.5ms）
func fillPads(dst []byte, padX, padY, nw, nh int) {
	row := inputSize * 3
	if padY > 0 {
		for i := 0; i < padY*row; i++ {
			dst[i] = padColor
		}
		bot := (padY + nh) * row
		for i := bot; i < inputSize*row; i++ {
			dst[i] = padColor
		}
	}
	if padX > 0 {
		for y := padY; y < padY+nh; y++ {
			base := y * row
			for i := 0; i < padX*3; i++ {
				dst[base+i] = padColor
			}
			r := base + (padX+nw)*3
			for i := r; i < base+row; i++ {
				dst[i] = padColor
			}
		}
	}
}

func letterboxGeom(w, h int) (scale float32, padX, padY, nw, nh int) {
	scale = float32(inputSize) / float32(w)
	if float32(inputSize)/float32(h) < scale {
		scale = float32(inputSize) / float32(h)
	}
	// Integer geometry avoids float32 truncating 720 * (416/1280) to 233.
	longest := w
	if h > longest {
		longest = h
	}
	nw = (w*inputSize + longest/2) / longest
	nh = (h*inputSize + longest/2) / longest
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	padX = (inputSize - nw) / 2
	padY = (inputSize - nh) / 2
	return
}

// blitInto 将 srcFd 描述的帧 RGA 缩放+格式转换进 rgaDstBufs[set]（letterbox 几何）
// doFill=false 时跳过灰边填充（视频流几何不变，pad 区域填充一次即可）
func blitInto(set int, doFill bool, srcFd int32, srcFmt int32, w, h, ws, hs, srcBytes int) (float32, int, int, bool) {
	viewW, viewH := orientedDims(w, h)
	scale, padX, padY, nw, nh := letterboxGeom(viewW, viewH)
	if doFill {
		fillPads(rgaDstBufs[set], padX, padY, nw, nh)
	}
	srcInfo := rgaMakeInfo(srcFd, srcFmt, 0, 0, int32(w), int32(h), int32(ws), int32(hs), int32(srcBytes))
	applySourceRotation(srcInfo)
	dstInfo := rgaMakeInfo(rgaDstFds[set], rkFmtRGB888, int32(padX), int32(padY), int32(nw), int32(nh),
		inputSize, inputSize, inputSize*inputSize*3)
	if r := rgaLibBlit(unsafe.Pointer(&srcInfo[0]), unsafe.Pointer(&dstInfo[0]), nil); r != 0 {
		return 0, 0, 0, false
	}
	return scale, padX, padY, true
}

// rgaConvertToBGR 用 RGA 把源帧转成等比例方向正确的预览 BGR。
func rgaConvertToBGR(srcFd int32, srcFmt int32, w, h, ws, hs, srcBytes int) bool {
	if !rgaOK || srcFd < 0 {
		return false
	}
	srcInfo := rgaMakeInfo(srcFd, srcFmt, 0, 0, int32(w), int32(h), int32(ws), int32(hs), int32(srcBytes))
	applySourceRotation(srcInfo)
	dstInfo := rgaMakeInfo(rgaAnnoFd, rkFmtBGR888, 0, 0, int32(rgaPreviewW), int32(rgaPreviewH),
		int32(rgaPreviewW), int32(rgaPreviewH), 640*360*3)
	if r := rgaLibBlit(unsafe.Pointer(&srcInfo[0]), unsafe.Pointer(&dstInfo[0]), nil); r != 0 {
		return false
	}
	return true
}

// rgaLetterbox: bgr 帧(内存) -> RGA 缩放+BGR->RGB -> NPU 输入（HTTP 路径用，经 rgaSrcBuf 中转）
func rgaLetterbox(bgr []byte, w, h, set int) (scale float32, padX, padY int, ok bool) {
	if !rgaOK || w > rgaMaxW || h > rgaMaxH || len(bgr) != w*h*3 {
		return 0, 0, 0, false
	}
	rgaMu.Lock()
	defer rgaMu.Unlock()
	copy(rgaSrcBuf, bgr)
	scale, padX, padY, ok = blitInto(set, true, rgaSrcFd, rkFmtBGR888, w, h, w, h, w*h*3)
	if !ok {
		return 0, 0, 0, false
	}
	if !inZeroCopy {
		copy(inputVirts[set], rgaDstBufs[set][:inAttr.size])
	}
	return scale, padX, padY, true
}

// rgaLetterboxFd: 直接从 dma-buf fd（文件源直读 buffer / V4L2 EXPBUF 帧）letterbox 进 NPU 输入
// doFill: 是否重填灰边（流式源几何不变，首帧填充后即可跳过）
func rgaLetterboxFd(set int, doFill bool, srcFd int32, srcFmt int32, w, h, ws, hs, srcBytes int) (scale float32, padX, padY int, ok bool) {
	if !rgaOK || w > rgaMaxW || h > rgaMaxH {
		return 0, 0, 0, false
	}
	rgaMu.Lock()
	defer rgaMu.Unlock()
	scale, padX, padY, ok = blitInto(set, doFill, srcFd, srcFmt, w, h, ws, hs, srcBytes)
	if !ok {
		return 0, 0, 0, false
	}
	if !inZeroCopy {
		copy(inputVirts[set], rgaDstBufs[set][:inAttr.size])
	}
	return scale, padX, padY, true
}
