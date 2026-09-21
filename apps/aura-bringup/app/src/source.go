// source.go: 帧源抽象 —— 原始 BGR 文件 / V4L2 摄像头
package main

import (
	"io"
	"os"
)

type frameSource interface {
	newBuf() *frameBuf                              // 分配帧缓冲（文件源优先 dma-buf 直读）
	next(fb *frameBuf) error                        // 取一帧（文件: 读字节; v4l2: DQBUF）
	prep(fb *frameBuf, set int) (float32, int, int) // 预处理进 NPU 输入 set（RGA letterbox）
	release(fb *frameBuf)                           // 推理和预览都完成后归还（v4l2: QBUF）
	close() error
}

// ---------------------------------------------------------------------------
// 原始 BGR 视频文件源（EOF 自动回绕循环；RGA 可用时帧直读 dma-buf，零拷贝）
// ---------------------------------------------------------------------------

type fileSource struct {
	f      *os.File
	w      int
	h      int
	sz     int
	filled [2]bool // 灰边已填充（几何不变，只填一次）
}

func newFileSource(path string, w, h int) (*fileSource, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return &fileSource{f: f, w: w, h: h, sz: w * h * 3}, nil
}

func (s *fileSource) newBuf() *frameBuf {
	if rgaOK && s.sz <= rgaMaxW*rgaMaxH*3 {
		if fd, buf, err := dmaAlloc(s.sz); err == nil {
			return &frameBuf{data: buf[:s.sz], fd: fd, rfmt: rkFmtBGR888, w: s.w, h: s.h}
		}
	}
	return &frameBuf{data: make([]byte, s.sz), fd: -1, rfmt: rkFmtBGR888, w: s.w, h: s.h}
}

func (s *fileSource) next(fb *frameBuf) error {
	_, err := io.ReadFull(s.f, fb.data[:s.sz])
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		if _, serr := s.f.Seek(0, 0); serr != nil {
			return serr
		}
		_, err = io.ReadFull(s.f, fb.data[:s.sz])
	}
	return err
}

func (s *fileSource) prep(fb *frameBuf, set int) (float32, int, int) {
	if fb.fd >= 0 {
		doFill := !s.filled[set]
		if sc, px, py, ok := rgaLetterboxFd(set, doFill, fb.fd, fb.rfmt, s.w, s.h, s.w, s.h, s.sz); ok {
			s.filled[set] = true
			return sc, px, py
		}
	}
	return prepFrame(fb.data[:s.sz], s.w, s.h, set)
}

func (s *fileSource) release(*frameBuf) {}
func (s *fileSource) close() error      { return s.f.Close() }
