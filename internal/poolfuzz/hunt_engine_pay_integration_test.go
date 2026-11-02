package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/hunt"
	"hackme/internal/store"
)

// Dig hygiene prove / clean_floor must remain Dig-only.
func TestHuntEnginePayDoesNotUseDigCleanFloor(t *testing.T) {
	t.Setenv("HACKME_HUNT_COVERAGE_ENGINE", "1")
	t.Setenv("HACKME_POOL_CLEAN_FLOOR", "1")
	t.Setenv("HACKME_POOL_CLEAN_EDGES_REQUIRE", "1")
	kind, full := DecideHuntEnginePay(0, 0, 0, 0, false)
	if full || kind != HuntEnginePayFloor {
		t.Fatalf("want floor")
	}
	err := checkCleanHygieneFloor(SubmitRequest{DurationMS: 0, EdgesTouchedOK: false}, 64, map[string]any{
		"coverage_kind": "wasm_edge_bitmap", "dig_package": "smoke",
	})
	if err == nil {
		t.Fatal("dig floor should still reject empty CLEAN")
	}
}

func TestFinalizeHuntEngineFloorSkipsPayRun(t *testing.T) {
	t.Setenv("HACKME_HUNT_COVERAGE_ENGINE", "1")
	t.Setenv("HACKME_HUNT_ENGINE_MIN_WALL_MS", "0")
	t.Setenv("HACKME_HUNT_ENGINE_FLOOR_PAY_RUN", "0")
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "e.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	spy := &spySettler{}
	svc := &Service{DB: db, Settler: spy}
	id := "hunt-engine-floor"
	cfg := map[string]any{
		"pool_distributed":     true,
		"work_kind":            "hunt_shard",
		"campaign_type":        "hunt",
		"escrow_split":         "50_50",
		"budget_hmc":           20,
		"budget_shards":        8,
		"upstream_target_id":   "jsmn",
		"harness_hash":         "deadbeef01",
		"iterations_per_shard": 2,
		"hunt_engine":          "libfuzzer",
		"check_semantics":      "native_crash",
	}
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "hunt", Status: "running", BudgetRuns: 8, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	if err := svc.EnsureWorkItems(ctx, id, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE fuzz_work_items SET status='leased', lease_owner=?, lease_until=?, updated_at=? WHERE campaign_id=? AND id=1`,
		"w1", now+600, now, id); err != nil {
		t.Fatal(err)
	}
	err = svc.finalizeHuntSubmit(ctx, finalizeHuntSubmitParams{
		req: SubmitRequest{
			WorkerID: "w1", MinerAddress: "HMC-1234567890123456", CampaignID: id, ItemID: 1,
			ActualInput: 1, DurationMS: 1000, SegmentExecDone: 1, HuntEngine: hunt.EngineLibFuzzer,
		},
		cfg: cfg, inputN: 1, expectedU: 1, pass: true, recordFinding: false, now: now,
		engineMode: true, enginePayKind: HuntEnginePayFloor, engineFullPay: false,
	})
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if spy.runs > 0 {
		t.Fatalf("floor must not PayRun, got %d", spy.runs)
	}
}

func TestFinalizeHuntEngineFloorPaysByDefault(t *testing.T) {
	t.Setenv("HACKME_HUNT_COVERAGE_ENGINE", "1")
	t.Setenv("HACKME_HUNT_ENGINE_MIN_WALL_MS", "0")
	t.Setenv("HACKME_HUNT_ENGINE_FLOOR_PAY_RUN", "")
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "floor-default.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	spy := &spySettler{}
	svc := &Service{DB: db, Settler: spy}
	id := "hunt-engine-floor-default"
	cfg := map[string]any{
		"pool_distributed": true, "work_kind": "hunt_shard", "campaign_type": "hunt",
		"escrow_split": "50_50", "budget_hmc": 20, "budget_shards": 8,
		"upstream_target_id": "jsmn", "harness_hash": "deadbeef03",
		"iterations_per_shard": 2, "hunt_engine": "libfuzzer", "check_semantics": "native_crash",
	}
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "hunt", Status: "running", BudgetRuns: 8, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	if err := svc.EnsureWorkItems(ctx, id, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE fuzz_work_items SET status='leased', lease_owner=?, lease_until=?, updated_at=? WHERE campaign_id=? AND id=1`,
		"w1", now+600, now, id); err != nil {
		t.Fatal(err)
	}
	err = svc.finalizeHuntSubmit(ctx, finalizeHuntSubmitParams{
		req: SubmitRequest{
			WorkerID: "w1", MinerAddress: "HMC-1234567890123456", CampaignID: id, ItemID: 1,
			ActualInput: 1, DurationMS: 1000, SegmentExecDone: 1, HuntEngine: hunt.EngineLibFuzzer,
		},
		cfg: cfg, inputN: 1, expectedU: 1, pass: true, recordFinding: false, now: now,
		engineMode: true, enginePayKind: HuntEnginePayFloor, engineFullPay: false,
	})
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if spy.runs < 1 {
		t.Fatal("floor must PayRun by default")
	}
}

