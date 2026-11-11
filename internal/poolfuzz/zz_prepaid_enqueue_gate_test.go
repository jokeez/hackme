package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/store"
)

// Med/High: Tick/EnsureWorkItems must not enqueue unpaid orders_publish campaigns.
func TestEnsureWorkItemsSkipsOrdersPublishWithoutPrepaid(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "prepaid-enq.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}

	unpaid := map[string]any{"pool_distributed": true, "orders_publish": true, "budget_hmc": 10.0}
	if err := svc.RegisterCampaign(ctx, Campaign{ID: "camp-unpaid", Status: "running", BudgetRuns: 8, Config: unpaid}); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureWorkItems(ctx, "camp-unpaid", time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_work_items WHERE campaign_id='camp-unpaid'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("unpaid orders_publish must not enqueue, got %d work items", n)
	}

	paid := map[string]any{"pool_distributed": true, "orders_publish": true, "prepaid_attested": true, "budget_hmc": 10.0}
	if err := svc.RegisterCampaign(ctx, Campaign{ID: "camp-paid", Status: "running", BudgetRuns: 8, Config: paid}); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureWorkItems(ctx, "camp-paid", time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_work_items WHERE campaign_id='camp-paid'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("prepaid_attested orders_publish must enqueue work")
	}
}
