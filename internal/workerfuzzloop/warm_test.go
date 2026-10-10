package workerfuzzloop

import (
	"context"
	"testing"

	"hackme/internal/sandbox"
)

func TestWarmDigHarnessCompilesMinimalGate(t *testing.T) {
	t.Setenv("HACKME_WORKER_WARM_HARNESS", "")
	warmDigHarness(context.Background(), sandbox.MinimalGateWasmHex)
	// Second call hits compiled cache path.
	warmDigHarness(context.Background(), sandbox.MinimalGateWasmHex)
}

func TestWarmDigHarnessCanDisable(t *testing.T) {
	t.Setenv("HACKME_WORKER_WARM_HARNESS", "0")
	warmDigHarness(context.Background(), "not-hex")
}
