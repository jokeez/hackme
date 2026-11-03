package workersettle

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestSUPPendingAttachApplyRoundTrip(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "sup.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	if err := s.SetPendingSUPUnits(ctx, "w-sup", "HMC-pay", 250_00000, 11); err != nil {
		t.Fatal(err)
	}
	// Second intent while pending exists must be refused (double-pay guard).
	if err := s.SetPendingSUPUnits(ctx, "w-sup", "HMC-pay", 10_00000, 12); err == nil {
		t.Fatal("expected second SetPendingSUPUnits to fail while pending set")
	}
	if err := s.AttachSUPTxHash(ctx, "w-sup", "sup-tx-one"); err != nil {
		t.Fatal(err)
	}
	w, err := s.GetWorker(ctx, "w-sup")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.PendingSUPJSON, "sup-tx-one") {
		t.Fatalf("pending_sup_json missing tx_hash: %q", w.PendingSUPJSON)
	}
	if err := s.ApplySettleSUPUnits(ctx, "w-sup", "HMC-pay", "sup-tx-one", 250_00000, 0, true); err != nil {
		t.Fatal(err)
	}
	w, err = s.GetWorker(ctx, "w-sup")
	if err != nil {
		t.Fatal(err)
	}
	if w.SettledSUPUnits != 250_00000 || strings.TrimSpace(w.PendingSUPJSON) != "" {
		t.Fatalf("after SUP apply: %+v", w)
	}
	var kind string
	var units uint64
	if err := s.db.QueryRow(`SELECT kind, units FROM settle_ledger WHERE worker_id='w-sup' ORDER BY id DESC LIMIT 1`).
		Scan(&kind, &units); err != nil {
		t.Fatal(err)
	}
	if kind != "sup_payout_applied" || units != 250_00000 {
		t.Fatalf("ledger kind=%s units=%d", kind, units)
	}
	var applied, pending int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM settle_sup_outbox WHERE status='applied'`).Scan(&applied)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM settle_sup_outbox WHERE status='pending'`).Scan(&pending)
	if applied != 1 || pending != 0 {
		t.Fatalf("sup outbox applied=%d pending=%d want 1/0", applied, pending)
	}
}

func TestSUPPendingClearAllowsRetry(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "sup-clear.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.SetPendingSUPUnits(ctx, "w", "HMC-a", 1_00000, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.ClearPendingSUP(ctx, "w"); err != nil {
		t.Fatal(err)
	}
	var abandoned int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM settle_sup_outbox WHERE status='abandoned'`).Scan(&abandoned)
	if abandoned != 1 {
		t.Fatalf("abandoned outbox=%d want 1", abandoned)
	}
	if err := s.SetPendingSUPUnits(ctx, "w", "HMC-a", 2_00000, 2); err != nil {
		t.Fatalf("after clear: %v", err)
	}
}

func TestSUPApplyRefusesWatermarkRegression(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "sup-wm.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.SetPendingSUPUnits(ctx, "w", "HMC-a", 100_00000, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.AttachSUPTxHash(ctx, "w", "tx-a"); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplySettleSUPUnits(ctx, "w", "HMC-a", "tx-a", 100_00000, 0, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPendingSUPUnits(ctx, "w", "HMC-a", 50_00000, 2); err != nil {
		t.Fatal(err)
	}
	if err := s.AttachSUPTxHash(ctx, "w", "tx-b"); err != nil {
		t.Fatal(err)
	}
	// Must not allow settled watermark to go backwards.
	if err := s.ApplySettleSUPUnits(ctx, "w", "HMC-a", "tx-b", 50_00000, 0, true); err == nil {
		t.Fatal("expected watermark regression error")
	}
}

func TestSUPSettleDoesNotClobberHMC(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "sup-hmc.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.ApplySettleUnits(ctx, "w", "HMC-a", "hmc-tx", 500_00000, 0, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPendingSUPUnits(ctx, "w", "HMC-a", 80_00000, 9); err != nil {
		t.Fatal(err)
	}
	if err := s.AttachSUPTxHash(ctx, "w", "sup-tx"); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplySettleSUPUnits(ctx, "w", "HMC-a", "sup-tx", 80_00000, 0, true); err != nil {
		t.Fatal(err)
	}
	w, err := s.GetWorker(ctx, "w")
	if err != nil {
		t.Fatal(err)
	}
	if w.SettledUnits != 500_00000 || w.SettledSUPUnits != 80_00000 {
		t.Fatalf("HMC/SUP watermarks crossed: %+v", w)
	}
}
