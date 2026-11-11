package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/store"
)

func TestTryFinalizeRevalidatesPayLandedBeforeClose(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "fin-race.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	spy := &spySettler{}
	svc := &Service{DB: db, Settler: spy}
	ctx := context.Background()
	const id = "camp-fin-race"
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: id, CampaignType: "property", Status: "running", BudgetRuns: 1,
		Config: map[string]any{"pool_distributed": true, "budget_hmc": 1.0},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE fuzz_work_items SET status='done', result_ok=1 WHERE campaign_id=?`, id); err != nil {
		t.Fatal(err)
	}
	svc.testInjectBeforeFinalize = func(ctx context.Context, campaignID string) {
		if _, err := svc.EnqueueSettleOutbox(ctx, "run", campaignID, "HMC-1234567890123456", "", 7); err != nil {
			t.Errorf("inject pay: %v", err)
		}
	}
	if err := svc.tryFinalizeEscrow(ctx, id); err != nil {
		t.Fatal(err)
	}
	if spy.finalize != 0 {
		t.Fatalf("Finalize called %d times after a pay landed in the decision window", spy.finalize)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_settle_outbox WHERE campaign_id=? AND kind='finalize'`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("finalize outbox rows=%d", n)
	}
	var pays int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_settle_outbox WHERE campaign_id=? AND kind='run' AND status='pending'`, id).Scan(&pays); err != nil {
		t.Fatal(err)
	}
	if pays != 1 {
		t.Fatalf("injected pay rows=%d", pays)
	}
}

func TestCampaignSettleLockSeesPayBeforeFinalize(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "fin-lock.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	spy := &spySettler{}
	svc := &Service{DB: db, Settler: spy}
	ctx := context.Background()
	const id = "camp-fin-lock"
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: id, CampaignType: "property", Status: "running", BudgetRuns: 1,
		Config: map[string]any{"pool_distributed": true, "budget_hmc": 1.0},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE fuzz_work_items SET status='done', result_ok=1 WHERE campaign_id=?`, id); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	started := make(chan struct{})
	held := make(chan error, 1)
	go func() {
		held <- svc.withCampaignSettle(id, func() error {
			close(started)
			<-release
			_, err := svc.EnqueueSettleOutbox(ctx, "run", id, "HMC-1234567890123456", "", 9)
			return err
		})
	}()
	<-started
	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.tryFinalizeEscrow(ctx, id)
	}()
	select {
	case err := <-errCh:
		t.Fatalf("finalize returned while the pay section held the lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if spy.finalize != 0 {
		t.Fatal("finalized while the pay section held the lock")
	}
	close(release)
	if err := <-held; err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if spy.finalize != 0 {
		t.Fatalf("Finalize called %d times after the pay row was enqueued", spy.finalize)
	}
}
