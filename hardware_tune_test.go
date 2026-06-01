package main

import (
	"runtime"
	"testing"

	"hackme/internal/gpuhost"
)

func TestBuildHardwareTuneDevicesNotNull(t *testing.T) {
	a := &app{}
	resp := a.buildHardwareTuneResponse()
	if resp.Devices == nil {
		t.Fatal("devices must be non-nil slice for JSON []")
	}
}

func TestBuildHardwareTuneAMDTelemetryLive(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}
	if len(gpuhost.ListAMDGPUTelemetry()) == 0 {
		t.Skip("no amdgpu on host")
	}
	a := &app{}
	resp := a.buildHardwareTuneResponse()
	if !resp.AMDTelemetry {
		t.Fatal("expected amd_telemetry true")
	}
	if len(resp.Devices) != 1 {
		t.Fatalf("devices=%d want 1", len(resp.Devices))
	}
	if resp.CanSetPowerLimit {
		t.Fatal("AMD host should not allow PL via API")
	}
	if resp.PresetsAvailable {
		t.Fatal("AMD should not expose NVIDIA preset buttons")
	}
	if resp.Devices[0].Hints.Family == "Unknown" {
		t.Fatalf("hints family=%q for %q", resp.Devices[0].Hints.Family, resp.Devices[0].Name)
	}
}
