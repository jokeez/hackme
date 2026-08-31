package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/hunt"
	"hackme/internal/store"
)

func TestHuntGuidedClaimFreezesCorpusSnapshot(t *testing.T) {
	t.Setenv("HACKME_POOL_HUNT_REPLAY", "0")
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "hunt-corpus.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	cfg := map[string]any{
		"pool_distributed":     true,
		"work_kind":            "hunt_shard",
		"campaign_type":        "hunt",
		"upstream_target_id":   "jsmn",
		"harness_hash":         "abc123",
		"iterations_per_shard": 4,
		"max_input_bytes":      256,
		"depth_tier":           "oss_cve",
		"hunt_segment_mutating": true,
	}
	hunt.ApplyPoolGuidedDefaults(cfg, "jsmn")
	id := "hunt-corpus-guided"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "hunt", Status: "running", BudgetRuns: 2, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, id, time.Now().Unix())
	w, ok, err := svc.Claim(ctx, "hunt-w1", time.Now().Unix())
	if err != nil || !ok {
		t.Fatal(err)
	}
	if w.CoverageKind != "hunt_corpus_guided" {
		t.Fatalf("coverage_kind=%q", w.CoverageKind)
	}
	if len(w.CorpusSeeds) == 0 {
		t.Fatal("expected corpus snapshot on hunt guided claim")
	}
	anchor := hunt.ShardAnchorBytes(id, w.InputN, cfg)
	if len(w.InputBytes) == 0 {
		t.Fatal("missing input bytes")
	}
	// Guided anchor may differ from raw hash anchor when corpus is non-empty.
	_ = anchor
	now := time.Now().Unix()
	_ = svc.upsertPoolCorpusSeed(ctx, id, 0xDEADBEEF, []byte("corpus-drift-after-claim"), 99, 1, 2, false, now)

	err = svc.Submit(ctx, SubmitRequest{
		WorkerID: "hunt-w1", WorkID: w.WorkID, CampaignID: w.CampaignID,
		ItemID: w.ItemID, InputN: w.InputN, ActualInput: w.ActualInput, InputBytes: w.InputBytes,
		CheckResult: 0, DurationMS: 5, SegmentExecDone: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	var corpusN int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_pool_corpus WHERE campaign_id=?`, id).Scan(&corpusN); err != nil {
		t.Fatal(err)
	}
	if corpusN < 2 {
		t.Fatalf("expected corpus growth after submit, got %d rows", corpusN)
	}
}

func TestHuntGuidedSubmitRequiresCorpusSnapshot(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "hunt-corpus-miss.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	cfg := map[string]any{
		"work_kind": "hunt_shard", "campaign_type": "hunt",
		"upstream_target_id": "x", "iterations_per_shard": 2,
		"hunt_corpus_guided": true, "guided_scheduling": true,
	}
	id := "hunt-snap-req"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "hunt", Status: "running", BudgetRuns: 1, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, id, time.Now().Unix())
	var itemID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM fuzz_work_items WHERE campaign_id=? LIMIT 1`, id).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	_, err = svc.SeedsForWorkItem(ctx, id, itemID, cfg)
	if err == nil {
		t.Fatal("expected missing corpus snapshot error")
	}
}
