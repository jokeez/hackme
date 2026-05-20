//go:build cuda

package gpupoh

import (
	"os"
	"testing"
)

// Integration: requires NVIDIA GPU + driver. Skips when no devices.
func TestDiscoverAcceleratorsCUDA(t *testing.T) {
	if os.Getenv("HACKME_GPU_INTEGRATION") == "0" {
		t.Skip("HACKME_GPU_INTEGRATION=0")
	}
	devs, err := GetAllGPUDevices()
	if err != nil {
		t.Fatal(err)
	}
	if len(devs) == 0 {
		t.Skip("no CUDA GPUs visible")
	}
	for _, d := range devs {
		if d.Backend != "cuda" {
			t.Fatalf("device %d backend=%q want cuda", d.Index, d.Backend)
		}
	}
	accs, err := DiscoverAccelerators()
	if err != nil {
		t.Fatal(err)
	}
	if len(accs) == 0 {
		t.Fatal("DiscoverAccelerators: empty")
	}
	if len(accs) != len(devs) {
		t.Logf("warning: listed=%d initialized=%d (some devices may fail NVRTC init)", len(devs), len(accs))
	}
	for _, a := range accs {
		if a.Backend() != "cuda" {
			t.Fatalf("backend=%q", a.Backend())
		}
		_ = a.Close()
	}
}
