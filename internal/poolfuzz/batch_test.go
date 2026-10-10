package poolfuzz

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/sandbox"
	"hackme/internal/store"
)

func TestClaimBatchLeasesMultiple(t *testing.T) {
	t.Setenv("HACKME_POOL_REPLAY_SAMPLE_PCT", "0")
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "batch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	cfg := fuzzengine.NormalizeCampaignConfig(map[string]any{
		"pool_distributed": true,
		"dig_package":      "audit",
		"exec_per_unit":    4,
		"check_semantics":  "detector",
		"wasm_check_hex":   sandbox.MinimalGateWasmHex,
		"seed_corpus":      []any{uint64(1)},
	}, "property")
	id := "batch-claim"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "property", Status: "running", BudgetRuns: 8, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, id, time.Now().Unix())
	items, err := svc.ClaimBatch(ctx, "batch-w1", time.Now().Unix(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("claimed %d want 3", len(items))
	}
	seen := map[int64]bool{}
	for _, w := range items {
		if seen[w.ItemID] {
			t.Fatalf("duplicate item %d", w.ItemID)
		}
		seen[w.ItemID] = true
		if w.CampaignID != id {
			t.Fatalf("campaign=%s", w.CampaignID)
		}
	}
}

func TestSubmitBatchCannotStealForeignLease(t *testing.T) {
	t.Setenv("HACKME_POOL_REPLAY_SAMPLE_PCT", "0")
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "batch-steal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	cfg := fuzzengine.NormalizeCampaignConfig(map[string]any{
		"pool_distributed": true,
		"dig_package":      "audit",
		"exec_per_unit":    4,
		"check_semantics":  "detector",
		"wasm_check_hex":   sandbox.MinimalGateWasmHex,
		"seed_corpus":      []any{uint64(1)},
	}, "property")
	id := "batch-steal"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "property", Status: "running", BudgetRuns: 4, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, id, time.Now().Unix())
	ownerItems, err := svc.ClaimBatch(ctx, "owner", time.Now().Unix(), 2)
	if err != nil || len(ownerItems) != 2 {
		t.Fatalf("owner claim: n=%d err=%v", len(ownerItems), err)
	}
	thiefItems, err := svc.ClaimBatch(ctx, "thief", time.Now().Unix(), 1)
	if err != nil || len(thiefItems) != 1 {
		t.Fatalf("thief claim: n=%d err=%v", len(thiefItems), err)
	}

	// Thief tries to submit owner's first lease + own lease in one batch.
	results := svc.SubmitBatch(ctx, []SubmitRequest{
		{
			WorkerID: "thief", WorkID: ownerItems[0].WorkID, CampaignID: id, ItemID: ownerItems[0].ItemID,
			InputN: ownerItems[0].InputN, ActualInput: ownerItems[0].ActualInput, InputBytes: ownerItems[0].InputBytes,
			CheckResult: 0, DurationMS: 1, SegmentExecDone: ownerItems[0].ExecPerUnit,
		},
		{
			WorkerID: "thief", WorkID: thiefItems[0].WorkID, CampaignID: id, ItemID: thiefItems[0].ItemID,
			InputN: thiefItems[0].InputN, ActualInput: thiefItems[0].ActualInput, InputBytes: thiefItems[0].InputBytes,
			CheckResult: 0, DurationMS: 1, SegmentExecDone: thiefItems[0].ExecPerUnit,
		},
	})
	if len(results) != 2 {
		t.Fatalf("results=%d", len(results))
	}
	if results[0].OK || !strings.Contains(results[0].Error, "leased by another") {
		t.Fatalf("steal row must fail, got ok=%v err=%q", results[0].OK, results[0].Error)
	}
	if !results[1].OK {
		t.Fatalf("own lease must succeed, err=%q", results[1].Error)
	}
	var ownerSt string
	_ = db.QueryRowContext(ctx, `SELECT status FROM fuzz_work_items WHERE campaign_id=? AND id=?`,
		id, ownerItems[0].ItemID).Scan(&ownerSt)
	if ownerSt != "leased" {
		t.Fatalf("owner lease must survive steal attempt, status=%q", ownerSt)
	}
}

func TestMaxBatchClaimSubmitIs16(t *testing.T) {
	if MaxBatchClaimSubmit != 16 {
		t.Fatalf("cap=%d want 16", MaxBatchClaimSubmit)
	}
}
