package main

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"
)

func TestRecordedPoseCoordinatesMatchPreview(t *testing.T) {
	var buf bytes.Buffer
	p := &poseSeqWriter{w: &buf, vw: 640, vh: 360, kpTh: 0.3}
	r := &demoRec{active: true, poseW: p}
	// Model center maps to the center of 1280x720, and of the 640x360 recording.
	d := detection{conf: 0.9, x1: 104, y1: 149.5, x2: 312, y2: 266.5}
	for k := range d.kp {
		d.kp[k] = [3]float32{208, 208, 0.9}
	}
	r.writePoseFrame(1, []detection{d}, 416.0/1280, 0, 91, 1280, 720)
	var f poseFrameJSON
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Targets) != 1 {
		t.Fatal("missing person")
	}
	for axis := 0; axis < 2; axis++ {
		got := f.Targets[0].Kps[0][axis].(float64)
		if math.Abs(got-0.5) > 0.0001 {
			t.Errorf("normalized center axis %d = %f, want 0.5", axis, got)
		}
	}
}

func TestPortraitRecordedPoseCoordinatesMatchPreview(t *testing.T) {
	var buf bytes.Buffer
	p := &poseSeqWriter{w: &buf, vw: 360, vh: 640, kpTh: 0.3}
	r := &demoRec{active: true, poseW: p}
	// Rotated 720x1280 center letterboxes to model center with side padding.
	d := detection{conf: 0.9}
	for k := range d.kp {
		d.kp[k] = [3]float32{208, 208, 0.9}
	}
	r.writePoseFrame(1, []detection{d}, 416.0/1280, 91, 0, 720, 1280)
	var f poseFrameJSON
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &f); err != nil {
		t.Fatal(err)
	}
	for axis := 0; axis < 2; axis++ {
		got := f.Targets[0].Kps[0][axis].(float64)
		if math.Abs(got-0.5) > 0.0001 {
			t.Errorf("portrait normalized center axis %d = %f, want 0.5", axis, got)
		}
	}
}
