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

func researchDeltaHuntCamp(t *testing.T) (*Service, string, int64) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "research-delta.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := &Service{DB: db}
	ctx := context.Background()
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
