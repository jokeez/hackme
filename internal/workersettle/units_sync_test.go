package workersettle

import (
	"context"
	"path/filepath"
	"testing"
)

// Stale float after units clear must not resurrect the watermark (double-pay class).
func TestGetWorkerUnitsAlwaysAuthoritative(t *testing.T) {
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
	// Clear anchor: both float and units must go to 0.
	if err := s.SetSessionAnchorHMC(ctx, "w", 0, true); err != nil {
		t.Fatal(err)
	}
	// Simulate a buggy writer that only cleared float / left stale float.
	if _, err := s.db.Exec(`UPDATE settle_workers SET session_anchor_hmc=5.0 WHERE worker_id='w'`); err != nil {
		t.Fatal(err)
	}
	w, err := s.GetWorker(ctx, "w")
	if err != nil {
		t.Fatal(err)
	}
	if w.SessionAnchorUnits != 0 || w.SessionAnchorHMC != 0 {
		t.Fatalf("cleared anchor must stay zero despite stale float: %+v", w)
	}
}

func TestSetSettledSUPWritesUnits(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.SetSettledSUP(ctx, "w", 5.5, "HMC-a"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSettledSUP(ctx, "w", 9.0, "HMC-a"); err != nil {
		t.Fatal(err)
	}
	w, err := s.GetWorker(ctx, "w")
	if err != nil {
		t.Fatal(err)
	}
	if w.SettledSUPUnits != HMCToUnits(9.0) || w.SettledSUP != 9.0 {
		t.Fatalf("SUP units/float mismatch: %+v", w)
	}
	// Stale float lower than units must not win.
	if _, err := s.db.Exec(`UPDATE settle_workers SET settled_sup=1.0 WHERE worker_id='w'`); err != nil {
		t.Fatal(err)
	}
	w, _ = s.GetWorker(ctx, "w")
	if w.SettledSUP != 9.0 || w.SettledSUPUnits != HMCToUnits(9.0) {
		t.Fatalf("units must win over stale float: %+v", w)
	}
}

func TestSessionAnchorSUPClearAndMirror(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.SetSessionAnchorSUP(ctx, "w", 0.5, false); err != nil {
		t.Fatal(err)
	}
	w, _ := s.GetWorker(ctx, "w")
	if w.SessionAnchorSUPU != HMCToUnits(0.5) || w.SessionAnchorSUP != 0.5 {
		t.Fatalf("SUP anchor mirror: %+v", w)
	}
	if err := s.SetSessionAnchorSUP(ctx, "w", 0, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE settle_workers SET session_anchor_sup=0.5 WHERE worker_id='w'`); err != nil {
		t.Fatal(err)
	}
	w, _ = s.GetWorker(ctx, "w")
	if w.SessionAnchorSUPU != 0 || w.SessionAnchorSUP != 0 {
		t.Fatalf("cleared SUP anchor: %+v", w)
	}
}

func TestApplySettleLedgerDeltaAndOutboxByTx(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	id1, err := s.SetPendingUnits(ctx, "w", "HMC-a", 150_00000, 1)
	if err != nil || id1 <= 0 {
		t.Fatalf("pending1: %d %v", id1, err)
	}
	// Second intent while pending exists must be refused (orphan/double-pay guard).
	if _, err := s.SetPendingUnits(ctx, "w", "HMC-a", 50_00000, 2); err == nil {
		t.Fatal("expected second SetPendingUnits to fail while pending set")
	}
	if err := s.AttachTxHash(ctx, "w", "tx-one"); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplySettleUnits(ctx, "w", "HMC-a", "tx-one", 150_00000, 0, true); err != nil {
		t.Fatal(err)
	}
	var kind string
	var units uint64
	var note string
	if err := s.db.QueryRow(`SELECT kind, units, note FROM settle_ledger WHERE worker_id='w' ORDER BY id DESC LIMIT 1`).
		Scan(&kind, &units, &note); err != nil {
		t.Fatal(err)
	}
	if kind != "payout_applied" || units != 150_00000 {
		t.Fatalf("ledger want delta 150e5 got kind=%s units=%d note=%q", kind, units, note)
	}
	if note == "" || note == "tx-one" {
		t.Fatalf("note should include watermark: %q", note)
	}
	var applied, pending int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM settle_outbox WHERE status='applied'`).Scan(&applied)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM settle_outbox WHERE status='pending'`).Scan(&pending)
	if applied != 1 || pending != 0 {
		t.Fatalf("outbox applied=%d pending=%d want 1/0", applied, pending)
	}
}

func TestSetPendingUnitsRejectsWhenPendingExists(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "pend.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if _, err := s.SetPendingUnits(ctx, "w", "HMC-a", 1_00000, 3); err != nil {
		t.Fatal(err)
	}
	_, err = s.SetPendingUnits(ctx, "w", "HMC-a", 2_00000, 4)
	if err == nil {
		t.Fatal("want error on second pending")
	}
	if err := s.ClearPending(ctx, "w"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetPendingUnits(ctx, "w", "HMC-a", 2_00000, 4); err != nil {
		t.Fatalf("after clear: %v", err)
	}
}
