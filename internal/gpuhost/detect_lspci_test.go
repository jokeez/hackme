//go:build linux

package gpuhost

import (
	"strings"
	"testing"
)

func TestLspciGPUNamesPreservesRadeonBracket(t *testing.T) {
	out := "00:02.0 Host bridge: Intel Corporation Device 1234\n" +
		"01:00.0 VGA compatible controller: Advanced Micro Devices, Inc. [AMD/ATI] Ellesmere [Radeon RX 470/480/570/570X/580/580X/590] (rev e7)\n" +
		"01:00.1 Audio device: Advanced Micro Devices, Inc. [AMD/ATI] Ellesmere HDMI Audio [Radeon RX 470/480 / 570/580/590]\n"
	names := parseLspciGPUNames(out)
	if len(names) != 1 {
		t.Fatalf("want 1 GPU, got %v", names)
	}
	low := strings.ToLower(names[0])
	if !strings.Contains(low, "radeon rx 470/480/570/570x/580/580x/590") {
		t.Fatalf("Radeon bracket lost: %q", names[0])
	}
	if strings.Contains(low, "rev e7") {
		t.Fatalf("rev suffix kept: %q", names[0])
	}
}

func TestLspciGPUNamesHostSmoke(t *testing.T) {
	// Real host lspci: only checks that parsing does not drop every GPU line.
	for _, n := range lspciGPUNames() {
		if strings.TrimSpace(n) == "" {
			t.Fatalf("empty GPU name in %v", lspciGPUNames())
		}
	}
}
