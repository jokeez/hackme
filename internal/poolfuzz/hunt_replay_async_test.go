package poolfuzz

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/hunt"
	"hackme/internal/store"
)

func TestHuntReplayAsyncEnqueueAndDrain(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang required")
	}
	t.Setenv("HACKME_POOL_HUNT_REPLAY_ASYNC", "1")
	t.Setenv("HACKME_POOL_HUNT_REPLAY", "1")
	t.Setenv("HACKME_POOL_HUNT_REPLAY_WORKERS", "1")
	t.Setenv("HACKME_REPO_ROOT", hunt.RepoRoot())

	hash, err := hunt.CatalogHarnessHash(hunt.RepoRoot(), "jsmn")
	if err != nil {
		t.Skip(err)
	}

	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "hunt-async.db"))
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
		"harness_hash":         hash,
		"iterations_per_shard": 2,
		"max_input_bytes":      256,
		"depth_tier":           "oss_cve",
		"check_semantics":      "native_crash",
	}
	id := "hunt-async-camp"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "hunt", Status: "running", BudgetRuns: 1, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, id, time.Now().Unix())
	w, ok, err := svc.Claim(ctx, "hunt-async-w1", time.Now().Unix())
	if err != nil || !ok {
		t.Fatal(err)
	}

	out, err := svc.SubmitWithOutcome(ctx, SubmitRequest{
		WorkerID: "hunt-async-w1", WorkID: w.WorkID, CampaignID: w.CampaignID,
		ItemID: w.ItemID, InputN: w.InputN, ActualInput: w.ActualInput, InputBytes: w.InputBytes,
		CheckResult: 0, DurationMS: 3, SegmentExecDone: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Async || out.ReplayStatus != huntReplayStatusPending {
		t.Fatalf("expected async pending, got %+v", out)
	}

	var st string
	if err := db.QueryRowContext(ctx, `SELECT status FROM fuzz_work_items WHERE id=?`, w.ItemID).Scan(&st); err != nil {
		t.Fatal(err)
	}
	if st != workStatusReplayPending {
		t.Fatalf("work status=%q want replay_pending", st)
	}

	if err := svc.DrainHuntReplayQueue(ctx); err != nil {
		t.Fatal(err)
	}
	var resultOK int
	if err := db.QueryRowContext(ctx, `SELECT status, result_ok FROM fuzz_work_items WHERE id=?`, w.ItemID).Scan(&st, &resultOK); err != nil {
		t.Fatal(err)
	}
	if st != "done" || resultOK != 1 {
		t.Fatalf("after drain status=%q result_ok=%d", st, resultOK)
	}
	var qStatus string
	if err := db.QueryRowContext(ctx, `SELECT status FROM fuzz_hunt_replay_queue WHERE campaign_id=? AND item_id=?`, id, w.ItemID).Scan(&qStatus); err != nil {
		t.Fatal(err)
	}
	if qStatus != huntReplayStatusDone {
		t.Fatalf("queue status=%q", qStatus)
	}
}

func TestHuntReplayAsyncDisabledSyncPath(t *testing.T) {
	t.Setenv("HACKME_POOL_HUNT_REPLAY_ASYNC", "0")
	t.Setenv("HACKME_POOL_HUNT_REPLAY", "0")

	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "hunt-sync.db"))
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
		"harness_hash":         "abc",
		"iterations_per_shard": 2,
		"max_input_bytes":      256,
	}
	id := "hunt-sync-camp"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "hunt", Status: "running", BudgetRuns: 1, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, id, time.Now().Unix())
	w, ok, err := svc.Claim(ctx, "hunt-sync-w1", time.Now().Unix())
	if err != nil || !ok {
		t.Fatal(err)
	}

	out, err := svc.SubmitWithOutcome(ctx, SubmitRequest{
		WorkerID: "hunt-sync-w1", WorkID: w.WorkID, CampaignID: w.CampaignID,
		ItemID: w.ItemID, InputN: w.InputN, ActualInput: w.ActualInput, InputBytes: w.InputBytes,
		CheckResult: 0, DurationMS: 1, SegmentExecDone: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Async {
		t.Fatalf("sync path should not async: %+v", out)
	}
}
