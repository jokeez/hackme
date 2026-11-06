package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"

	"hackme/internal/store"
)

// A finding whose detail carries no miner is paid to the miner of its own work item — not to
// whoever reported the campaign's newest finding (audit: bounty to the wrong finder).
func TestReplayBountyGoesToTheFindingsOwnWorkItem(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "finder.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	svc := &Service{DB: db}
	id := "camp-finder"
	cfg := map[string]any{"pool_distributed": true, "work_kind": "hunt_shard", "escrow_split": "50_50"}
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "hunt", Status: "completed", BudgetRuns: 2, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	finder, other := "HMC-1111111111111111", "HMC-2222222222222222"
	now := int64(1_700_000_000)
	for _, it := range []struct {
		id    int64
		miner string
	}{{1, finder}, {2, other}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO fuzz_work_items
			 (campaign_id, id, input_n, status, attempts, result_ok, miner_address, settle_run_status, created_at, updated_at)
			 VALUES (?, ?, ?, 'done', 1, 1, ?, 'paid', ?, ?)`, id, it.id, it.id, it.miner, now, now); err != nil {
			t.Fatal(err)
		}
	}
	// First (bounty-winning) finding: no miner in detail, only its work item. Newer finding: other miner.
	for _, f := range []struct {
		id, detail string
		at         int64
	}{
		{"f-first", `{"item_id":1}`, now},
		{"f-newer", `{"item_id":2,"miner_address":"` + other + `"}`, now + 60},
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO fuzz_findings (id, campaign_id, finding_type, severity, detail_json, created_at)
			 VALUES (?, ?, 'crash', 'high', ?, ?)`, f.id, id, f.detail, f.at); err != nil {
			t.Fatal(err)
		}
	}
	if _, findings, _, err := svc.ReplayCampaignSettles(ctx, id); err != nil || findings != 1 {
		t.Fatalf("findings=%d err=%v", findings, err)
	}
	var got string
	if err := db.QueryRowContext(ctx, `SELECT miner_address FROM fuzz_settle_outbox WHERE campaign_id=? AND kind='finding'`, id).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != finder {
		t.Fatalf("bounty paid to %s, want the finder %s", got, finder)
	}
}
