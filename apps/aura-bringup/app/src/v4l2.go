// v4l2.go: V4L2 摄像头帧源（ARM32 多平面，EXPBUF 零拷贝，已实测跑通）
// 链路：sensor -> rkcif/rkisp -> V4L2 MMAP buffer --EXPBUF--> dma-buf fd
//
//	--RGA(NV12->RGB letterbox)--> NPU 输入 dma-buf，全程零拷贝
//
// EXPBUF 不可用时回退 mmap + memcpy 到 RGA 源 buffer（仍只有一拷）
package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	vidiocQuerycap  = 0x80685600
	vidiocGFmt      = 0xC0D05604 // ARM64: v4l2_format = 208 字节（板实测）
	vidiocSFmt      = 0xC0D05605 // ARM64: v4l2_format = 208 字节（板实测）
	vidiocReqbufs   = 0xC0145608
	vidiocQuerybuf  = 0xC0585609 // ARM64: v4l2_buffer = 88 字节
	vidiocExpbuf    = 0xC0405610
	vidiocQbuf      = 0xC058560F // ARM64: v4l2_buffer = 88 字节
	vidiocDqbuf     = 0xC0585611 // ARM64: v4l2_buffer = 88 字节
	vidiocStreamon  = 0x40045612
	vidiocStreamoff = 0x40045613

	v4l2BufTypeCapture = 9 // V4L2_BUF_TYPE_VIDEO_CAPTURE_MPLANE
	v4l2MemoryMmap     = 1
	v4l2FieldAny       = 0

	pixFmtNV12 = 0x3231564E // 'NV12'
	pixFmtYUYV = 0x56595559 // 'YUYV'

	v4l2NBuf = 3
)

// v4l2_buffer 字段偏移（arm32，多平面，32-bit time_t，总长 68）
const (
	vbIndex     = 0
	vbType      = 4
	vbBytesused = 8
	vbFlags     = 12
	vbField     = 16
	vbTimeval   = 20
	vbSequence  = 44
	vbMemory    = 60
	vbMPlanes   = 64
	vbLength    = 72
	vbSize      = 96 // 88 实测，留余量
)

type v4l2Plane struct {
	bytesused  uint32
	length     uint32
	mOffset    uint32
	dataOffset uint32
	reserved   [11]uint32
}

type v4l2Source struct {
	fd        int
	w, h      int
	rgaFmt    int32
	srcBytes  int
	stride    int
	expFds    [v4l2NBuf]int32
	maps      [v4l2NBuf][]byte
	lengths   [v4l2NBuf]uint32
	useExp    bool
	streaming bool
	filled    [2]bool
}

func ioctl(fd int, req uintptr, arg unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(arg))
	if errno != 0 {
		return errno
	}
	return nil
}

// setFmt 尝试设置像素格式，返回实际生效的 pixfmt/stride/size
func (s *v4l2Source) setFmt(pix uint32) (uint32, int, int, error) {
	var f [208]byte
	*(*uint32)(unsafe.Pointer(&f[0])) = v4l2BufTypeCapture
	*(*uint32)(unsafe.Pointer(&f[8])) = uint32(s.w)   // pix_mp.width（union 基址 8）
	*(*uint32)(unsafe.Pointer(&f[12])) = uint32(s.h)  // pix_mp.height
	*(*uint32)(unsafe.Pointer(&f[16])) = pix          // pix_mp.pixelformat
	*(*uint32)(unsafe.Pointer(&f[20])) = v4l2FieldAny // pix_mp.field
	f[8+180] = 1                                      // pix_mp.num_planes（实测 180 相对 pix_mp）
	if err := ioctl(s.fd, vidiocSFmt, unsafe.Pointer(&f[0])); err != nil {
		return 0, 0, 0, err
	}
	s.w = int(*(*uint32)(unsafe.Pointer(&f[8])))
	s.h = int(*(*uint32)(unsafe.Pointer(&f[12])))
	got := *(*uint32)(unsafe.Pointer(&f[16]))
	stride := int(*(*uint32)(unsafe.Pointer(&f[8+20+4]))) // plane_fmt[0].bytesperline
	size := int(*(*uint32)(unsafe.Pointer(&f[8+20])))     // plane_fmt[0].sizeimage
	return got, stride, size, nil
}

