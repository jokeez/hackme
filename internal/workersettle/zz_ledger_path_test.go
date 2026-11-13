package workersettle

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// The settler must never derive a second ledger beside STATE_FILE: a derived DB has zero
// watermarks and re-pays accrual the main ledger already settled (~1153 HMC, 2026-10/11).
func TestLedgerNeverDerivedFromStateFile(t *testing.T) {
	t.Setenv("STATE_DB", "")
	t.Setenv("HACKME_WORKER_SETTLEMENT_DB", "")
	t.Setenv("SETTLE_SQLITE", "1")
	dir := t.TempDir()
	cfg := Config{StateJSON: filepath.Join(dir, "worker_settlement_state.json"), AdminToken: "a", CoordAdminToken: "c"}
	cfg.ResolveStateDB()
	if cfg.StateDB != "" {
		t.Fatalf("derived ledger %q from STATE_FILE", cfg.StateDB)
	}
	for name, run := range map[string]func(context.Context, Config) (Result, error){"hmc": Run, "sup": RunSUP} {
		if _, err := run(context.Background(), cfg); err == nil || !strings.Contains(err.Error(), "STATE_DB required") {
			t.Fatalf("%s: want STATE_DB refusal, got %v", name, err)
		}
	}
	// Relative paths would name a different ledger (and lock) per working directory.
	rel := Config{StateJSON: "data/worker_settlement_state.json", StateDB: "data/worker_settlement.db", AdminToken: "a", CoordAdminToken: "c"}
	if _, err := Run(context.Background(), rel); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("relative ledger must be refused, got %v", err)
	}
	t.Setenv("HACKME_WORKER_SETTLEMENT_DB", filepath.Join(dir, "worker_settlement.db"))
	cfg2 := Config{StateJSON: cfg.StateJSON}
	cfg2.ResolveStateDB()
	if cfg2.StateDB != filepath.Join(dir, "worker_settlement.db") {
		t.Fatalf("explicit ledger not used: %q", cfg2.StateDB)
	}
}
