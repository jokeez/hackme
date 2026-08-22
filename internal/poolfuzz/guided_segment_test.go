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

func TestGuidedSegmentUsesClaimSnapshotAfterCorpusDrift(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "guided-seg.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	cfg := fuzzengine.NormalizeCampaignConfig(map[string]any{
		"pool_distributed":  true,
		"input_mode":        "bytes",
		"depth_tier":        "bytes_corpus",
		"guided_scheduling": true,
		"exec_per_unit":     8,
		"max_input_bytes":   256,
		"seed_byte_corpus":  []any{"41414141", "42424242"},
		"check_semantics":   "detector",
		"wasm_check_hex":    sandbox.MinimalGateWasmHex,
	}, "property")
	id := "guided-seg-snap"
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
	if len(w.CorpusSeeds) == 0 {
		t.Fatal("expected corpus snapshot on guided claim")
	}
	// Simulate live corpus drift after claim (must not affect submit verify).
	now := time.Now().Unix()
	_ = svc.upsertPoolCorpusSeed(ctx, id, 0xDEADBEEF, []byte("corpus-drift"), 99, 1, 2, false, now)

	err = svc.Submit(ctx, SubmitRequest{
		WorkerID: "w1", WorkID: w.WorkID, CampaignID: w.CampaignID,
		ItemID: w.ItemID, InputN: w.InputN, ActualInput: w.ActualInput, InputBytes: w.InputBytes,
		CheckResult: 0, DurationMS: 3, SegmentExecDone: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
}