func newV4l2Source(dev string, w, h int) (*v4l2Source, error) {
	fd, err := syscall.Open(dev, syscall.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %v", dev, err)
	}
	s := &v4l2Source{fd: fd, w: w, h: h}
	for i := range s.expFds {
		s.expFds[i] = -1
	}

	// 优先 NV12（rkisp mainpath 默认格式，RGA 可直接转 RGB）
	got, stride, size, err := s.setFmt(pixFmtNV12)
	if err == nil && got == pixFmtNV12 {
		s.rgaFmt = rkFmtNV12
	} else {
		got, stride, size, err = s.setFmt(pixFmtYUYV)
		if err != nil || got != pixFmtYUYV {
			syscall.Close(fd)
			return nil, fmt.Errorf("no usable pixfmt (NV12/YUYV): %v got=%08x", err, got)
		}
		s.rgaFmt = rkFmtYUYV
	}
	s.stride = stride
	s.srcBytes = size
	fmt.Printf("v4l2 fmt=%08x %dx%d stride=%d size=%d\n", got, s.w, s.h, stride, size)

	// 请求 MMAP buffer
	var req [20]byte
	*(*uint32)(unsafe.Pointer(&req[0])) = v4l2NBuf
	*(*uint32)(unsafe.Pointer(&req[4])) = v4l2BufTypeCapture
	*(*uint32)(unsafe.Pointer(&req[8])) = v4l2MemoryMmap
	if err := ioctl(s.fd, vidiocReqbufs, unsafe.Pointer(&req[0])); err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("reqbufs: %v", err)
	}
	nbuf := int(*(*uint32)(unsafe.Pointer(&req[0])))
	if nbuf < 1 {
		syscall.Close(fd)
		return nil, fmt.Errorf("reqbufs returned %d", nbuf)
	}

	s.useExp = true
	for i := 0; i < v4l2NBuf && i < nbuf; i++ {
		// QUERYBUF 拿 offset/length（mmap 回退用）
		var b [vbSize]byte
		*(*uint32)(unsafe.Pointer(&b[vbIndex])) = uint32(i)
		*(*uint32)(unsafe.Pointer(&b[vbType])) = v4l2BufTypeCapture
		*(*uint32)(unsafe.Pointer(&b[vbMemory])) = v4l2MemoryMmap
		var planes [1]v4l2Plane
		*(*uintptr)(unsafe.Pointer(&b[vbMPlanes])) = uintptr(unsafe.Pointer(&planes[0]))
		*(*uint32)(unsafe.Pointer(&b[vbLength])) = 1
		if err := ioctl(s.fd, vidiocQuerybuf, unsafe.Pointer(&b[0])); err != nil {
			syscall.Close(fd)
			return nil, fmt.Errorf("querybuf %d: %v", i, err)
		}
		s.lengths[i] = planes[0].length
		off := planes[0].mOffset

		// EXPBUF 导出 dma-buf fd（零拷贝路径）
		var eb [64]byte
		*(*uint32)(unsafe.Pointer(&eb[0])) = v4l2BufTypeCapture
		*(*uint32)(unsafe.Pointer(&eb[4])) = uint32(i) // index
		*(*uint32)(unsafe.Pointer(&eb[8])) = 0         // plane
		*(*uint32)(unsafe.Pointer(&eb[12])) = dmaFdFlags
		if err := ioctl(s.fd, vidiocExpbuf, unsafe.Pointer(&eb[0])); err != nil {
			s.useExp = false
		} else {
			s.expFds[i] = int32(*(*uint32)(unsafe.Pointer(&eb[16])))
		}

		// mmap（回退路径 / 调试用）
		m, merr := syscall.Mmap(fd, int64(off), int(s.lengths[i]),
			syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
		if merr == nil {
			s.maps[i] = m
		}

		// 初始 QBUF
		var qb [vbSize]byte
		*(*uint32)(unsafe.Pointer(&qb[vbIndex])) = uint32(i)
		*(*uint32)(unsafe.Pointer(&qb[vbType])) = v4l2BufTypeCapture
		*(*uint32)(unsafe.Pointer(&qb[vbMemory])) = v4l2MemoryMmap
		*(*uintptr)(unsafe.Pointer(&qb[vbMPlanes])) = uintptr(unsafe.Pointer(&planes[0]))
		*(*uint32)(unsafe.Pointer(&qb[vbLength])) = 1
		if err := ioctl(s.fd, vidiocQbuf, unsafe.Pointer(&qb[0])); err != nil {
			syscall.Close(fd)
			return nil, fmt.Errorf("qbuf %d: %v", i, err)
		}
	}
	if s.useExp {
		fmt.Println("v4l2: EXPBUF dma-buf zero-copy")
	} else {
		fmt.Println("v4l2: EXPBUF unavailable, mmap+copy fallback")
	}

	// STREAMON（阻塞模式 DQBUF 等帧）
	on := int32(v4l2BufTypeCapture)
	if err := ioctl(s.fd, vidiocStreamon, unsafe.Pointer(&on)); err != nil {
		syscall.Close(fd)
		return nil, fmt.Errorf("streamon: %v", err)
	}
	s.streaming = true
	return s, nil
}

