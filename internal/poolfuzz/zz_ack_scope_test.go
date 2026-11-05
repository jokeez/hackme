package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/store"
)

// Scoped (orders_publish) ACK for campaign A must not touch campaign B's work items.
func TestAudit7ScopedAckPromotesForeignWorkItems(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "a7ack.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	cfgA := map[string]any{"pool_distributed": true, "orders_publish": true, "budget_hmc": 10.0}
	cfgB := map[string]any{"pool_distributed": true, "budget_hmc": 10.0}
	if err := svc.RegisterCampaign(ctx, Campaign{ID: "campA", Status: "running", BudgetRuns: 8, Config: cfgA}); err != nil {
		t.Fatal(err)
	}
	if err := svc.RegisterCampaign(ctx, Campaign{ID: "campB", Status: "running", BudgetRuns: 8, Config: cfgB}); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, "campB", time.Now().Unix())
	var itemB int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM fuzz_work_items WHERE campaign_id='campB' ORDER BY id LIMIT 1`).Scan(&itemB); err != nil {
		t.Fatal(err)
	}
	obB, err := svc.EnqueueSettleOutbox(ctx, "run", "campB", "HMC-2222222222222222", "", itemB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE fuzz_work_items SET status='done', settle_run_status='queued', settle_run_outbox_id=? WHERE id=?`, obB, itemB); err != nil {
		t.Fatal(err)
	}
	// Holder of campA's scoped publish token ACKs campB's outbox id.
	n, err := svc.AckSettleOutboxForOrdersPublish(ctx, "campA", []int64{obB})
	if err != nil {
		t.Fatal(err)
	}
	var st, ost string
	_ = db.QueryRowContext(ctx, `SELECT settle_run_status FROM fuzz_work_items WHERE id=?`, itemB).Scan(&st)
	_ = db.QueryRowContext(ctx, `SELECT status FROM fuzz_settle_outbox WHERE id=?`, obB).Scan(&ost)
	t.Logf("acked=%d campB item settle_run_status=%q outbox=%q", n, st, ost)
	if st == "paid" && ost == "pending" {
		t.Fatalf("foreign campaign work item promoted to paid while its outbox row is still pending")
	}
}
