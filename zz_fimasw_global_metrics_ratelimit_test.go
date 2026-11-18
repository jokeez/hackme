package main

import (
	"os"
	"strings"
	"testing"
)

// fimasw 2026-11-18: handleGlobalMetrics must rate-limit like 1d98e1a sibling relays.
func TestFimaswGlobalMetricsHasAllowRate(t *testing.T) {
	src, err := os.ReadFile("pool.go")
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(string(src), "func (a *app) handleGlobalMetrics(")
	if i < 0 {
		t.Fatal("handleGlobalMetrics missing")
	}
	body := string(src)[i:]
	if j := strings.Index(body, "\nfunc "); j > 0 {
		body = body[:j]
	}
	if !strings.Contains(body, "allowRate") {
		t.Fatal("handleGlobalMetrics missing allowRate (relay amplification)")
	}
	if !strings.Contains(body, "fetchCoordinatorStats") || !strings.Contains(body, "fetchCoordinatorWorkStats") {
		t.Fatal("expected coordinator fan-out still present")
	}
}