func (s *v4l2Source) newBuf() *frameBuf {
	return &frameBuf{data: make([]byte, 8), fd: -1, rfmt: s.rgaFmt, w: s.w, h: s.h}
}

func (s *v4l2Source) next(fb *frameBuf) error {
	var b [vbSize]byte
	*(*uint32)(unsafe.Pointer(&b[vbType])) = v4l2BufTypeCapture
	*(*uint32)(unsafe.Pointer(&b[vbMemory])) = v4l2MemoryMmap
	var planes [1]v4l2Plane
	*(*uintptr)(unsafe.Pointer(&b[vbMPlanes])) = uintptr(unsafe.Pointer(&planes[0]))
	*(*uint32)(unsafe.Pointer(&b[vbLength])) = 1
	if err := ioctl(s.fd, vidiocDqbuf, unsafe.Pointer(&b[0])); err != nil {
		return fmt.Errorf("dqbuf: %v", err)
	}
	fb.aux = int(*(*uint32)(unsafe.Pointer(&b[vbIndex])))
	return nil
}

func (s *v4l2Source) prep(fb *frameBuf, set int) (float32, int, int) {
	i := fb.aux
	if i < 0 || i >= v4l2NBuf {
		return 1, 0, 0
	}
	ws := s.stride
	if s.rgaFmt == rkFmtYUYV { ws /= 2 } // RGA stride is pixels, V4L2 stride is bytes
	if s.useExp && s.expFds[i] >= 0 {
		// 零拷贝：RGA 直接读摄像头 dma-buf（几何不变，灰边只填一次）
		doFill := !s.filled[set]
		if sc, px, py, ok := rgaLetterboxFd(set, doFill, s.expFds[i], s.rgaFmt, s.w, s.h, ws, s.h, s.srcBytes); ok {
			s.filled[set] = true
			return sc, px, py
		}
	}
	// 回退：拷进 RGA 源 buffer 再 blit
	if s.maps[i] != nil && len(s.maps[i]) >= s.srcBytes && s.srcBytes <= len(rgaSrcBuf) {
		rgaMu.Lock()
		copy(rgaSrcBuf, s.maps[i][:s.srcBytes])
		sc, px, py, ok := blitInto(set, true, rgaSrcFd, s.rgaFmt, s.w, s.h, ws, s.h, s.srcBytes)
		rgaMu.Unlock()
		if ok {
			if !inZeroCopy {
				copy(inputVirts[set], rgaDstBufs[set][:inAttr.size])
			}
			return sc, px, py
		}
	}
	return 1, 0, 0
}

func (s *v4l2Source) release(fb *frameBuf) {
	i := fb.aux
	if i < 0 || i >= v4l2NBuf {
		return
	}
	var qb [vbSize]byte
	*(*uint32)(unsafe.Pointer(&qb[vbIndex])) = uint32(i)
	*(*uint32)(unsafe.Pointer(&qb[vbType])) = v4l2BufTypeCapture
	*(*uint32)(unsafe.Pointer(&qb[vbMemory])) = v4l2MemoryMmap
	var planes [1]v4l2Plane
	*(*uintptr)(unsafe.Pointer(&qb[vbMPlanes])) = uintptr(unsafe.Pointer(&planes[0]))
	*(*uint32)(unsafe.Pointer(&qb[vbLength])) = 1
	ioctl(s.fd, vidiocQbuf, unsafe.Pointer(&qb[0]))
}

func (s *v4l2Source) close() error {
	if s.streaming {
		off := int32(v4l2BufTypeCapture)
		ioctl(s.fd, vidiocStreamoff, unsafe.Pointer(&off))
	}
	for i := range s.maps {
		if s.maps[i] != nil {
			syscall.Munmap(s.maps[i])
		}
		if s.expFds[i] >= 0 {
			syscall.Close(int(s.expFds[i]))
		}
	}
	return syscall.Close(s.fd)
}

// preview converts the exact dequeued frame while it is still owned by this job.
// QBUF must happen only after inference AND this read have completed.
func (s *v4l2Source) preview(fb *frameBuf) bool {
	i := fb.aux
	if i < 0 || i >= v4l2NBuf {
		return false
	}
	rgaMu.Lock()
	defer rgaMu.Unlock()
	fd := s.expFds[i]
	if !s.useExp || fd < 0 {
		if len(s.maps[i]) < s.srcBytes || len(rgaSrcBuf) < s.srcBytes {
			return false
		}
		copy(rgaSrcBuf, s.maps[i][:s.srcBytes])
		fd = rgaSrcFd
	}
	ws := s.stride
	if s.rgaFmt == rkFmtYUYV {
		ws /= 2
	}
	return rgaConvertToBGR(fd, s.rgaFmt, s.w, s.h, ws, s.h, s.srcBytes)
}
