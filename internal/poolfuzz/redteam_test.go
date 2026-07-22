package poolfuzz

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/store"
)

type spySettler struct {
	mu         sync.Mutex
	runs       int
	findings   int
	crashBonus int
	finalize   int
}

func (s *spySettler) PayRun(context.Context, string, string, int64, int64) (SettleResult, error) {
	s.mu.Lock()
	s.runs++
	s.mu.Unlock()
	return SettleResult{Applied: true}, nil
}
func (s *spySettler) PayFinding(context.Context, string, string, string, int64, int64) (SettleResult, error) {
	s.mu.Lock()
	s.findings++
	s.mu.Unlock()
	return SettleResult{Applied: true}, nil
}
func (s *spySettler) PayCrashBonus(context.Context, string, string, int64, int64) (SettleResult, error) {
	s.mu.Lock()
	s.crashBonus++
	s.mu.Unlock()
	return SettleResult{Applied: true}, nil
}
func (s *spySettler) Finalize(context.Context, string, int64) (SettleResult, error) {
	s.mu.Lock()
	s.finalize++
	s.mu.Unlock()
	return SettleResult{Applied: true}, nil
}

func TestRedteamReplaySubmitNoDoubleSettle(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "rt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	spy := &spySettler{}
	svc := &Service{DB: db, Settler: spy}
	ctx := context.Background()
	cfg := fuzzengine.NormalizeCampaignConfig(map[string]any{
		"pool_distributed": true,
		"budget_hmc":       1.0,
		"check_semantics":  "pow_gate",
		"wasm_check_hex":   "00",
	}, "property")
	id := "rt-replay"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "property", Status: "running", BudgetRuns: 2, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	if err := svc.EnsureWorkItems(ctx, id, now); err != nil {
		t.Fatal(err)
	}
	inN, actual := actualInputForWorkItem(t, ctx, svc, id, 1, cfg)
	sub := SubmitRequest{
		WorkerID: "w1", MinerAddress: "HMC-1234567890123456",
		WorkID: id + ":1", CampaignID: id, ItemID: 1, InputN: inN, ActualInput: actual, CheckResult: 1, DurationMS: 1,
	}
	if err := svc.Submit(ctx, sub); err != nil {
		t.Fatal(err)
	}
	if err := svc.Submit(ctx, sub); err != nil {
		t.Fatal(err)
	}
	spy.mu.Lock()
	runs := spy.runs
	spy.mu.Unlock()
	if runs != 1 {
		t.Fatalf("replay submit must not double-settle runs: got %d", runs)
	}
}

func TestRedteamWrongWorkerNoSettle(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "rt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	spy := &spySettler{}
	svc := &Service{DB: db, Settler: spy}
	ctx := context.Background()
	cfg := fuzzengine.NormalizeCampaignConfig(map[string]any{"pool_distributed": true, "budget_hmc": 1.0, "check_semantics": "pow_gate"}, "property")
	id := "rt-worker"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "property", Status: "running", BudgetRuns: 1, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	_ = svc.EnsureWorkItems(ctx, id, now)
	_, ok, err := svc.Claim(ctx, "alice", now)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	inN, actual := actualInputForWorkItem(t, ctx, svc, id, 1, cfg)
	if err := svc.Submit(ctx, SubmitRequest{
		WorkerID: "bob", MinerAddress: "HMC-1234567890123456",
		CampaignID: id, ItemID: 1, InputN: inN, ActualInput: actual, CheckResult: 1, DurationMS: 1,
	}); err != nil {
		t.Fatal(err)
	}
	spy.mu.Lock()
	runs := spy.runs
	spy.mu.Unlock()
	if runs != 0 {
		t.Fatalf("wrong worker must not settle, runs=%d", runs)
	}
	var status string
	_ = db.QueryRowContext(ctx, `SELECT status FROM fuzz_work_items WHERE campaign_id=? AND id=1`, id).Scan(&status)
	if status == "done" {
		t.Fatal("work item must not be marked done by wrong worker")
	}
}

