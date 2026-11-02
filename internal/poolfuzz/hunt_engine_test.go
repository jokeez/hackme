package poolfuzz

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"hackme/internal/fuzzengine"
	"hackme/internal/hunt"
	"hackme/internal/store"
)

func TestHuntEngineByteFallbackDefaultOff(t *testing.T) {
	t.Setenv("HACKME_HUNT_ENGINE_BYTE_FALLBACK", "")
	if HuntEngineAllowByteFallback() {
		t.Fatal("default OFF for SKU honesty")
	}
	t.Setenv("HACKME_HUNT_ENGINE_BYTE_FALLBACK", "1")
	if !HuntEngineAllowByteFallback() {
		t.Fatal("explicit ON")
	}
	t.Setenv("HACKME_HUNT_ENGINE_BYTE_FALLBACK_DEEP", "")
	if HuntEngineAllowByteFallbackForConfig(map[string]any{"hunt_package": "hunt_heavy"}) {
		t.Fatal("deep must not fallback without DEEP=1")
	}
}

func TestDecideHuntEnginePayFloorOnlyBlocksEdges(t *testing.T) {
	cfg := map[string]any{"hunt_edge_pay": "floor_only"}
	kind, full := DecideHuntEnginePayForConfig(cfg, 9, 3, 0, 0, false)
	if kind != HuntEnginePayFloor || full {
		t.Fatalf("floor_only edges: kind=%s full=%v", kind, full)
	}
	kind, full = DecideHuntEnginePayForConfig(cfg, 0, 0, 1, 0, false)
	if kind != HuntEnginePayCrash || !full {
		t.Fatalf("floor_only crash must still full-pay: kind=%s full=%v", kind, full)
	}
	cfg2 := map[string]any{"hunt_edge_pay": "showmap"}
	kind, full = DecideHuntEnginePayForConfig(cfg2, 2, 1, 0, 0, false)
	if kind != HuntEnginePayEdges || !full {
		t.Fatalf("showmap edges: kind=%s full=%v", kind, full)
	}
}

func TestDecideHuntEnginePayIgnoresMinerEdges(t *testing.T) {
	t.Setenv("HACKME_HUNT_COVERAGE_ENGINE", "1")
	// Fake miner edges must not unlock full pay without coordinator proof.
	kind, full := DecideHuntEnginePay(0, 0, 0, 9999, true)
	if full || kind != HuntEnginePayFloor {
		t.Fatalf("empty CLEAN want floor, got kind=%s full=%v", kind, full)
	}
	kind, full = DecideHuntEnginePay(3, 2, 0, 0, false)
	if !full || kind != HuntEnginePayEdges {
		t.Fatalf("proven edges want full, got kind=%s full=%v", kind, full)
	}
	kind, full = DecideHuntEnginePay(0, 0, 1, 0, false)
	if !full || kind != HuntEnginePayCrash {
		t.Fatalf("crash want full, got kind=%s full=%v", kind, full)
	}
	kind, full = DecideHuntEnginePay(0, 5, 0, 100, true)
	if full || kind != HuntEnginePayFloor {
		t.Fatalf("seeds without proven edges → floor, got kind=%s full=%v", kind, full)
	}
}

func TestWorkerSupportsHuntEngine(t *testing.T) {
	if WorkerSupportsHuntEngine(nil, hunt.EngineLibFuzzer) {
		t.Fatal("legacy must not imply libfuzzer")
	}
	if !WorkerSupportsHuntEngine(nil, hunt.EngineByte) {
		t.Fatal("legacy implies byte")
	}
	ads := []string{hunt.EngineLibFuzzer, hunt.EngineByte}
	if !WorkerSupportsHuntEngine(ads, hunt.EngineLibFuzzer) {
		t.Fatal("libfuzzer advertised")
	}
	if WorkerSupportsHuntEngine(ads, hunt.EngineAFLPP) {
		t.Fatal("afl not advertised")
	}
}

func TestWorkerHuntEnginesParse(t *testing.T) {
	got := WorkerHuntEngines("libfuzzer,aflpp,byte,libfuzzer")
	if len(got) != 3 {
		t.Fatalf("got %#v", got)
	}
}

func TestLeaseProvenTrack(t *testing.T) {
	clearLeaseProven("c1", 7)
	noteLeaseProven("c1", 7, 2, 1, 0, hunt.EngineLibFuzzer, "v1")
	noteLeaseProven("c1", 7, 1, 0, 1, "", "")
	tr := peekLeaseProven("c1", 7)
	if tr.SeedsAccepted != 3 || tr.ProvenEdges != 1 || tr.CrashesOK != 1 {
		t.Fatalf("peek %#v", tr)
	}
	taken := takeLeaseProven("c1", 7)
	if taken.ProvenEdges != 1 {
		t.Fatalf("take %#v", taken)
	}
	if peekLeaseProven("c1", 7).SeedsAccepted != 0 {
		t.Fatal("should be cleared")
	}
}

