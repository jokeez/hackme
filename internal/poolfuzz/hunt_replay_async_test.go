package poolfuzz

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestHuntReplayCancelClearsQueue(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "hunt-cancel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	now := time.Now().Unix()
	id := "hunt-cancel-camp"
	cfg := map[string]any{
		"pool_distributed": true, "work_kind": "hunt_shard", "campaign_type": "hunt",
		"upstream_target_id": "jsmn", "harness_hash": "x", "iterations_per_shard": 2,
	}
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "hunt", Status: "running", BudgetRuns: 2, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, id, now)
	_, _ = db.ExecContext(ctx,
		`UPDATE fuzz_work_items SET status='replay_pending', updated_at=? WHERE campaign_id=?`, now, id)
	_, _ = db.ExecContext(ctx,
		`INSERT INTO fuzz_hunt_replay_queue
		 (campaign_id, item_id, worker_id, miner_address, input_n, worker_check_result, worker_trap, segment_exec_done, duration_ms, status, created_at, updated_at)
		 SELECT ?, id, 'w', '', input_n, 0, '', 0, 0, 'pending', ?, ? FROM fuzz_work_items WHERE campaign_id=? LIMIT 1`,
		id, now, now, id)
	if err := svc.SetCampaignStatus(ctx, id, "cancelled"); err != nil {
		t.Fatal(err)
	}
	var rp, qPending int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_work_items WHERE campaign_id=? AND status='replay_pending'`, id).Scan(&rp)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_hunt_replay_queue WHERE campaign_id=? AND status IN ('pending','processing')`, id).Scan(&qPending)
	if rp != 0 || qPending != 0 {
		t.Fatalf("cancel left replay_pending=%d queue_active=%d", rp, qPending)
	}
}

func TestReclaimStaleHuntReplayJobs(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "hunt-stale.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	now := time.Now().Unix()
	_, err = db.ExecContext(ctx,
		`INSERT INTO fuzz_hunt_replay_queue
		 (campaign_id, item_id, worker_id, miner_address, input_n, worker_check_result, worker_trap, segment_exec_done, duration_ms, status, created_at, updated_at)
		 VALUES ('c', 1, 'w', '', 1, 0, '', 0, 0, 'processing', ?, ?)`,
		now-3600, now-huntReplayStaleProcessingSec-10)
	if err != nil {
		t.Fatal(err)
	}
	svc.reclaimStaleHuntReplayJobs(ctx, now)
	var st string
	if err := db.QueryRowContext(ctx, `SELECT status FROM fuzz_hunt_replay_queue WHERE campaign_id='c' AND item_id=1`).Scan(&st); err != nil {
		t.Fatal(err)
	}
	if st != huntReplayStatusPending {
		t.Fatalf("status=%q want pending", st)
	}
}

func TestHuntReplayRetryableClassifies(t *testing.T) {
	if !huntReplayRetryable(fmt.Errorf("database is locked")) {
		t.Fatal("busy should retry")
	}
	if !huntReplayRetryable(fmt.Errorf("fuzzupstream: exec timeout: x")) {
		t.Fatal("timeout should retry")
	}
	if !huntReplayRetryable(fmt.Errorf("poolfuzz: settle flush: boom")) {
		t.Fatal("settle should retry")
	}
	if huntReplayRetryable(fmt.Errorf("poolfuzz: campaign cancelled")) {
		t.Fatal("cancel must not retry forever")
	}
	if huntReplayRetryable(fmt.Errorf("executable file not found")) {
		t.Fatal("missing binary must be definitive")
	}
	if huntReplayRetryCount("retry:3:database is locked") != 3 {
		t.Fatal("retry count parse")
	}
}

func TestListPendingSettleOutboxFinalizeLast(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "settle-order.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	fin, err := svc.EnqueueSettleOutbox(ctx, "finalize", "camp-ord", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.EnqueueSettleOutbox(ctx, "run", "camp-ord", "HMC-a", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if fin >= run {
		t.Fatalf("expected finalize id %d < run id %d for race setup", fin, run)
	}
	items, err := svc.ListPendingSettleOutbox(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 2 {
		t.Fatalf("items=%d", len(items))
	}
	if items[0].Kind != "run" || items[1].Kind != "finalize" {
		t.Fatalf("want run then finalize, got %+v %+v", items[0], items[1])
	}
}

func TestFinalizeHuntSubmitDrainsSettleWhenAlreadyDone(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "settle-done.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	spy := &failThenOKSettler{failUntil: 0}
	svc := &Service{DB: db, Settler: spy}
	ctx := context.Background()
	cfg := map[string]any{
		"pool_distributed": true,
		"work_kind":        "hunt_shard",
		"campaign_type":    "hunt",
		"budget_hmc":       1.0,
		"check_semantics":  "native_crash",
	}
	id := "hunt-settle-done"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "hunt", Status: "running", BudgetRuns: 1, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	if err := svc.EnsureWorkItems(ctx, id, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE fuzz_work_items SET status='done', attempts=1, result_ok=1, miner_address=?, settle_run_status='pending', lease_owner='', lease_until=0, updated_at=?
		 WHERE campaign_id=? AND id=1`,
		"HMC-1234567890123456", now, id); err != nil {
		t.Fatal(err)
	}
	err = svc.finalizeHuntSubmit(ctx, finalizeHuntSubmitParams{
		req: SubmitRequest{
			WorkerID: "w1", MinerAddress: "HMC-1234567890123456",
			CampaignID: id, ItemID: 1, DurationMS: 1,
		},
		cfg:               cfg,
		pass:              true,
		fromReplayPending: true,
		now:               now,
	})
	if err != nil {
		t.Fatalf("settle-only finalize: %v", err)
	}
	var runSt string
	if err := db.QueryRowContext(ctx,
		`SELECT settle_run_status FROM fuzz_work_items WHERE campaign_id=? AND id=1`, id).Scan(&runSt); err != nil {
		t.Fatal(err)
	}
	if runSt != "paid" && runSt != "queued" {
		t.Fatalf("settle_run_status=%q want paid|queued", runSt)
	}
	if spy.runs < 1 {
		t.Fatal("expected PayRun on settle-only path")
	}
}

func TestHuntReplayRequiredWhenEscrowEnabled(t *testing.T) {
	t.Setenv("HACKME_POOL_HUNT_REPLAY", "0")
	svc := &Service{}
	_, _, _, _, _, _, err := svc.evalHuntSubmitCheck(context.Background(), "c", 1, map[string]any{
		"budget_hmc":           1.0,
		"iterations_per_shard": 1,
	}, SubmitRequest{SegmentExecDone: 1}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "replay required") {
		t.Fatalf("want fail-closed escrow without replay, got %v", err)
	}
}
