package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/store"
)

func TestSubmitRejectsSegmentExecDoneMismatch(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "seg.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	cfg := fuzzengine.NormalizeCampaignConfig(map[string]any{
		"pool_distributed": true,
		"input_mode":       "bytes",
		"depth_tier":       "bytes_corpus",
		"exec_per_unit":    8,
		"max_input_bytes":  128,
		"seed_byte_corpus": []any{"41414141"},
		"check_semantics":  "detector",
		"wasm_check_hex":   "00",
	}, "property")
	id := "seg-mismatch"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "property", Status: "running", BudgetRuns: 1, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, id, time.Now().Unix())
	w, ok, err := svc.Claim(ctx, "w1", time.Now().Unix())
	if err != nil || !ok {
		t.Fatal(err)
	}
	if w.ExecPerUnit != 8 {
		t.Fatalf("exec_per_unit=%d want 8", w.ExecPerUnit)
	}
	err = svc.Submit(ctx, SubmitRequest{
		WorkerID: "w1", WorkID: w.WorkID, CampaignID: id, ItemID: w.ItemID,
		InputN: w.InputN, ActualInput: w.ActualInput, InputBytes: w.InputBytes,
		CheckResult: 0, DurationMS: 1, SegmentExecDone: 3,
	})
	if err == nil {
		t.Fatal("expected segment_exec_done mismatch")
	}
}

func TestSubmitAcceptsSegmentExecDone(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "seg-ok.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	cfg := fuzzengine.NormalizeCampaignConfig(map[string]any{
		"pool_distributed": true,
		"input_mode":       "bytes",
		"depth_tier":       "bytes_corpus",
		"exec_per_unit":    4,
		"max_input_bytes":  64,
		"seed_byte_corpus": []any{"00"},
		"check_semantics":  "detector",
		"wasm_check_hex":   "00",
	}, "property")
	id := "seg-ok"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "property", Status: "running", BudgetRuns: 1, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, id, time.Now().Unix())
	w, ok, err := svc.Claim(ctx, "w1", time.Now().Unix())
	if err != nil || !ok {
		t.Fatal(err)
	}
	err = svc.Submit(ctx, SubmitRequest{
		WorkerID: "w1", WorkID: w.WorkID, CampaignID: id, ItemID: w.ItemID,
		InputN: w.InputN, ActualInput: w.ActualInput, InputBytes: w.InputBytes,
		CheckResult: 0, DurationMS: 2, SegmentExecDone: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
}
