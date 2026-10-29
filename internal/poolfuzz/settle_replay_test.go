package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/store"
)

func TestReplayCampaignSettlesUsesRunFloorForPendingFloor(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "replay-floor.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	svc := &Service{DB: db}
	id := "replay-floor-camp"
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: id, CampaignType: "hunt", Status: "completed", BudgetRuns: 2,
		Config: map[string]any{"budget_hmc": 15, "escrow_split": "50_50", "pool_distributed": true},
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	miner := "HMC-1234567890123456"
	for _, row := range []struct {
		id int64
		st string
	}{{1, "pending_floor"}, {2, "paid"}} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO fuzz_work_items(campaign_id, id, input_n, status, miner_address, settle_run_status, created_at, updated_at)
			 VALUES(?,?,?,?,?,?,?,?)`,
			id, row.id, row.id, "done", miner, row.st, now, now); err != nil {
			t.Fatal(err)
		}
	}

	runs, findings, fin, err := svc.ReplayCampaignSettles(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if runs != 1 {
		t.Fatalf("runs_enqueued=%d want 1 (skip paid)", runs)
	}
	if findings != 0 || fin != 1 {
		t.Fatalf("findings=%d finalize=%d", findings, fin)
	}
	var kind string
	if err := db.QueryRowContext(ctx,
		`SELECT kind FROM fuzz_settle_outbox WHERE campaign_id=? AND work_item_id=1`, id).Scan(&kind); err != nil {
		t.Fatal(err)
	}
	if kind != "run_floor" {
		t.Fatalf("outbox kind=%q want run_floor", kind)
	}
	var nRun int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM fuzz_settle_outbox WHERE campaign_id=? AND kind='run'`, id).Scan(&nRun); err != nil {
		t.Fatal(err)
	}
	if nRun != 0 {
		t.Fatalf("unexpected full-run outbox rows=%d", nRun)
	}
}
