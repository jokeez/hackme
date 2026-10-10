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

// Report #35 — sampled + crash-first Dig replay:
// clean hygiene skip must not mint bounty; crash/found claims still full-replay;
// sample path is deterministic under fixed seed.

func TestZZReport35CleanHygieneSkipCannotMintBounty(t *testing.T) {
	t.Setenv("HACKME_POOL_FULL_REPLAY", "")
	t.Setenv("HACKME_POOL_REPLAY_SAMPLE_PCT", "0")
	t.Setenv("HACKME_POOL_REPLAY_SAMPLE_PCT_DEEP", "0")
	t.Setenv("HACKME_POOL_REPLAY_CRASH_ALWAYS", "1")
	t.Setenv("HACKME_POOL_REPLAY_SAMPLE_SEED", "zz35")

	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "zz35.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	spy := &spySettler{}
	svc := &Service{DB: db, Settler: spy}
	ctx := context.Background()

	safeInput := uint64(42)
	cfg := fuzzengine.NormalizeCampaignConfig(map[string]any{
		"pool_distributed": true,
		"dig_package":      "audit",
		"budget_hmc":       2.0,
		"exec_per_unit":    8,
		"check_semantics":  "detector",
		"wasm_check_hex":   mustReadWasmHex(t, "../../tasks/artifacts/security/rust_script_push_bounds_guard.wasm"),
		"seed_corpus":      []any{safeInput},
		"mutation_rounds":  0,
	}, "property")
	id := "zz35-hygiene"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "property", Status: "running", BudgetRuns: 1, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, id, time.Now().Unix())
	w, ok, err := svc.Claim(ctx, "w-hygiene", time.Now().Unix())
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}

	// Attacker tries to mint a finding on hygiene path by lying about check_result
	// without a trap — that forces finding_claim replay, which must not pay if WASM clean.
	out, err := svc.SubmitWithOutcome(ctx, SubmitRequest{
		WorkerID: "w-hygiene", MinerAddress: "HMC-aaaaaaaaaaaaaaaa",
		WorkID: w.WorkID, CampaignID: id, ItemID: w.ItemID,
		InputN: w.InputN, ActualInput: w.ActualInput, InputBytes: w.InputBytes,
		CheckResult: 1, DurationMS: 1, SegmentExecDone: w.ExecPerUnit,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.ReplayStatus != replayReasonFindingClaim {
		t.Fatalf("forged found must force replay, status=%q", out.ReplayStatus)
	}
	var findings int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_findings WHERE campaign_id=?`, id).Scan(&findings)
	if findings != 0 {
		t.Fatalf("forged detector hit on clean WASM must not mint finding, got %d", findings)
	}
	spy.mu.Lock()
	paid, bonus := spy.findings, spy.crashBonus
	spy.mu.Unlock()
	if paid != 0 || bonus != 0 {
		t.Fatalf("forged found must not pay bounty/bonus paid=%d bonus=%d", paid, bonus)
	}
}

func TestZZReport35TrapForcesReplayNotHygiene(t *testing.T) {
	t.Setenv("HACKME_POOL_FULL_REPLAY", "")
	t.Setenv("HACKME_POOL_REPLAY_SAMPLE_PCT", "0")
	t.Setenv("HACKME_POOL_REPLAY_SAMPLE_SEED", "zz35-clean")

	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "zz35c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()

	safeInput := uint64(42)
	cfg := fuzzengine.NormalizeCampaignConfig(map[string]any{
		"pool_distributed": true,
		"dig_package":      "audit",
		"exec_per_unit":    8,
		"check_semantics":  "detector",
		"wasm_check_hex":   mustReadWasmHex(t, "../../tasks/artifacts/security/rust_script_push_bounds_guard.wasm"),
		"seed_corpus":      []any{safeInput},
		"mutation_rounds":  0,
	}, "property")
	id := "zz35-clean"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "property", Status: "running", BudgetRuns: 1, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, id, time.Now().Unix())
	w, ok, err := svc.Claim(ctx, "w-clean", time.Now().Unix())
	if err != nil || !ok {
		t.Fatal(err)
	}
	out, err := svc.SubmitWithOutcome(ctx, SubmitRequest{
		WorkerID: "w-clean", WorkID: w.WorkID, CampaignID: id, ItemID: w.ItemID,
		InputN: w.InputN, ActualInput: w.ActualInput, InputBytes: w.InputBytes,
		CheckResult: 0, DurationMS: 2, SegmentExecDone: w.ExecPerUnit,
		Trap: "fabricated trap must force crash_claim replay",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Non-empty trap forces crash_claim replay (cannot bury forged trap via hygiene).
	if out.ReplayStatus != replayReasonCrashClaim {
		t.Fatalf("trap claim status=%q want %q", out.ReplayStatus, replayReasonCrashClaim)
	}
	var findings int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_findings WHERE campaign_id=?`, id).Scan(&findings)
	if findings != 0 {
		t.Fatalf("fabricated trap must not mint finding after replay, got %d", findings)
	}
}

