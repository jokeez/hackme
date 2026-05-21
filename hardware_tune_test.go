package main

import (
	"testing"

	"hackme/internal/gputune"
)

func TestFinalizeTuneDevicePresets(t *testing.T) {
	dev := gpuTuneDevice{
		Name:  "NVIDIA GeForce RTX 5060 Ti",
		Hints: gputune.ForGPUName("NVIDIA GeForce RTX 5060 Ti"),
	}
	finalizeTuneDevice(&dev, 0, true)
	if !dev.PresetsAvailable {
		t.Fatal("expected presets available from hints TDP")
	}
	if dev.PresetDailyW <= 0 || dev.PresetEcoW <= 0 {
		t.Fatalf("presets: eco=%v daily=%v", dev.PresetEcoW, dev.PresetDailyW)
	}
	if dev.ManualOC.Vendor == "" {
		t.Fatal("expected manual OC from rig profile detect")
	}
	if dev.NvidiaSMIPlCommand == "" {
		t.Fatal("expected nvidia-smi pl command hint")
	}
}
