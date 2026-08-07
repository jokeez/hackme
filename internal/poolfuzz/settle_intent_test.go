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

func (s *failThenOKSettler) PayRun(_ context.Context, _, _ string, _, _ int64) (SettleResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs++
	if s.runs <= s.failUntil {
		return SettleResult{}, errors.New("injected settle failure")
	}
	return SettleResult{OutboxID: int64(s.runs), Applied: true}, nil
}
func (s *failThenOKSettler) PayFinding(context.Context, string, string, string, int64, int64) (SettleResult, error) {
	s.mu.Lock()
	s.findings++
	s.mu.Unlock()
	return SettleResult{Applied: true}, nil
}
func (s *failThenOKSettler) PayCrashBonus(context.Context, string, string, int64, int64) (SettleResult, error) {
	return SettleResult{Applied: true}, nil
}
func (s *failThenOKSettler) Finalize(context.Context, string, int64) (SettleResult, error) {
	return SettleResult{Applied: true}, nil
}

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
	leaseWorkItemForTest(t, ctx, db, id, 1, "w1")
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

func TestSubmitSettleQueuedNotPaidUntilApplied(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "settle-queued.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	queued := &queuedSettler{}
	svc := &Service{DB: db, Settler: queued}
	ctx := context.Background()
	cfg := fuzzengine.NormalizeCampaignConfig(map[string]any{
		"pool_distributed": true,
		"budget_hmc":       1.0,
		"check_semantics":  "pow_gate",
	}, "property")
	id := "settle-queued"
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
	leaseWorkItemForTest(t, ctx, db, id, 1, "w1")
	if err := svc.Submit(ctx, sub); err != nil {
		t.Fatalf("submit: %v", err)
	}
	var runSt string
	var outboxID int64
	if err := db.QueryRowContext(ctx,
		`SELECT settle_run_status, settle_run_outbox_id FROM fuzz_work_items WHERE campaign_id=? AND id=1`, id).Scan(&runSt, &outboxID); err != nil {
		t.Fatal(err)
	}
	if runSt != "queued" || outboxID <= 0 {
		t.Fatalf("want queued with outbox id, got status=%q outbox=%d", runSt, outboxID)
	}
	if err := svc.Submit(ctx, sub); err != nil {
		t.Fatalf("retry: %v", err)
	}
	var outbox2 int64
	if err := db.QueryRowContext(ctx,
		`SELECT settle_run_outbox_id FROM fuzz_work_items WHERE campaign_id=? AND id=1`, id).Scan(&outbox2); err != nil {
		t.Fatal(err)
	}
	if outbox2 != outboxID {
		t.Fatalf("reuse outbox: first=%d second=%d", outboxID, outbox2)
	}
	if queued.runs != 2 {
		t.Fatalf("PayRun calls=%d want 2", queued.runs)
	}
	if queued.lastReuse != outboxID {
		t.Fatalf("second call reuseOutboxID=%d want %d", queued.lastReuse, outboxID)
	}
}

type queuedSettler struct {
	runs      int
	lastReuse int64
	outboxSeq int64
}

func (s *queuedSettler) PayRun(_ context.Context, _, _ string, _, reuse int64) (SettleResult, error) {
	s.runs++
	s.lastReuse = reuse
	id := reuse
	if id <= 0 {
		s.outboxSeq++
		id = s.outboxSeq
	}
	return SettleResult{OutboxID: id, Applied: false}, nil
}
func (s *queuedSettler) PayFinding(context.Context, string, string, string, int64, int64) (SettleResult, error) {
	return SettleResult{Applied: false}, nil
}
func (s *queuedSettler) PayCrashBonus(context.Context, string, string, int64, int64) (SettleResult, error) {
	return SettleResult{Applied: false}, nil
}
func (s *queuedSettler) Finalize(context.Context, string, int64) (SettleResult, error) {
	return SettleResult{Applied: true}, nil
}

func TestEnqueueSettleOutboxIdempotentReplay(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "settle.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	id1, err := svc.EnqueueSettleOutbox(ctx, "run", "camp-a", "HMC-abc", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	id2, err := svc.EnqueueSettleOutbox(ctx, "run", "camp-a", "HMC-abc", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Fatalf("replay should reuse id: %d vs %d", id1, id2)
	}
	idItem2, err := svc.EnqueueSettleOutbox(ctx, "run", "camp-a", "HMC-abc", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if idItem2 == id1 {
		t.Fatal("distinct work_item_id must not collapse outbox rows (FUZZ-01 underpay)")
	}
	id3, err := svc.EnqueueSettleOutbox(ctx, "finding", "camp-a", "HMC-abc", "high", 0)
	if err != nil {
		t.Fatal(err)
	}
	if id3 == id1 || id3 == idItem2 {
		t.Fatal("different kind/severity should create new row")
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_settle_outbox`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("want 3 outbox rows got %d", n)
	}
}

func TestEnqueueSettleOutboxConcurrentUnique(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "settle-race.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	const n = 32
	ids := make([]int64, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			id, err := svc.EnqueueSettleOutbox(ctx, "run", "race-camp", "HMC-race0000000001", "", 7)
			if err != nil {
				t.Errorf("enqueue: %v", err)
				return
			}
			ids[i] = id
		}(i)
	}
	wg.Wait()
	first := ids[0]
	for _, id := range ids {
		if id != first {
			t.Fatalf("concurrent enqueue diverged: %v", ids)
		}
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_settle_outbox WHERE campaign_id='race-camp'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("want 1 outbox row after race, got %d", count)
	}
}
