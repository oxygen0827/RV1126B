package main

import (
	"fmt"
	"os"
)

const fitnessUploadTrigger = "/tmp/fitness_upload.trigger"
const fitnessSaveTrigger = "/tmp/fitness_save.trigger"
const fitnessCueFile = "/tmp/fitness_cue.txt"

var lastFitnessCue string

// publishFitnessCue exposes the same short rule state used by the old DRM HUD
// to the LVGL process without making the two processes share a framebuffer.
func publishFitnessCue(rep int, short string) {
	cue := fmt.Sprintf("REP %d", rep)
	if short != "" {
		cue += " - " + short
	}
	if cue == lastFitnessCue {
		return
	}
	tmp := fitnessCueFile + ".tmp"
	if err := os.WriteFile(tmp, []byte(cue+"\n"), 0o644); err == nil {
		if err := os.Rename(tmp, fitnessCueFile); err == nil {
			lastFitnessCue = cue
		}
	}
}

// pollFitnessUploadTrigger lets the LVGL front-end request the existing
// uploader without sharing a GUI or camera framebuffer with yolosrv.
func pollFitnessUploadTrigger() {
	if _, err := os.Stat(fitnessSaveTrigger); err == nil {
		_ = os.Remove(fitnessSaveTrigger)
		_ = os.Remove(demoLocalOK)
		_ = os.Remove(demoLocalErr)
		if err := demoRecS.launchLocalSave(); err != nil {
			fmt.Println("fitness local save:", err)
			_ = os.WriteFile(demoLocalErr, []byte(err.Error()), 0o644)
		}
	}
	if _, err := os.Stat(fitnessUploadTrigger); err != nil {
		return
	}
	_ = os.Remove(fitnessUploadTrigger)
	if err := demoRecS.launchUpload(); err != nil {
		fmt.Println("fitness upload:", err)
		// The LVGL front-end polls status_err.txt. Always publish a terminal
		// failure state so a trigger/permission error cannot leave it spinning
		// forever in UPLOADING.
		_ = os.WriteFile(demoStatusErr, []byte(err.Error()), 0o644)
	}
}