func TestStampHuntProvenOnSubmitKeepsAudit(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "stamp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := &Service{DB: db}
	cid := "camp-stamp"
	_, err = db.Exec(`INSERT INTO fuzz_campaigns(id, campaign_type, title, status, config_json, created_at)
		VALUES(?,?,?,?,?,1)`, cid, "hunt", "t", "running", `{"work_kind":"hunt_shard"}`)
	if err != nil {
		t.Fatal(err)
	}
	res, err := db.Exec(`INSERT INTO fuzz_work_items(campaign_id, input_n, status, lease_owner, created_at, updated_at)
		VALUES(?,?,?,?,?,?)`, cid, 1, "leased", "worker-stamp", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	itemID, _ := res.LastInsertId()
	svc.noteLeaseProvenPersist(context.Background(), cid, itemID, "worker-stamp", 3, 2, 0, hunt.EngineLibFuzzer, "libfuzzer")
	proven := svc.takeLeaseProvenPersist(context.Background(), cid, itemID)
	svc.stampHuntProvenOnSubmit(context.Background(), cid, itemID, "worker-stamp", proven, hunt.EngineLibFuzzer, "libfuzzer-vtest")
	var edges, seeds int
	var eng, ver string
	if err := db.QueryRow(`SELECT hunt_proven_edges, hunt_proven_seeds, hunt_proven_engine, hunt_proven_engine_ver
		FROM fuzz_work_items WHERE campaign_id=? AND id=?`, cid, itemID).Scan(&edges, &seeds, &eng, &ver); err != nil {
		t.Fatal(err)
	}
	if edges != 2 || seeds != 3 || eng != hunt.EngineLibFuzzer || ver != "libfuzzer-vtest" {
		t.Fatalf("stamp edges=%d seeds=%d eng=%q ver=%q", edges, seeds, eng, ver)
	}
	// Foreign stamp must not overwrite after ownership moved.
	_, _ = db.Exec(`UPDATE fuzz_work_items SET lease_owner='bob' WHERE id=?`, itemID)
	svc.stampHuntProvenOnSubmit(context.Background(), cid, itemID, "alice", leaseProvenTrack{ProvenEdges: 99, SeedsAccepted: 99}, hunt.EngineLibFuzzer, "evil")
	_ = db.QueryRow(`SELECT hunt_proven_edges, hunt_proven_seeds FROM fuzz_work_items WHERE id=?`, itemID).Scan(&edges, &seeds)
	if edges != 2 || seeds != 3 {
		t.Fatalf("foreign stamp overwrote: edges=%d seeds=%d", edges, seeds)
	}
}

func TestNoteLeaseProvenBoundToOwnerAndClearedOnTransfer(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "bind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := &Service{DB: db}
	ctx := context.Background()
	cid := "camp-proven-bind"
	_, err = db.Exec(`INSERT INTO fuzz_campaigns(id, campaign_type, title, status, config_json, created_at)
		VALUES(?,?,?,?,?,1)`, cid, "hunt", "t", "running", `{"work_kind":"hunt_shard","pool_distributed":true}`)
	if err != nil {
		t.Fatal(err)
	}
	res, err := db.Exec(`INSERT INTO fuzz_work_items(campaign_id, input_n, status, lease_owner, lease_until, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?)`, cid, 1, "leased", "alice", 9999999999, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	itemID, _ := res.LastInsertId()

	// Foreign owner must not credit prove.
	svc.noteLeaseProvenPersist(ctx, cid, itemID, "eve", 9, 9, 0, hunt.EngineLibFuzzer, "evil")
	var edges, seeds int
	_ = db.QueryRow(`SELECT hunt_proven_edges, hunt_proven_seeds FROM fuzz_work_items WHERE id=?`, itemID).
		Scan(&edges, &seeds)
	if edges != 0 || seeds != 0 {
		t.Fatalf("foreign prove leaked: edges=%d seeds=%d", edges, seeds)
	}
	if peekLeaseProven(cid, itemID).ProvenEdges != 0 {
		t.Fatal("foreign prove must not touch memory either")
	}

	svc.noteLeaseProvenPersist(ctx, cid, itemID, "alice", 2, 5, 0, hunt.EngineLibFuzzer, "ok")
	_ = db.QueryRow(`SELECT hunt_proven_edges, hunt_proven_seeds FROM fuzz_work_items WHERE id=?`, itemID).
		Scan(&edges, &seeds)
	if edges != 5 || seeds != 2 {
		t.Fatalf("owner prove: edges=%d seeds=%d", edges, seeds)
	}

	// Release clears durable + memory so successor cannot inherit PayRun.
	ok, err := svc.ReleaseWorkLease(ctx, cid, itemID, "alice")
	if err != nil || !ok {
		t.Fatalf("release: ok=%v err=%v", ok, err)
	}
	_ = db.QueryRow(`SELECT hunt_proven_edges, hunt_proven_seeds FROM fuzz_work_items WHERE id=?`, itemID).
		Scan(&edges, &seeds)
	if edges != 0 || seeds != 0 {
		t.Fatalf("after release want zero prove, got edges=%d seeds=%d", edges, seeds)
	}
	if peekLeaseProven(cid, itemID).ProvenEdges != 0 {
		t.Fatal("memory prove must clear on release")
	}

	// Reclaim keeps durable prove for force-complete, clears memory only.
	_, _ = db.Exec(`UPDATE fuzz_work_items SET status='leased', lease_owner='alice', lease_until=1,
		hunt_proven_edges=7, hunt_proven_seeds=3 WHERE id=?`, itemID)
	noteLeaseProven(cid, itemID, 3, 7, 0, hunt.EngineLibFuzzer, "stale")
	now := int64(100)
	n, err := svc.ReclaimExpiredLeases(ctx, now)
	if err != nil || n != 1 {
		t.Fatalf("reclaim n=%d err=%v", n, err)
	}
	var st string
	_ = db.QueryRow(`SELECT status, hunt_proven_edges, hunt_proven_seeds FROM fuzz_work_items WHERE id=?`, itemID).
		Scan(&st, &edges, &seeds)
	if st != "pending" || edges != 7 || seeds != 3 {
		t.Fatalf("reclaim should keep durable prove: status=%s edges=%d seeds=%d", st, edges, seeds)
	}
	if peekLeaseProven(cid, itemID).ProvenEdges != 0 {
		t.Fatal("memory must clear on reclaim")
	}

	// Successor claim zeros prove so bob cannot inherit alice PayRun credit.
	_, _ = db.Exec(`UPDATE fuzz_campaigns SET status='running' WHERE id=?`, cid)
	cfgJSON := `{"pool_distributed":true,"work_kind":"hunt_shard","check_semantics":"detector","wasm_check_hex":"0061736d0100000001060160017e017f0302010007090105636865636b00000a0601040041010b"}`
	_, _ = db.Exec(`UPDATE fuzz_campaigns SET config_json=? WHERE id=?`, cfgJSON, cid)
	// claimOnePendingInCampaign requires harness for Hunt — use Dig-style property instead.
	_, _ = db.Exec(`UPDATE fuzz_campaigns SET campaign_type='property', config_json=? WHERE id=?`,
		`{"pool_distributed":true,"check_semantics":"detector","wasm_check_hex":"0061736d0100000001060160017e017f0302010007090105636865636b00000a0601040041010b"}`, cid)
	work, ok, err := svc.claimOnePendingInCampaign(ctx, "bob", cid, now+10)
	if err != nil || !ok {
		t.Fatalf("claim successor: ok=%v err=%v", ok, err)
	}
	_ = db.QueryRow(`SELECT hunt_proven_edges, hunt_proven_seeds FROM fuzz_work_items WHERE id=?`, work.ItemID).
		Scan(&edges, &seeds)
	if edges != 0 || seeds != 0 {
		t.Fatalf("successor lease must zero prove: edges=%d seeds=%d", edges, seeds)
	}
}

func TestApplyHuntEngineByteFallback(t *testing.T) {
	w := &ClaimedWork{
		HuntEngine:         hunt.EngineLibFuzzer,
		EngineLeaseSec:     60,
		ExecPerUnit:        1,
		IterationsPerShard: 48,
		CoverageKind:       "hunt_libfuzzer",
	}
	ApplyHuntEngineByteFallback(w, map[string]any{"iterations_per_shard": 48})
	if w.HuntEngine != hunt.EngineByte || w.EngineLeaseSec != 0 || w.ExecPerUnit != 48 {
		t.Fatalf("fallback %#v", w)
	}
}

func TestRegisterCampaignStampsHuntLiteEngine(t *testing.T) {
	t.Setenv("HACKME_HUNT_COVERAGE_ENGINE", "1")
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "stamp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	id := "hunt-stamp-lite"
	cfg := map[string]any{
		"pool_distributed": true,
		"work_kind":        "hunt_shard",
		"campaign_type":    "hunt",
		"hunt_package":     "hunt_lite",
		"budget_hmc":       20,
		"budget_shards":    8,
		// intentionally omit hunt_engine (legacy bootstrap nodes)
	}
	if err := svc.RegisterCampaign(context.Background(), Campaign{
		ID: id, CampaignType: "hunt", Status: "running", BudgetRuns: 8, Config: cfg,
	}); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := db.QueryRow(`SELECT config_json FROM fuzz_campaigns WHERE id=?`, id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	got := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if eng, _ := got["hunt_engine"].(string); eng != hunt.EngineLibFuzzer {
		t.Fatalf("hunt_engine=%v want libfuzzer (raw keys=%v)", got["hunt_engine"], got)
	}
	if prof, _ := got["hunt_engine_profile"].(string); prof != hunt.EngineProfileFleet {
		t.Fatalf("profile=%v", got["hunt_engine_profile"])
	}
}

func TestHuntSubmitExecExpected(t *testing.T) {
	t.Setenv("HACKME_HUNT_COVERAGE_ENGINE", "1")
	cfg := map[string]any{"iterations_per_shard": 48, "work_kind": "hunt_shard", "hunt_engine": "libfuzzer"}
	if got := huntSubmitExecExpected(cfg, SubmitRequest{HuntEngine: hunt.EngineLibFuzzer}); got != 1 {
		t.Fatalf("engine want 1 got %d", got)
	}
	if got := huntSubmitExecExpected(cfg, SubmitRequest{HuntEngine: hunt.EngineByte}); got != 48 {
		t.Fatalf("byte want 48 got %d", got)
	}
	// Legacy omit → L1 iterations (must not infer libfuzzer from campaign cfg).
	if got := huntSubmitExecExpected(cfg, SubmitRequest{}); got != 48 {
		t.Fatalf("legacy omit want 48 got %d", got)
	}
	if huntEngineSubmitMode(SubmitRequest{}) {
		t.Fatal("empty hunt_engine must not be engine mode")
	}
}

func TestBuildHuntClaimCoverageEngine(t *testing.T) {
	t.Setenv("HACKME_HUNT_COVERAGE_ENGINE", "1")
	t.Setenv("HACKME_HUNT_ENGINE", "libfuzzer")
	// Smoke: parsing + exec expected only (full claim needs harness DB).
	cfg := map[string]any{
		"work_kind": "hunt_shard", "campaign_type": "hunt",
		"hunt_engine": "libfuzzer", "iterations_per_shard": 48,
	}
	if eng := hunt.HuntEngineFromConfig(cfg); eng != hunt.EngineLibFuzzer {
		t.Fatalf("engine %q", eng)
	}
	if !HuntEngineEnabled() {
		t.Fatal("enabled")
	}
}

func TestCheckHuntEngineWallFloor(t *testing.T) {
	t.Setenv("HACKME_HUNT_COVERAGE_ENGINE", "1")
	t.Setenv("HACKME_HUNT_ENGINE_MIN_WALL_MS", "")
	cfg := map[string]any{"hunt_engine": "libfuzzer", "engine_lease_sec": 60}
	req := SubmitRequest{HuntEngine: "libfuzzer", DurationMS: 999_999} // client ms ignored
	if err := checkHuntEngineWallFloor(req, cfg, 2); err == nil {
		t.Fatal("server wall 2ms must reject")
	}
	if err := checkHuntEngineWallFloor(req, cfg, 55_000); err != nil {
		t.Fatalf("server wall 55s ok: %v", err)
	}
	t.Setenv("HACKME_HUNT_ENGINE_MIN_WALL_MS", "0")
	if err := checkHuntEngineWallFloor(req, cfg, 1); err != nil {
		t.Fatalf("escape: %v", err)
	}
}

func TestProveHuntCorpusSeedEdgesRequiresHarnessExec(t *testing.T) {
	t.Setenv("HACKME_HUNT_COVERAGE_ENGINE", "1")
	t.Setenv("HACKME_POOL_HUNT_REPLAY", "1")
	orig := huntEngineSeedExec
	t.Cleanup(func() { huntEngineSeedExec = orig })

	// Without harness exec, structural novelty must not count.
	huntEngineSeedExec = func(context.Context, *Service, string, map[string]any, []byte) (bool, bool, string, error) {
		return false, false, "", nil
	}
	svc := &Service{}
	cfg := map[string]any{
		"work_kind": "hunt_shard", "campaign_type": "hunt",
		"upstream_target_id": "jsmn", "harness_hash": "x",
	}
	seeds := []fuzzengine.PoolCorpusSeed{{InputBytes: []byte(`{"novel":true}`), Energy: 2}}
	n, err := svc.ProveHuntCorpusSeedEdges(context.Background(), "c-prove", cfg, seeds, 1)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("want 0 proven without harness exec, got %d", n)
	}

	// Harness CLEAN + new bucket → counts (recordCoverage needs DB; use stub Service with nil DB
	// would fail — only assert crash path skips edge count).
	huntEngineSeedExec = func(context.Context, *Service, string, map[string]any, []byte) (bool, bool, string, error) {
		return true, true, "hunt_crash:test", nil
	}
	n, err = svc.ProveHuntCorpusSeedEdges(context.Background(), "c-prove", cfg, seeds, 1)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("crash seeds must not count as edge prove, got %d", n)
	}
}
