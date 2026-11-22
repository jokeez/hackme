package poolfuzz

import (
	"context"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/hunt"
	"hackme/internal/store"
)

func TestValidResearchCorpusNamespace(t *testing.T) {
	if !ValidResearchCorpusNamespace("research:jsmn") {
		t.Fatal("research:jsmn")
	}
	if ValidResearchCorpusNamespace("pack:secrets") {
		t.Fatal("customer Dig pack namespace must be rejected")
	}
	if ValidResearchCorpusNamespace("research:../etc") {
		t.Fatal("path inject")
	}
	if ValidResearchCorpusNamespace("research:") {
		t.Fatal("empty target")
	}
	if ResearchNamespaceForTarget("jsmn") != "research:jsmn" {
		t.Fatalf("got %q", ResearchNamespaceForTarget("jsmn"))
	}
}

func TestAcceptResearchCorpusDeltaRequiresLease(t *testing.T) {
	svc, id, itemID := researchDeltaHuntCamp(t)
	ctx := context.Background()
	_, err := svc.AcceptResearchCorpusDelta(ctx, ResearchCorpusDeltaRequest{
		WorkerID: "w1", CampaignID: id, ItemID: itemID, Namespace: "research:jsmn",
		Seeds: []fuzzengine.PoolCorpusSeed{{InputBytes: []byte("abc"), Energy: 2}},
	})
	if err == nil || !strings.Contains(err.Error(), "active lease") {
		t.Fatalf("want lease error, got %v", err)
	}
}

func TestAcceptResearchCorpusDeltaRejectsPackNamespace(t *testing.T) {
	svc, id, itemID := researchDeltaHuntCamp(t)
	ctx := context.Background()
	leaseWorkItemForTest(t, ctx, svc.DB, id, itemID, "w1")
	_, err := svc.AcceptResearchCorpusDelta(ctx, ResearchCorpusDeltaRequest{
		WorkerID: "w1", CampaignID: id, ItemID: itemID, Namespace: "pack:secrets",
		Seeds: []fuzzengine.PoolCorpusSeed{{InputBytes: []byte("abc"), Energy: 2}},
	})
	if err == nil || !strings.Contains(err.Error(), "research") {
		t.Fatalf("want research namespace error, got %v", err)
	}
}

func TestAcceptResearchCorpusDeltaRejectsCrossCampaignNamespace(t *testing.T) {
	svc, id, itemID := researchDeltaHuntCamp(t)
	ctx := context.Background()
	leaseWorkItemForTest(t, ctx, svc.DB, id, itemID, "w1")
	_, err := svc.AcceptResearchCorpusDelta(ctx, ResearchCorpusDeltaRequest{
		WorkerID: "w1", CampaignID: id, ItemID: itemID, Namespace: "research:other",
		Seeds: []fuzzengine.PoolCorpusSeed{{InputBytes: []byte("abc"), Energy: 2}},
	})
	if err == nil || !strings.Contains(err.Error(), "match") {
		t.Fatalf("want namespace match error, got %v", err)
	}
}

