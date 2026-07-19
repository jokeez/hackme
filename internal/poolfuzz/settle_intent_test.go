package poolfuzz

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/store"
)

type failThenOKSettler struct {
	mu        sync.Mutex
	runs      int
	findings  int
	failUntil int
}

func (s *failThenOKSettler) PayRun(context.Context, string, string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs++
	if s.runs <= s.failUntil {
		return errors.New("injected settle failure")
	}
	return nil
}
func (s *failThenOKSettler) PayFinding(context.Context, string, string, string) error {
	s.mu.Lock()
	s.findings++
	s.mu.Unlock()
	return nil
}
func (s *failThenOKSettler) Finalize(context.Context, string) error { return nil }

func TestSubmitSettleFailureThenRetryPaysOnce(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "settle.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	spy := &failThenOKSettler{failUntil: 1}
	svc := &Service{DB: db, Settler: spy}
	ctx := context.Background()
	cfg := fuzzengine.NormalizeCampaignConfig(map[string]any{
		"pool_distributed": true,
		"budget_hmc":       1.0,
		"check_semantics":  "pow_gate",
	}, "property")
	id := "settle-retry"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "property", Status: "running", BudgetRuns: 2, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureWorkItems(ctx, id, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	inN, actual := actualInputForWorkItem(t, ctx, svc, id, 1, cfg)
	sub := SubmitRequest{
		WorkerID: "w1", MinerAddress: "HMC-1234567890123456",
		WorkID: id + ":1", CampaignID: id, ItemID: 1, InputN: inN, ActualInput: actual, CheckResult: 1, DurationMS: 1,
	}
	if err := svc.Submit(ctx, sub); err == nil {
		t.Fatal("expected first submit to fail on PayRun")
	}
	var runSt string
	if err := db.QueryRowContext(ctx,
		`SELECT settle_run_status FROM fuzz_work_items WHERE campaign_id=? AND id=1`, id).Scan(&runSt); err != nil {
		t.Fatal(err)
	}
	if runSt != "pending" {
		t.Fatalf("after failed PayRun want settle_run_status=pending, got %q", runSt)
	}
	if err := svc.Submit(ctx, sub); err != nil {
		t.Fatalf("retry submit: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`SELECT settle_run_status FROM fuzz_work_items WHERE campaign_id=? AND id=1`, id).Scan(&runSt); err != nil {
		t.Fatal(err)
	}
	if runSt != "paid" {
		t.Fatalf("after retry want settle_run_status=paid, got %q", runSt)
	}
	if err := svc.Submit(ctx, sub); err != nil {
		t.Fatalf("idempotent third submit: %v", err)
	}
	spy.mu.Lock()
	runs := spy.runs
	spy.mu.Unlock()
	if runs != 2 {
		t.Fatalf("PayRun must run once fail + once success (exactly 2), got %d", runs)
	}
}