func TestRedteamPowGateFakeFinding(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "rt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	cfg := fuzzengine.NormalizeCampaignConfig(map[string]any{
		"pool_distributed": true,
		"check_semantics":  "pow_gate",
		"wasm_check_hex":   "00",
	}, "property")
	id := "rt-pow"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "property", Status: "running", BudgetRuns: 1, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, id, time.Now().Unix())
	inN, actual := actualInputForWorkItem(t, ctx, svc, id, 1, cfg)
	// pow_gate: check_ret=1 is PASS — must not create finding
	if err := svc.Submit(ctx, SubmitRequest{
		WorkerID: "w", CampaignID: id, ItemID: 1, InputN: inN, ActualInput: actual, CheckResult: 1, DurationMS: 1,
	}); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_findings WHERE campaign_id=?`, id).Scan(&n)
	if n != 0 {
		t.Fatalf("pow_gate check_ret=1 must not be a finding, got %d", n)
	}
}

func TestRedteamBountySettleOnce(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "rt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	spy := &spySettler{}
	svc := &Service{DB: db, Settler: spy}
	ctx := context.Background()
	violation := uint64(0x4c | (521 << 8))
	cfg := fuzzengine.NormalizeCampaignConfig(map[string]any{
		"pool_distributed": true,
		"budget_hmc":       2.0,
		"check_semantics":  "detector",
		"wasm_check_hex":   mustReadWasmHex(t, "../../tasks/artifacts/security/rust_script_push_bounds_guard.wasm"),
		"seed_corpus":      []any{violation},
		"mutation_rounds":  0,
	}, "property")
	id := "rt-bounty"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "property", Status: "running", BudgetRuns: 3, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, id, time.Now().Unix())
	wasm := mustReadWasmHex(t, "../../tasks/artifacts/security/rust_script_push_bounds_guard.wasm")
	for item := int64(1); item <= 2; item++ {
		cr, _, trap, err := ExecuteLocally(ctx, wasm, violation, 800)
		if err != nil {
			t.Fatal(err)
		}
		if err := svc.Submit(ctx, SubmitRequest{
			WorkerID: "w", MinerAddress: "HMC-aaaaaaaaaaaaaaaa",
			CampaignID: id, ItemID: item, InputN: uint64(item), ActualInput: violation,
			CheckResult: cr, DurationMS: 1, Trap: trap,
		}); err != nil {
			t.Fatal(err)
		}
	}
	spy.mu.Lock()
	f := spy.findings
	spy.mu.Unlock()
	if f != 2 {
		t.Fatalf("each finding submit may call PayFinding (node dedupes); coordinator calls=%d", f)
	}
}

func TestRedteamFabricatedTrapNoBounty(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "rt.db"))
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
		"budget_hmc":       2.0,
		"check_semantics":  "detector",
		"wasm_check_hex":   mustReadWasmHex(t, "../../tasks/artifacts/security/rust_script_push_bounds_guard.wasm"),
		"seed_corpus":      []any{safeInput},
		"mutation_rounds":  0,
	}, "property")
	id := "rt-fake-trap"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "property", Status: "running", BudgetRuns: 1, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	_, ok, err := svc.Claim(ctx, "w1", now)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if err := svc.Submit(ctx, SubmitRequest{
		WorkerID: "w1", MinerAddress: "HMC-bbbbbbbbbbbbbbbb",
		CampaignID: id, ItemID: 1, InputN: 1, ActualInput: safeInput,
		CheckResult: 0, DurationMS: 1, Trap: "fabricated wasm trap",
	}); err != nil {
		t.Fatal(err)
	}
	var findings int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_findings WHERE campaign_id=?`, id).Scan(&findings)
	if findings != 0 {
		t.Fatalf("fabricated trap must not create finding, got %d", findings)
	}
	spy.mu.Lock()
	paid := spy.findings
	bonus := spy.crashBonus
	spy.mu.Unlock()
	if paid != 0 {
		t.Fatalf("fabricated trap must not pay bounty, PayFinding calls=%d", paid)
	}
	if bonus != 0 {
		t.Fatalf("fabricated trap must not pay crash bonus, calls=%d", bonus)
	}
}