func TestZZReport35HygieneSkipPath(t *testing.T) {
	t.Setenv("HACKME_POOL_FULL_REPLAY", "")
	t.Setenv("HACKME_POOL_REPLAY_SAMPLE_PCT", "0")
	t.Setenv("HACKME_POOL_REPLAY_SAMPLE_SEED", "zz35-hygiene-ok")

	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "zz35h.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()

	cfg := fuzzengine.NormalizeCampaignConfig(map[string]any{
		"pool_distributed": true,
		"dig_package":      "audit",
		"exec_per_unit":    8,
		"check_semantics":  "detector",
		"wasm_check_hex":   sandbox.MinimalGateWasmHex,
		"seed_corpus":      []any{uint64(1)},
	}, "property")
	id := "zz35-skip"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "property", Status: "running", BudgetRuns: 1, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, id, time.Now().Unix())
	w, ok, err := svc.Claim(ctx, "w-skip", time.Now().Unix())
	if err != nil || !ok {
		t.Fatal(err)
	}
	out, err := svc.SubmitWithOutcome(ctx, SubmitRequest{
		WorkerID: "w-skip", WorkID: w.WorkID, CampaignID: id, ItemID: w.ItemID,
		InputN: w.InputN, ActualInput: w.ActualInput, InputBytes: w.InputBytes,
		CheckResult: 0, DurationMS: 2, SegmentExecDone: w.ExecPerUnit,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.ReplayStatus != replayReasonHygieneSkip {
		t.Fatalf("clean pct=0 status=%q want %q", out.ReplayStatus, replayReasonHygieneSkip)
	}
	var st string
	_ = db.QueryRowContext(ctx, `SELECT status FROM fuzz_work_items WHERE campaign_id=? AND id=?`, id, w.ItemID).Scan(&st)
	if st != "done" {
		t.Fatalf("hygiene skip must complete work, status=%q", st)
	}
	var findings int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_findings WHERE campaign_id=?`, id).Scan(&findings)
	if findings != 0 {
		t.Fatalf("hygiene skip must not mint findings, got %d", findings)
	}
}

func TestZZReport35SamplePathDeterministic(t *testing.T) {
	t.Setenv("HACKME_POOL_FULL_REPLAY", "")
	t.Setenv("HACKME_POOL_REPLAY_SAMPLE_SEED", "zz35-sample")

	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "zz35s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()

	mk := func(id string, pctEnv string) SubmitOutcome {
		t.Helper()
		t.Setenv("HACKME_POOL_REPLAY_SAMPLE_PCT", pctEnv)
		cfg := fuzzengine.NormalizeCampaignConfig(map[string]any{
			"pool_distributed":  true,
			"dig_package":       "audit",
			"exec_per_unit":     4,
			"check_semantics":   "detector",
			"wasm_check_hex":    sandbox.MinimalGateWasmHex,
			"guided_scheduling": false,
			"seed_corpus":       []any{uint64(1)},
			"mutation_rounds":   0,
		}, "property")
		if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "property", Status: "running", BudgetRuns: 1, Config: cfg}); err != nil {
			t.Fatal(err)
		}
		_ = svc.EnsureWorkItems(ctx, id, time.Now().Unix())
		w, ok, err := svc.Claim(ctx, "w-"+id, time.Now().Unix())
		if err != nil || !ok {
			t.Fatalf("claim %s: ok=%v err=%v", id, ok, err)
		}
		out, err := svc.SubmitWithOutcome(ctx, SubmitRequest{
			WorkerID: "w-" + id, WorkID: w.WorkID, CampaignID: id, ItemID: w.ItemID,
			InputN: w.InputN, ActualInput: w.ActualInput, InputBytes: w.InputBytes,
			CheckResult: 0, DurationMS: 1, SegmentExecDone: w.ExecPerUnit,
		})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	hit := mk("zz35-sample-100", "100")
	if hit.ReplayStatus != replayReasonSample {
		t.Fatalf("pct=100 status=%q want %q", hit.ReplayStatus, replayReasonSample)
	}
	miss := mk("zz35-sample-0", "0")
	if miss.ReplayStatus != replayReasonHygieneSkip {
		t.Fatalf("pct=0 status=%q want %q", miss.ReplayStatus, replayReasonHygieneSkip)
	}
	// Same campaign/item/seed → stable hit bit.
	a := replaySampleHit("zz35-sample-camp", 7, 5)
	b := replaySampleHit("zz35-sample-camp", 7, 5)
	if a != b {
		t.Fatal("sample bit must be stable under fixed seed")
	}
}

func TestZZReport35CrashClaimStillReplays(t *testing.T) {
	t.Setenv("HACKME_POOL_FULL_REPLAY", "")
	t.Setenv("HACKME_POOL_REPLAY_SAMPLE_PCT", "0")
	t.Setenv("HACKME_POOL_REPLAY_CRASH_ALWAYS", "1")

	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "zz35crash.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()

	// Use a known crash-class input for script_push bounds guard when possible;
	// even if WASM does not trap, status must be crash_claim (forced replay).
	cfg := fuzzengine.NormalizeCampaignConfig(map[string]any{
		"pool_distributed": true,
		"dig_package":      "audit",
		"exec_per_unit":    4,
		"check_semantics":  "detector",
		"wasm_check_hex":   mustReadWasmHex(t, "../../tasks/artifacts/security/rust_script_push_bounds_guard.wasm"),
		"seed_corpus":      []any{uint64(42)},
		"mutation_rounds":  0,
	}, "property")
	id := "zz35-crash"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "property", Status: "running", BudgetRuns: 1, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, id, time.Now().Unix())
	w, ok, err := svc.Claim(ctx, "w-crash", time.Now().Unix())
	if err != nil || !ok {
		t.Fatal(err)
	}
	out, err := svc.SubmitWithOutcome(ctx, SubmitRequest{
		WorkerID: "w-crash", WorkID: w.WorkID, CampaignID: id, ItemID: w.ItemID,
		InputN: w.InputN, ActualInput: w.ActualInput, InputBytes: w.InputBytes,
		CheckResult: 0, DurationMS: 1, SegmentExecDone: w.ExecPerUnit,
		Trap: "out of bounds memory access",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.ReplayStatus != replayReasonCrashClaim {
		t.Fatalf("crash claim status=%q want %q", out.ReplayStatus, replayReasonCrashClaim)
	}
	// Fabricated trap without real WASM fault must not become a finding.
	var findings int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_findings WHERE campaign_id=?`, id).Scan(&findings)
	if findings != 0 {
		t.Fatalf("unconfirmed crash claim must not mint finding, got %d", findings)
	}
}

func TestZZReport35LeaseStealStillBlocked(t *testing.T) {
	t.Setenv("HACKME_POOL_REPLAY_SAMPLE_PCT", "0")
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "zz35lease.db"))
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
	id := "zz35-lease"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "property", Status: "running", BudgetRuns: 1, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, id, time.Now().Unix())
	w, ok, err := svc.Claim(ctx, "owner", time.Now().Unix())
	if err != nil || !ok {
		t.Fatal(err)
	}
	err = svc.Submit(ctx, SubmitRequest{
		WorkerID: "thief", WorkID: w.WorkID, CampaignID: id, ItemID: w.ItemID,
		InputN: w.InputN, ActualInput: w.ActualInput, InputBytes: w.InputBytes,
		CheckResult: 0, DurationMS: 1, SegmentExecDone: w.ExecPerUnit,
	})
	if err == nil || !strings.Contains(err.Error(), "leased by another") {
		t.Fatalf("lease steal must fail, err=%v", err)
	}
}