func TestAcceptResearchCorpusDeltaSeedsOK(t *testing.T) {
	svc, id, itemID := researchDeltaHuntCamp(t)
	ctx := context.Background()
	leaseWorkItemForTest(t, ctx, svc.DB, id, itemID, "w1")
	out, err := svc.AcceptResearchCorpusDelta(ctx, ResearchCorpusDeltaRequest{
		WorkerID: "w1", CampaignID: id, ItemID: itemID, Namespace: "research:jsmn",
		Seeds: []fuzzengine.PoolCorpusSeed{{InputBytes: []byte("new-unit-1"), Energy: 3}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.SeedsAccepted != 1 {
		t.Fatalf("seeds=%d", out.SeedsAccepted)
	}
	nsSeeds, err := svc.ListNamespaceCorpus(ctx, "research:jsmn", 64)
	if err != nil || len(nsSeeds) < 1 {
		t.Fatalf("namespace seeds=%d err=%v", len(nsSeeds), err)
	}
	campSeeds, err := svc.loadPoolCorpusSeeds(ctx, id, 64)
	if err != nil || len(campSeeds) < 1 {
		t.Fatalf("campaign seeds=%d err=%v", len(campSeeds), err)
	}
}

func TestAcceptResearchCorpusDeltaFakeCrashRejected(t *testing.T) {
	svc, id, itemID := researchDeltaHuntCamp(t)
	ctx := context.Background()
	leaseWorkItemForTest(t, ctx, svc.DB, id, itemID, "w1")
	prev := researchCrashReplayer
	t.Cleanup(func() { researchCrashReplayer = prev })
	researchCrashReplayer = func(ctx context.Context, s *Service, campaignID string, cfg map[string]any, input []byte) (bool, string, error) {
		return false, "hunt_replay_reject:fake_crash", nil
	}
	out, err := svc.AcceptResearchCorpusDelta(ctx, ResearchCorpusDeltaRequest{
		WorkerID: "w1", CampaignID: id, ItemID: itemID, Namespace: "research:jsmn",
		Crashes: [][]byte{[]byte("not-a-real-crash")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.CrashesReplayOK != 0 || out.Findings != 0 || out.CrashesRejected != 1 {
		t.Fatalf("out=%+v", out)
	}
	var findings int
	_ = svc.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_findings WHERE campaign_id=?`, id).Scan(&findings)
	if findings != 0 {
		t.Fatalf("forged crash must not mint finding, got %d", findings)
	}
}

func TestAcceptResearchCorpusDeltaCrashReplayRecordsFinding(t *testing.T) {
	svc, id, itemID := researchDeltaHuntCamp(t)
	ctx := context.Background()
	leaseWorkItemForTest(t, ctx, svc.DB, id, itemID, "w1")
	prev := researchCrashReplayer
	t.Cleanup(func() { researchCrashReplayer = prev })
	researchCrashReplayer = func(ctx context.Context, s *Service, campaignID string, cfg map[string]any, input []byte) (bool, string, error) {
		return true, "hunt_crash:heap-buffer-overflow", nil
	}
	out, err := svc.AcceptResearchCorpusDelta(ctx, ResearchCorpusDeltaRequest{
		WorkerID: "w1", CampaignID: id, ItemID: itemID, Namespace: "research:jsmn",
		MinerAddr: "HMC-test",
		Crashes:   [][]byte{[]byte("crash-input")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.CrashesReplayOK != 1 || out.Findings != 1 {
		t.Fatalf("out=%+v", out)
	}
	var findings int
	_ = svc.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_findings WHERE campaign_id=?`, id).Scan(&findings)
	if findings != 1 {
		t.Fatalf("findings=%d", findings)
	}
	// No settle_finding auto-queued from research path (shard submit owns settle).
	var findSt string
	_ = svc.DB.QueryRowContext(ctx,
		`SELECT COALESCE(settle_finding_status,'') FROM fuzz_work_items WHERE campaign_id=? AND id=?`, id, itemID).Scan(&findSt)
	if findSt != "" {
		t.Fatalf("research slot must not enqueue settle_finding, got %q", findSt)
	}
}

func TestAcceptResearchCorpusDeltaStaleCrashNoPayCredit(t *testing.T) {
	t.Setenv("HACKME_HUNT_COVERAGE_ENGINE", "1")
	svc, id, itemID := researchDeltaHuntCamp(t)
	ctx := context.Background()
	leaseWorkItemForTest(t, ctx, svc.DB, id, itemID, "w1")
	prev := researchCrashReplayer
	t.Cleanup(func() { researchCrashReplayer = prev })
	researchCrashReplayer = func(ctx context.Context, s *Service, campaignID string, cfg map[string]any, input []byte) (bool, string, error) {
		return true, "hunt_crash:heap-buffer-overflow", nil
	}
	crash := []byte("same-crash-bytes")
	first, err := svc.AcceptResearchCorpusDelta(ctx, ResearchCorpusDeltaRequest{
		WorkerID: "w1", CampaignID: id, ItemID: itemID, Namespace: "research:jsmn",
		MinerAddr: "HMC-test", Crashes: [][]byte{crash},
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.CrashesReplayOK != 1 || first.Findings != 1 {
		t.Fatalf("first=%+v", first)
	}
	var provenCrashes int
	_ = svc.DB.QueryRowContext(ctx,
		`SELECT COALESCE(hunt_proven_crashes,0) FROM fuzz_work_items WHERE campaign_id=? AND id=?`, id, itemID).Scan(&provenCrashes)
	if provenCrashes != 1 {
		t.Fatalf("first DB hunt_proven_crashes=%d", provenCrashes)
	}
	clearLeaseProven(id, itemID)

	// New item re-uploads the same bytes — ASAN still passes, but no PayRun credit.
	now := time.Now().Unix()
	res, err := svc.DB.ExecContext(ctx,
		`INSERT INTO fuzz_work_items(campaign_id, input_n, status, created_at, updated_at) VALUES (?,?, 'pending', ?, ?)`,
		id, 424242, now, now)
	if err != nil {
		t.Fatal(err)
	}
	item2, _ := res.LastInsertId()
	leaseWorkItemForTest(t, ctx, svc.DB, id, item2, "w1")
	second, err := svc.AcceptResearchCorpusDelta(ctx, ResearchCorpusDeltaRequest{
		WorkerID: "w1", CampaignID: id, ItemID: item2, Namespace: "research:jsmn",
		MinerAddr: "HMC-test", Crashes: [][]byte{crash, crash}, // duplicate in-batch too
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.CrashesReplayOK != 0 || second.Findings != 0 {
		t.Fatalf("stale re-upload must not pay-credit, second=%+v", second)
	}
	if second.CrashesRejected < 1 {
		t.Fatalf("expected rejects, second=%+v", second)
	}
	var proven2 int
	_ = svc.DB.QueryRowContext(ctx,
		`SELECT COALESCE(hunt_proven_crashes,0) FROM fuzz_work_items WHERE campaign_id=? AND id=?`, id, item2).Scan(&proven2)
	if proven2 != 0 {
		t.Fatalf("second DB hunt_proven_crashes=%d want 0", proven2)
	}
	kind, full := DecideHuntEnginePay(0, 0, second.CrashesReplayOK, 0, false)
	if full || kind == HuntEnginePayCrash {
		t.Fatalf("pay decision must be floor, kind=%s full=%v", kind, full)
	}
}

func TestAcceptResearchCorpusDeltaMutatedCrashSameBucketNoPayCredit(t *testing.T) {
	// valax High: flip first-8 bytes while keeping the same ASAN trap — must not re-earn PayRun.
	t.Setenv("HACKME_HUNT_COVERAGE_ENGINE", "1")
	svc, id, itemID := researchDeltaHuntCamp(t)
	ctx := context.Background()
	leaseWorkItemForTest(t, ctx, svc.DB, id, itemID, "w1")
	prev := researchCrashReplayer
	t.Cleanup(func() { researchCrashReplayer = prev })
	// Same FormatHuntTrap-style trap for both inputs (crash-preserving mutation).
	const trap = "hunt_crash:heap-buffer-overflow"
	researchCrashReplayer = func(ctx context.Context, s *Service, campaignID string, cfg map[string]any, input []byte) (bool, string, error) {
		return true, trap, nil
	}
	firstCrash := []byte("AAAAAAAACrash!")
	first, err := svc.AcceptResearchCorpusDelta(ctx, ResearchCorpusDeltaRequest{
		WorkerID: "w1", CampaignID: id, ItemID: itemID, Namespace: "research:jsmn",
		MinerAddr: "HMC-test", Crashes: [][]byte{firstCrash},
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.CrashesReplayOK != 1 || first.Findings != 1 {
		t.Fatalf("first=%+v", first)
	}
	clearLeaseProven(id, itemID)

	now := time.Now().Unix()
	res, err := svc.DB.ExecContext(ctx,
		`INSERT INTO fuzz_work_items(campaign_id, input_n, status, created_at, updated_at) VALUES (?,?, 'pending', ?, ?)`,
		id, 424243, now, now)
	if err != nil {
		t.Fatal(err)
	}
	item2, _ := res.LastInsertId()
	leaseWorkItemForTest(t, ctx, svc.DB, id, item2, "w1")
	mutated := []byte("AAAAAAABCrash!") // first-8 u64 differs; same sanitizer trap
	second, err := svc.AcceptResearchCorpusDelta(ctx, ResearchCorpusDeltaRequest{
		WorkerID: "w1", CampaignID: id, ItemID: item2, Namespace: "research:jsmn",
		MinerAddr: "HMC-test", Crashes: [][]byte{mutated},
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.CrashesReplayOK != 0 || second.Findings != 0 {
		t.Fatalf("mutated same-bucket crash must not pay-credit, second=%+v", second)
	}
	kind, full := DecideHuntEnginePay(0, 0, second.CrashesReplayOK, 0, false)
	if full || kind == HuntEnginePayCrash {
		t.Fatalf("pay decision must be floor, kind=%s full=%v", kind, full)
	}
}

func TestAcceptResearchCorpusDeltaRejectsUnauthedForeignLease(t *testing.T) {
	svc, id, itemID := researchDeltaHuntCamp(t)
	ctx := context.Background()
	leaseWorkItemForTest(t, ctx, svc.DB, id, itemID, "owner")
	_, err := svc.AcceptResearchCorpusDelta(ctx, ResearchCorpusDeltaRequest{
		WorkerID: "attacker", CampaignID: id, ItemID: itemID, Namespace: "research:jsmn",
		Seeds: []fuzzengine.PoolCorpusSeed{{InputBytes: []byte("inject"), Energy: 9}},
	})
	if err == nil || !strings.Contains(err.Error(), "active lease") {
		t.Fatalf("cross-worker inject must fail, got %v", err)
	}
}

func TestAcceptResearchCorpusDeltaNextClaimKeepsSeed(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HACKME_REPO_ROOT", dir)
	t.Setenv("HACKME_POOL_HUNT_REPLAY", "0")
	t.Setenv("HACKME_POOL_HUNT_REPLAY_ASYNC", "0")
	prevResolve := resolveCoverageHarness
	prevExec := huntEngineSeedExec
	t.Cleanup(func() {
		resolveCoverageHarness = prevResolve
		huntEngineSeedExec = prevExec
	})
	resolveCoverageHarness = func(context.Context, string, string, ...hunt.CoverageHarnessOpts) (string, bool, error) {
		return "", false, nil
	}
	huntEngineSeedExec = func(context.Context, *Service, string, map[string]any, []byte) (bool, bool, string, error) {
		return false, false, "", nil
	}

	db, err := store.Open(filepath.Join(dir, "claim-wire.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := &Service{DB: db}
	ctx := context.Background()

	// Fill the wire budget with boot starters plus unique fillers. A first-fit
	// trim of this list drops anything appended after it.
	existing := make([]any, 0, 110)
	existing = append(existing, hex.EncodeToString([]byte("{}")))
	for i := 0; i < 109; i++ {
		existing = append(existing, hex.EncodeToString([]byte{0xB0, byte(i), byte(i >> 8), 0x11}))
	}
	fresh := []byte("feature-unit-from-miner-a")
	want := hex.EncodeToString(fresh)
	const hash = "c0ffee00c0ffee00c0ffee00c0ffee00c0ffee00c0ffee00c0ffee00c0ffee00"
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
		"seed_byte_corpus":     existing,
	}
	hunt.ApplyPoolGuidedDefaults(cfg, "jsmn")
	id := "research-delta-claim-wire"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "hunt", Status: "running", BudgetRuns: 2, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	hunt.SetHarnessObjectDir("")
	t.Cleanup(func() { hunt.SetHarnessObjectDir("") })
	if err := hunt.PutHarnessArtifact(ctx, db, hash, []byte("claim-wire-harness"), "jsmn"); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureWorkItems(ctx, id, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	var itemID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM fuzz_work_items WHERE campaign_id=? ORDER BY id LIMIT 1`, id).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	leaseWorkItemForTest(t, ctx, db, id, itemID, "miner-a")
	out, err := svc.AcceptResearchCorpusDelta(ctx, ResearchCorpusDeltaRequest{
		WorkerID: "miner-a", CampaignID: id, ItemID: itemID, Namespace: "research:jsmn",
		Seeds: []fuzzengine.PoolCorpusSeed{{InputBytes: fresh, Energy: 9}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.SeedsAccepted != 1 {
		t.Fatalf("accepted=%d", out.SeedsAccepted)
	}
	w, ok, err := svc.Claim(ctx, "miner-b", time.Now().Unix())
	if err != nil || !ok {
		t.Fatalf("next claim ok=%v err=%v", ok, err)
	}
	if len(w.SeedByteCorpus) == 0 || len(w.SeedByteCorpus) > defaultSeedCorpusWireMaxItems {
		t.Fatalf("claim trim len=%d", len(w.SeedByteCorpus))
	}
	if w.SeedByteCorpus[0] != want {
		t.Fatalf("just-accepted seed absent from claim trim head: first=%v want=%s len=%d", w.SeedByteCorpus[0], want, len(w.SeedByteCorpus))
	}
}

func researchDeltaHuntCamp(t *testing.T) (*Service, string, int64) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HACKME_REPO_ROOT", dir)
	db, err := store.Open(filepath.Join(dir, "research-delta.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := &Service{DB: db}
	ctx := context.Background()
	// Corpus-delta unit tests must not compile afl-cc / replay the catalog harness.
	// Crash credit goes through researchCrashReplayer; edge prove is a separate path.
	prevResolve := resolveCoverageHarness
	prevExec := huntEngineSeedExec
	t.Cleanup(func() {
		resolveCoverageHarness = prevResolve
		huntEngineSeedExec = prevExec
	})
	resolveCoverageHarness = func(context.Context, string, string, ...hunt.CoverageHarnessOpts) (string, bool, error) {
		return "", false, nil
	}
	huntEngineSeedExec = func(context.Context, *Service, string, map[string]any, []byte) (bool, bool, string, error) {
		return false, false, "", nil
	}
	cfg := map[string]any{
		"pool_distributed":     true,
		"work_kind":            "hunt_shard",
		"campaign_type":        "hunt",
		"upstream_target_id":   "jsmn",
		"harness_hash":         "abcdef0123456789",
		"iterations_per_shard": 2,
		"max_input_bytes":      256,
	}
	id := "research-delta-" + filepath.Base(dir)
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "hunt", Status: "running", BudgetRuns: 2, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, id, time.Now().Unix())
	var itemID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM fuzz_work_items WHERE campaign_id=? ORDER BY id LIMIT 1`, id).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	return svc, id, itemID
}