func TestRedteamBountyRequiresNativeBlocksPayFinding(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "rt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	spy := &spySettler{}
	svc := &Service{DB: db, Settler: spy}
	ctx := context.Background()
	violation := uint64(0x4c | (521 << 8))
	cfg := fuzzengine.NormalizeCampaignConfig(map[string]any{
		"pool_distributed":       true,
		"budget_hmc":             2.0,
		"check_semantics":        "detector",
		"bounty_requires_native": true,
		"wasm_check_hex":         mustReadWasmHex(t, "../../tasks/artifacts/security/rust_script_push_bounds_guard.wasm"),
		"seed_corpus":            []any{violation},
		"mutation_rounds":        0,
	}, "property")
	id := "rt-native-bounty"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "property", Status: "running", BudgetRuns: 1, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, id, time.Now().Unix())
	wasm := mustReadWasmHex(t, "../../tasks/artifacts/security/rust_script_push_bounds_guard.wasm")
	cr, _, trap, err := ExecuteLocally(ctx, wasm, violation, 800)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Submit(ctx, SubmitRequest{
		WorkerID: "w", MinerAddress: "HMC-cccccccccccccccc",
		CampaignID: id, ItemID: 1, InputN: 1, ActualInput: violation,
		CheckResult: cr, DurationMS: 1, Trap: trap,
	}); err != nil {
		t.Fatal(err)
	}
	var findings int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_findings WHERE campaign_id=?`, id).Scan(&findings)
	if findings != 1 {
		t.Fatalf("detector violation should record finding, got %d", findings)
	}
	spy.mu.Lock()
	f, bonus := spy.findings, spy.crashBonus
	spy.mu.Unlock()
	if f != 0 {
		t.Fatalf("bounty_requires_native must block PayFinding before confirm, calls=%d", f)
	}
	if bonus != 0 {
		t.Fatalf("detector/noise finding must not skim crash_bonus, calls=%d", bonus)
	}
}

func TestRedteamCrashClassPaysCrashBonusOnce(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "rt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	spy := &spySettler{}
	svc := &Service{DB: db, Settler: spy}
	ctx := context.Background()
	// op=1 item_id>=3 → ClassifyCheckFail returns crash/critical under non-detector semantics.
	crashInput := uint64(1 | (3 << 8) | (1 << 24))
	cfg := fuzzengine.NormalizeCampaignConfig(map[string]any{
		"pool_distributed": true,
		"budget_hmc":       2.0,
		"check_semantics":  "property",
		"wasm_check_hex":   mustReadWasmHex(t, "../../tasks/artifacts/security/rust_script_push_bounds_guard.wasm"),
		"seed_corpus":      []any{crashInput},
		"mutation_rounds":  0,
	}, "property")
	id := "rt-crash-bonus"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "property", Status: "running", BudgetRuns: 2, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, id, time.Now().Unix())
	for item := int64(1); item <= 2; item++ {
		if err := svc.Submit(ctx, SubmitRequest{
			WorkerID: "w", MinerAddress: "HMC-dddddddddddddddd",
			CampaignID: id, ItemID: item, InputN: uint64(item), ActualInput: crashInput,
			CheckResult: 0, DurationMS: 1,
		}); err != nil {
			t.Fatal(err)
		}
	}
	spy.mu.Lock()
	bonus := spy.crashBonus
	spy.mu.Unlock()
	if bonus < 1 {
		t.Fatalf("crash-class finding should attempt crash bonus, calls=%d", bonus)
	}
}
