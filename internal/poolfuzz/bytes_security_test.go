package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/sandbox"
	"hackme/internal/store"
)

func TestClaimClampsByteInputToMax(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "clamp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	seeds := TracefuseByteSeeds()
	cfg := PilotBytesCorpusConfig("00", 64, seeds, true)
	id := "clamp-bytes"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "property", Status: "running", BudgetRuns: 4, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, id, time.Now().Unix())
	w, ok, err := svc.Claim(ctx, "w1", time.Now().Unix())
	if err != nil || !ok {
		t.Fatal(err)
	}
	if len(w.InputBytes) == 0 {
		t.Fatal("expected byte input")
	}
	if len(w.InputBytes) > 64 {
		t.Fatalf("claim must clamp to max_input_bytes=64, got %d", len(w.InputBytes))
	}
}

func TestSubmitRejectsInputBytesMismatch(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "mismatch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	cfg := fuzzengine.NormalizeCampaignConfig(map[string]any{
		"pool_distributed": true,
		"input_mode":       "bytes",
		"max_input_bytes":  128,
		"seed_byte_corpus": []any{"41414141"},
		"check_semantics":  "detector",
		"wasm_check_hex":   sandbox.MinimalGateWasmHex,
	}, "property")
	id := "bytes-mismatch"
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
		InputN: w.InputN, ActualInput: w.ActualInput, InputBytes: []byte("BBBB"),
		CheckResult: 0, DurationMS: 1,
	})
	if err == nil {
		t.Fatal("expected input_bytes mismatch")
	}
}
