package main

import (
	"os"
	"testing"
	"unsafe"
)

// Exercise the actual NPU and preview blit call sites.
func TestBlitRotationAndGeometry(t *testing.T) {
	oldBlit, oldRot, oldOK := rgaLibBlit, rgaRotation, rgaOK
	oldW, oldH := rgaPreviewW, rgaPreviewH
	defer func() {
		rgaLibBlit, rgaRotation, rgaOK = oldBlit, oldRot, oldOK
		rgaPreviewW, rgaPreviewH = oldW, oldH
	}()
	rgaRotation, rgaOK = 0x04, true
	rgaPreviewW, rgaPreviewH = 360, 640
	calls := 0
	rgaLibBlit = func(src, dst, pat unsafe.Pointer) int32 {
		calls++
		read := func(p unsafe.Pointer, off int) int32 { return *(*int32)(unsafe.Add(p, off)) }
		if got := read(src, 72); got != 4 {
			t.Errorf("source rotation=%d, want HAL_TRANSFORM_ROT_90=4", got)
		}
		if got := read(dst, 72); got != 0 {
			t.Errorf("destination rotation=%d, want 0", got)
		}
		if calls == 1 && (read(dst, riRect+8) != 234 || read(dst, riRect+12) != 416 || read(dst, riRect) != 91) {
			t.Error("rotated 9:16 input must letterbox to 234x416 at x=91")
		}
		if calls == 2 && (read(dst, riRect+8) != 360 || read(dst, riRect+12) != 640) {
			t.Error("portrait preview must be 360x640")
		}
		return 0
	}
	blitInto(0, false, 9, rkFmtNV12, 1280, 720, 1280, 720, 1382400)
	rgaConvertToBGR(9, rkFmtNV12, 1280, 720, 1280, 720, 1382400)
	if calls != 2 {
		t.Fatalf("blits=%d", calls)
	}
}

// AURA_RGA_TEST=1: four quadrants detect wrong rotations through real hardware.
func TestHardwareRotation(t *testing.T) {
	if os.Getenv("AURA_RGA_TEST") != "1" {
		t.Skip("requires Aura RGA")
	}
	rgaRotation = 0x04
	if err := rgaSetup(); err != nil {
		t.Fatal(err)
	}
	colors := [4][3]byte{{0, 0, 255}, {0, 255, 0}, {255, 0, 0}, {255, 255, 255}}
	for y := 0; y < 720; y++ {
		for x := 0; x < 1280; x++ {
			c := colors[(y/360)*2+x/640]
			copy(rgaSrcBuf[(y*1280+x)*3:], c[:])
		}
	}
	if _, _, _, ok := blitInto(0, true, rgaSrcFd, rkFmtBGR888, 1280, 720, 1280, 720, 1280*720*3); !ok {
		t.Fatal("NPU blit failed")
	}
	if !rgaConvertToBGR(rgaSrcFd, rkFmtBGR888, 1280, 720, 1280, 720, 1280*720*3) {
		t.Fatal("preview blit failed")
	}
	// Clockwise: output TL/TR/BL/BR = source BL/TL/BR/TR.
	wants := [][3]byte{colors[2], colors[0], colors[3], colors[1]}
	for i, p := range [][2]int{{149, 104}, {266, 104}, {149, 312}, {266, 312}} {
		want := wants[i]
		at := (p[1]*416 + p[0]) * 3
		for k := 0; k < 3; k++ {
			if got := rgaDstBufs[0][at+k]; got != want[2-k] {
				t.Errorf("NPU quadrant %d channel %d=%d want %d", i, k, got, want[2-k])
			}
		}
	}
	for i, p := range [][2]int{{90, 160}, {270, 160}, {90, 480}, {270, 480}} {
		want := wants[i]
		at := (p[1]*360 + p[0]) * 3
		for k := 0; k < 3; k++ {
			if got := rgaAnnoBuf[at+k]; got != want[k] {
				t.Errorf("preview quadrant %d channel %d=%d want %d", i, k, got, want[k])
			}
		}
	}
}
