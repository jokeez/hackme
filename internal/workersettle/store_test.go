package workersettle

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateApplyPendingRoundTrip(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "worker_settlement_state.json")
	raw := []byte(`{
  "workers": {
    "w1": {"settled_hmc": 1.5, "payout_address": "HMC-abc", "pending_settle": {"delta_hmc": 0.25, "payout_address": "HMC-abc", "nonce": 9}}
  },
  "meta": {"last_force_unix": 42}
}`)
	if err := os.WriteFile(jsonPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "worker_settlement.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	ok, err := s.MigrateFromJSON(ctx, jsonPath)
	if err != nil || !ok {
		t.Fatalf("migrate: ok=%v err=%v", ok, err)
	}
	ok2, err := s.MigrateFromJSON(ctx, jsonPath)
	if err != nil || ok2 {
		t.Fatalf("second migrate must be no-op: ok=%v err=%v", ok2, err)
	}
	w, err := s.GetWorker(ctx, "w1")
	if err != nil {
		t.Fatal(err)
	}
	if w.SettledHMC != 1.5 || w.PayoutAddress != "HMC-abc" || w.PendingJSON == "" {
		t.Fatalf("row=%+v", w)
	}
	meta, err := s.GetMeta(ctx)
	if err != nil || meta.LastForceUnix != 42 {
		t.Fatalf("meta=%+v err=%v", meta, err)
	}

	id, err := s.SetPending(ctx, "w2", "HMC-xyz", 0.01, 3)
	if err != nil || id <= 0 {
		t.Fatalf("pending id=%d err=%v", id, err)
	}
	if err := s.AttachTxHash(ctx, "w2", "deadbeef"); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplySettle(ctx, "w2", "HMC-xyz", "deadbeef", 0.01, 0, true); err != nil {
		t.Fatal(err)
	}
	w2, err := s.GetWorker(ctx, "w2")
	if err != nil {
		t.Fatal(err)
	}
	if w2.SettledHMC != 0.01 || w2.PendingJSON != "" || w2.LastTxHash != "deadbeef" {
		t.Fatalf("after apply: %+v", w2)
	}
	n, err := s.CountPendingOutbox(ctx)
	if err != nil || n != 0 {
		// w1 migrated pending is only on worker row, not outbox; w2 applied
		t.Fatalf("pending outbox=%d err=%v", n, err)
	}
	exp, err := s.ExportJSON(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(exp, &doc); err != nil {
		t.Fatal(err)
	}
	workers, _ := doc["workers"].(map[string]any)
	if workers["w1"] == nil || workers["w2"] == nil {
		t.Fatalf("export missing workers: %s", exp)
	}
}

func TestExportJSONPreservesSUPAndRefusesEmptyWipe(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "state.json")
	raw := []byte(`{
  "workers": {
    "w1": {"settled_hmc": 2.0, "settled_sup": 5.5, "pending_sup": {"delta_sup": 0.1}, "payout_address": "HMC-a"},
    "sup-only": {"settled_sup": 1.0, "payout_address": "HMC-b"}
  },
  "meta": {"last_force_unix": 7}
}`)
	if err := os.WriteFile(jsonPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "s.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if _, err := s.MigrateFromJSON(ctx, jsonPath); err != nil {
		t.Fatal(err)
	}
	// HMC apply must not wipe settled_sup.
	if err := s.ApplySettle(ctx, "w1", "HMC-a", "tx1", 3.0, 0, true); err != nil {
		t.Fatal(err)
	}
	// Simulate SUP advancing watermark in JSON only (old bug path), then export merge.
	bumped := []byte(`{
  "workers": {
    "w1": {"settled_hmc": 2.0, "settled_sup": 9.0, "pending_sup": {"delta_sup": 0.2}, "payout_address": "HMC-a"},
    "sup-only": {"settled_sup": 1.0, "payout_address": "HMC-b"}
  },
  "meta": {"last_force_unix": 7}
}`)
	if err := os.WriteFile(jsonPath, bumped, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.ExportJSONToFile(ctx, jsonPath); err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	workers := doc["workers"].(map[string]any)
	w1 := workers["w1"].(map[string]any)
	if anyFloat(w1["settled_hmc"]) != 3.0 {
		t.Fatalf("settled_hmc want 3 got %v", w1["settled_hmc"])
	}
	if anyFloat(w1["settled_sup"]) != 9.0 {
		t.Fatalf("settled_sup must keep JSON max 9, got %v", w1["settled_sup"])
	}
	if w1["pending_sup"] == nil {
		t.Fatal("pending_sup must be preserved")
	}
	if workers["sup-only"] == nil {
		t.Fatal("sup-only worker must survive HMC export")
	}
	row, _ := s.GetWorker(ctx, "w1")
	if row.SettledSUP < 9.0 {
		t.Fatalf("SQLite settled_sup should sync up to 9, got %v", row.SettledSUP)
	}

	// Empty ledger must not wipe non-empty JSON.
	s2, err := Open(filepath.Join(dir, "empty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	emptyJSON := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(emptyJSON, []byte(`{"workers":{},"meta":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.MigrateFromJSON(ctx, emptyJSON); err != nil {
		t.Fatal(err)
	}
	if err := s2.ExportJSONToFile(ctx, jsonPath); err == nil {
		t.Fatal("expected refuse empty export over non-empty JSON")
	}
}

func TestMigrateMissingJSONRefuses(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = s.MigrateFromJSON(context.Background(), filepath.Join(dir, "nope.json"))
	if err == nil {
		t.Fatal("expected error on missing migrate json")
	}
	ok, err := s.MigratedFromJSON(context.Background())
	if err != nil || ok {
		t.Fatalf("must not mark migrated: ok=%v err=%v", ok, err)
	}
}

func TestClearPending(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if _, err := s.SetPending(ctx, "w", "HMC-a", 1.0, 1); err != nil {
		t.Fatal(err)
	}
	n, err := s.CountPendingOutbox(ctx)
	if err != nil || n != 1 {
		t.Fatalf("pending outbox=%d err=%v", n, err)
	}
	if err := s.ClearPending(ctx, "w"); err != nil {
		t.Fatal(err)
	}
	w, _ := s.GetWorker(ctx, "w")
	if w.PendingJSON != "" {
		t.Fatalf("pending still set: %q", w.PendingJSON)
	}
	n, err = s.CountPendingOutbox(ctx)
	if err != nil || n != 0 {
		t.Fatalf("outbox must be abandoned: pending=%d err=%v", n, err)
	}
}

func TestSessionAnchorSUP(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.SetSettledSUP(ctx, "w", 9.0, "HMC-a"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSessionAnchorSUP(ctx, "w", 0.5, false); err != nil {
		t.Fatal(err)
	}
	w, err := s.GetWorker(ctx, "w")
	if err != nil {
		t.Fatal(err)
	}
	if w.SettledSUP != 9.0 || w.SessionAnchorSUP != 0.5 {
		t.Fatalf("row=%+v", w)
	}
	if err := s.SetSessionAnchorSUP(ctx, "w", 0, true); err != nil {
		t.Fatal(err)
	}
	w, _ = s.GetWorker(ctx, "w")
	if w.SessionAnchorSUP != 0 {
		t.Fatalf("anchor not cleared: %+v", w)
	}
}

func TestSessionAnchorHMC(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.ApplySettle(ctx, "w", "HMC-a", "tx1", 5.0, 5.0, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSessionAnchorHMC(ctx, "w", 0.0, false); err != nil {
		t.Fatal(err)
	}
	w, err := s.GetWorker(ctx, "w")
	if err != nil {
		t.Fatal(err)
	}
	if w.SettledHMC != 5.0 || w.SessionAnchorHMC != 0.0 {
		t.Fatalf("row=%+v", w)
	}
	if err := s.SetSessionAnchorHMC(ctx, "w", 1.25, false); err != nil {
		t.Fatal(err)
	}
	w, _ = s.GetWorker(ctx, "w")
	if w.SessionAnchorHMC != 1.25 || w.SessionAnchorUnits != HMCToUnits(1.25) || w.SettledHMC != 5.0 {
		t.Fatalf("re-anchor lost settled: %+v", w)
	}
	if err := s.SetSessionAnchorHMC(ctx, "w", 0, true); err != nil {
		t.Fatal(err)
	}
	w, _ = s.GetWorker(ctx, "w")
	if w.SessionAnchorHMC != 0 || w.SessionAnchorUnits != 0 || w.SettledHMC != 5.0 {
		t.Fatalf("clear anchor must zero units+float, keep settled: %+v", w)
	}
}

func TestTxHashKnown(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if k, err := s.TxHashKnown(ctx, "aa"); err != nil || k {
		t.Fatalf("empty store: %v %v", k, err)
	}
	if _, err := s.SetPendingUnits(ctx, "w1", "HMC-to", 100, 7); err != nil {
		t.Fatal(err)
	}
	if err := s.AttachTxHash(ctx, "w1", "aa"); err != nil {
		t.Fatal(err)
	}
	if k, err := s.TxHashKnown(ctx, "aa"); err != nil || !k {
		t.Fatalf("attached hash must be known: %v %v", k, err)
	}
	if k, _ := s.TxHashKnown(ctx, "bb"); k {
		t.Fatal("unknown hash reported known")
	}
}
