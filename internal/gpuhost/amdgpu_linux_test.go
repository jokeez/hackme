//go:build linux

package gpuhost

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListAMDGPUTelemetryLive(t *testing.T) {
	ents, err := os.ReadDir("/sys/class/drm")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if !drmCardNameRe.MatchString(e.Name()) {
			continue
		}
		root := filepath.Join("/sys/class/drm", e.Name(), "device")
		t.Logf("card %s isAmdgpu=%v", e.Name(), isAmdgpuDevice(root))
	}
	list := ListAMDGPUTelemetry()
	t.Logf("amdgpu cards=%d", len(list))
	for _, c := range list {
		t.Logf("  idx=%d card=%s name=%q busy=%.0f temp=%.0f power=%.0f",
			c.Index, c.Card, c.Name, c.BusyPct, c.TempC, c.PowerDrawW)
	}
	if len(list) == 0 {
		t.Skip("no amdgpu sysfs on this host (CI OK)")
	}
	if list[0].Name == "" {
		t.Fatal("expected non-empty GPU name")
	}
}
