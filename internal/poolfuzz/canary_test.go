package poolfuzz

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/store"
)

func TestCanaryMissRejectsFakeClean(t *testing.T) {
	t.Setenv("HACKME_POOL_CANARY", "1")
	t.Setenv("HACKME_POOL_CANARY_PCT", "100")
	t.Setenv("HACKME_POOL_CLEAN_FLOOR", "1")
	t.Setenv("HACKME_POOL_REPLAY_SAMPLE_PCT", "0")
	t.Setenv("HACKME_POOL_REPLAY_SAMPLE_SEED", "canary-miss")

	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "canary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	cfg := fuzzengine.NormalizeCampaignConfig(map[string]any{
		"pool_distributed":  true,
		"dig_package":       "audit",
		"guard_pack":        "script_bounds",
		"exec_per_unit":     4,
		"check_semantics":   "detector",
		"guided_scheduling": true,
		"wasm_check_hex":    mustReadWasmHex(t, "../../tasks/artifacts/security/rust_script_push_bounds_guard.wasm"),
		"seed_corpus":       []any{uint64(42)},
	}, "property")
	id := "canary-miss"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "property", Status: "running", BudgetRuns: 1, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, id, time.Now().Unix())
	w, ok, err := svc.Claim(ctx, "w-fake", time.Now().Unix())
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if !w.IsCanary {
		t.Fatal("expected canary claim at 100%")
	}
	_, err = svc.SubmitWithOutcome(ctx, SubmitRequest{
		WorkerID: "w-fake", WorkID: w.WorkID, CampaignID: id, ItemID: w.ItemID,
		InputN: w.InputN, ActualInput: w.ActualInput, InputBytes: w.InputBytes,
		CheckResult: 0, DurationMS: 5, SegmentExecDone: w.ExecPerUnit,
	})
	if err == nil || !strings.Contains(err.Error(), "canary_miss") {
		t.Fatalf("want canary_miss, got %v", err)
	}
}

func TestCanaryHitFindingPathOK(t *testing.T) {
	t.Setenv("HACKME_POOL_CANARY", "1")
	t.Setenv("HACKME_POOL_CANARY_PCT", "100")
	t.Setenv("HACKME_POOL_CLEAN_FLOOR", "1")
	t.Setenv("HACKME_POOL_REPLAY_SAMPLE_PCT", "0")
	t.Setenv("HACKME_POOL_REPLAY_CRASH_ALWAYS", "1")

	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "canary-hit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	cfg := fuzzengine.NormalizeCampaignConfig(map[string]any{
		"pool_distributed":  true,
		"dig_package":       "audit",
		"guard_pack":        "script_bounds",
		"exec_per_unit":     4,
		"check_semantics":   "detector",
		"guided_scheduling": true,
		"wasm_check_hex":    mustReadWasmHex(t, "../../tasks/artifacts/security/rust_script_push_bounds_guard.wasm"),
		"seed_corpus":       []any{uint64(42)},
		"mutation_rounds":   0,
	}, "property")
	id := "canary-hit"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "property", Status: "running", BudgetRuns: 1, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, id, time.Now().Unix())
	w, ok, err := svc.Claim(ctx, "w-honest", time.Now().Unix())
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if !w.IsCanary {
		t.Fatal("expected canary claim")
	}
	out, err := svc.SubmitWithOutcome(ctx, SubmitRequest{
		WorkerID: "w-honest", MinerAddress: "HMC-cccccccccccccccc",
		WorkID: w.WorkID, CampaignID: id, ItemID: w.ItemID,
		InputN: w.InputN, ActualInput: w.ActualInput, InputBytes: w.InputBytes,
		CheckResult: 1, DurationMS: 8, SegmentExecDone: w.ExecPerUnit,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.ReplayStatus != replayReasonFindingClaim {
		t.Fatalf("honest canary hit should full-replay, status=%q", out.ReplayStatus)
	}
}