func TestFinalizeHuntEngineEdgesPays(t *testing.T) {
	t.Setenv("HACKME_HUNT_COVERAGE_ENGINE", "1")
	t.Setenv("HACKME_HUNT_ENGINE_MIN_WALL_MS", "0")
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "e2.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	spy := &spySettler{}
	svc := &Service{DB: db, Settler: spy}
	id := "hunt-engine-edges"
	cfg := map[string]any{
		"pool_distributed": true, "work_kind": "hunt_shard", "campaign_type": "hunt",
		"escrow_split": "50_50", "budget_hmc": 20, "budget_shards": 8,
		"upstream_target_id": "jsmn", "harness_hash": "deadbeef02",
		"iterations_per_shard": 2, "hunt_engine": "libfuzzer", "check_semantics": "native_crash",
	}
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "hunt", Status: "running", BudgetRuns: 8, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	if err := svc.EnsureWorkItems(ctx, id, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE fuzz_work_items SET status='leased', lease_owner=?, lease_until=?, updated_at=? WHERE campaign_id=? AND id=1`,
		"w1", now+600, now, id); err != nil {
		t.Fatal(err)
	}
	err = svc.finalizeHuntSubmit(ctx, finalizeHuntSubmitParams{
		req: SubmitRequest{
			WorkerID: "w1", MinerAddress: "HMC-1234567890123456", CampaignID: id, ItemID: 1,
			ActualInput: 1, DurationMS: 1000, SegmentExecDone: 1, HuntEngine: hunt.EngineLibFuzzer,
		},
		cfg: cfg, inputN: 1, expectedU: 1, pass: true, recordFinding: false, now: now,
		engineMode: true, enginePayKind: HuntEnginePayEdges, engineFullPay: true,
	})
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if spy.runs < 1 {
		t.Fatal("proven edges must PayRun")
	}
}

// Legacy (non-coverage-engine) submits with hunt_edge_pay=floor_only must enqueue
// run_floor, not full PayRun — otherwise CLEAN drains the runs pool 10×.
func TestFinalizeLegacyFloorOnlyEnqueuesFloor(t *testing.T) {
	t.Setenv("HACKME_HUNT_ENGINE_FLOOR_PAY_RUN", "1")
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "legacy-floor.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	spy := &spySettler{}
	svc := &Service{DB: db, Settler: spy}
	id := "hunt-legacy-floor-only"
	cfg := map[string]any{
		"pool_distributed": true, "work_kind": "hunt_shard", "campaign_type": "hunt",
		"escrow_split": "50_50", "budget_hmc": 20, "budget_shards": 8,
		"upstream_target_id": "jsmn", "harness_hash": "deadbeef04",
		"iterations_per_shard": 2, "check_semantics": "native_crash",
		"hunt_edge_pay": "floor_only",
	}
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "hunt", Status: "running", BudgetRuns: 8, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	if err := svc.EnsureWorkItems(ctx, id, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE fuzz_work_items SET status='leased', lease_owner=?, lease_until=?, updated_at=? WHERE campaign_id=? AND id=1`,
		"w1", now+600, now, id); err != nil {
		t.Fatal(err)
	}
	// Mimic service.go non-engine branch: stamp floor gate even when engineMode was false for eval.
	kind, full := DecideHuntEnginePayForConfig(cfg, 0, 0, 0, 0, false)
	if kind != HuntEnginePayFloor || full {
		t.Fatalf("Decide: kind=%s full=%v", kind, full)
	}
	err = svc.finalizeHuntSubmit(ctx, finalizeHuntSubmitParams{
		req: SubmitRequest{
			WorkerID: "w1", MinerAddress: "HMC-1234567890123456", CampaignID: id, ItemID: 1,
			ActualInput: 1, DurationMS: 1000, SegmentExecDone: 1,
		},
		cfg: cfg, inputN: 1, expectedU: 1, pass: true, recordFinding: false, now: now,
		engineMode: true, enginePayKind: kind, engineFullPay: full,
	})
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if spy.floors < 1 {
		t.Fatalf("floor_only legacy must PayRunFloor, floors=%d fullRuns=%d", spy.floors, spy.fullRuns)
	}
	if spy.fullRuns != 0 {
		t.Fatalf("floor_only must not full PayRun, fullRuns=%d", spy.fullRuns)
	}
}
