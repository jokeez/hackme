//go:build linux

package gpuhost

import (
	"strings"
	"testing"
)

func TestLspciGPUNamesPreservesRadeonBracket(t *testing.T) {
	names := lspciGPUNames()
	if len(names) == 0 {
		t.Skip("no lspci VGA on host")
	}
	combined := strings.ToLower(strings.Join(names, " "))
	if !strings.Contains(combined, "rx 580") && !strings.Contains(combined, "radeon") {
		t.Fatalf("expected Radeon/RX 580 in lspci names, got: %v", names)
	}
}
