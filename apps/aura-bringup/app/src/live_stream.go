package main

// 方案A: 实时骨架叠加 MJPEG 流 (Type-C → adb reverse → 上位机显示)
// yolosrv v4l2 流水线每帧调用 pushAnnotated(); /stream 提供 MJPEG
import (
	"bytes"
	"image"
	"image/jpeg"
	"sync"
)

var (
	mjMu   sync.Mutex
	mjJPEG []byte
	mjSeq  uint64
)

var drawEdges = [][2]int{
	{15, 13}, {13, 11}, {11, 5}, {5, 1}, {1, 0}, {0, 2}, {2, 4}, {4, 6}, {6, 8}, {8, 10},
	{12, 14}, {14, 16}, {11, 12}, {5, 6}, {1, 2}, {1, 3}, {3, 5}, {2, 6}, {6, 12},
}

var digitGlyph = [10][15]int{
	{1, 1, 1, 1, 0, 1, 1, 0, 1, 1, 0, 1, 1, 1, 1},
	{0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1},
	{1, 1, 1, 0, 0, 1, 1, 1, 1, 1, 0, 0, 1, 1, 1},
	{1, 1, 1, 0, 0, 1, 1, 1, 1, 0, 0, 1, 1, 1, 1},
	{1, 0, 1, 1, 0, 1, 1, 1, 1, 0, 0, 1, 0, 0, 1},
	{1, 1, 1, 1, 0, 0, 1, 1, 1, 0, 0, 1, 1, 1, 1},
	{1, 1, 1, 1, 0, 0, 1, 1, 1, 1, 0, 1, 1, 1, 1},
	{1, 1, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1},
	{1, 1, 1, 1, 0, 1, 1, 1, 1, 1, 0, 1, 1, 1, 1},
	{1, 1, 1, 1, 0, 1, 1, 1, 1, 0, 0, 1, 1, 1, 1},
}

func drawGlyph(c []byte, w, h, x0, y0, digit int, r, g, b byte) {
	for yy := 0; yy < 5; yy++ {
		for xx := 0; xx < 3; xx++ {
			if digitGlyph[digit][yy*3+xx] > 0 {
				px, py := x0+xx, y0+yy
				if px >= 0 && py >= 0 && px < w && py < h {
					i := (py*w + px) * 3
					c[i] = b
					c[i+1] = g
					c[i+2] = r
				}
			}
		}
	}
}

func pushAnnotated(annoBGR []byte, w, h int, keep []detection, scale float32, padX, padY int, fps float64, srcW, srcH int) {
	if len(annoBGR) < w*h*3 {
		return
	}
	var c []byte = annoBGR
	mapX := func(v float32) int { return int((v - float32(padX)) / scale * float32(w) / float32(srcW)) }
	mapY := func(v float32) int { return int((v - float32(padY)) / scale * float32(h) / float32(srcH)) }
	put := func(x, y int, r, g, b byte) {
		if x >= 0 && y >= 0 && x < w && y < h {
			i := (y*w + x) * 3
			c[i] = b
			c[i+1] = g
			c[i+2] = r
		}
	}
	line := func(x0, y0, x1, y1 int, r, g, b byte) {
		dx, dy := x1-x0, y1-y0
		n := dx
		if n < 0 {
			n = -dx
		}
		if dy < 0 && -dy > n {
			n = -dy
		}
		if dy > 0 && dy > n {
			n = dy
		}
		if n == 0 {
			n = 1
		}
		for i := 0; i <= n; i++ {
			t := i * 1024 / n
			put(x0+(x1-x0)*t/1024, y0+(y1-y0)*t/1024, r, g, b)
			put(x0+(x1-x0)*t/1024+1, y0+(y1-y0)*t/1024, r, g, b)
		}
	}
	for _, d := range keep {
		ax1 := mapX(d.x1)
		ay1 := mapY(d.y1)
		ax2 := mapX(d.x2)
		ay2 := mapY(d.y2)
		for _, e := range drawEdges {
			a, b2 := e[0], e[1]
			if a < 17 && b2 < 17 && d.kp[a][2] > 0.3 && d.kp[b2][2] > 0.3 {
				line(mapX(d.kp[a][0]), mapY(d.kp[a][1]), mapX(d.kp[b2][0]), mapY(d.kp[b2][1]), 255, 90, 0)
			}
		}
		for k := 0; k < 17; k++ {
			if d.kp[k][2] > 0.3 {
				kx := mapX(d.kp[k][0])
				ky := mapY(d.kp[k][1])
				for dyy := -1; dyy <= 1; dyy++ {
					for dxx := -1; dxx <= 1; dxx++ {
						put(kx+dxx, ky+dyy, 255, 0, 0)
					}
				}
			}
		}
		// 检测框
		line(ax1, ay1, ax2, ay1, 0, 230, 0)
		line(ax1, ay2, ax2, ay2, 0, 230, 0)
		line(ax1, ay1, ax1, ay2, 0, 230, 0)
		line(ax2, ay1, ax2, ay2, 0, 230, 0)
	}
	fps10 := int(fps*10 + 0.5)
	drawGlyph(c, w, h, 4, 3, fps10/100%10, 255, 255, 0)
	drawGlyph(c, w, h, 10, 3, fps10/10%10, 255, 255, 0)
	put(9, 5, 255, 170, 0)
	drawGlyph(c, w, h, 14, 3, fps10%10, 255, 255, 0)

	mjMu.Lock()
	jpg := boardJPEG(annoBGR, w, h)
	if len(jpg) > 0 {
		mjJPEG = jpg
		mjSeq++
	}
	mjMu.Unlock()
}

func boardJPEG(bgr []byte, w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		img.Pix[i*4+0] = bgr[i*3+2]
		img.Pix[i*4+1] = bgr[i*3+1]
		img.Pix[i*4+2] = bgr[i*3+0]
		img.Pix[i*4+3] = 0xff
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 70}); err != nil {
		return nil
	}
	return buf.Bytes()
}
