package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/store"
)

func TestObservePoolCorpusSkipsCullUnderMax(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "corpus.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	id := "cull-under"
	cfg := fuzzengine.NormalizeCampaignConfig(map[string]any{
		"pool_distributed":  true,
		"guided_scheduling": true,
		"pool_corpus_max":   256,
		"check_semantics":   "pow_gate",
	}, "fuzz")
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "fuzz", Status: "running", BudgetRuns: 10, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	before, _ := svc.poolCorpusSize(ctx, id)
	for i := 0; i < 5; i++ {
		if err := svc.observePoolCorpus(ctx, id, uint64(1000+i), []byte{byte(i)}, false, now); err != nil {
			t.Fatal(err)
		}
	}
	after, err := svc.poolCorpusSize(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if after < before+5 {
		t.Fatalf("under-max observe should keep seeds; before=%d after=%d", before, after)
	}
}

func TestCullPoolCorpusTrimsToMax(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "corpus2.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	id := "cull-trim"
	cfg := fuzzengine.NormalizeCampaignConfig(map[string]any{
		"pool_distributed": true,
		"check_semantics":  "pow_gate",
	}, "fuzz")
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "fuzz", Status: "running", BudgetRuns: 10, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	for i := 0; i < 40; i++ {
		if err := svc.upsertPoolCorpusSeed(ctx, id, uint64(2000+i), []byte{byte(i)}, 1, i, i, false, now); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.cullPoolCorpus(ctx, id, 16); err != nil {
		t.Fatal(err)
	}
	n, err := svc.poolCorpusSize(ctx, id)
	if err != nil || n > 16 {
		t.Fatalf("after cull size=%d err=%v", n, err)
	}
}

func TestCountCrashClassFindingsSQL(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "crash.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	id := "crash-count"
	cfg := fuzzengine.NormalizeCampaignConfig(map[string]any{"pool_distributed": true, "check_semantics": "pow_gate"}, "fuzz")
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "fuzz", Status: "running", BudgetRuns: 10, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	_, err = db.ExecContext(ctx,
		`INSERT INTO fuzz_findings (id, campaign_id, finding_type, severity, title, input_sha256, created_at)
		 VALUES ('f1',?, 'native_crash', 'high', 'a', 'aa', ?),
		        ('f2',?, 'detector_hit', 'low', 'b', 'bb', ?),
		        ('f3',?, 'heap_asan_oob', 'critical', 'c', 'cc', ?)`,
		id, now, id, now, id, now)
	if err != nil {
		t.Fatal(err)
	}
	n, err := svc.countCrashClassFindings(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("want 2 crash-class, got %d", n)
	}
}

func TestRelaySettlerSkipInlineHTTP(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "settle.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	cfg := fuzzengine.NormalizeCampaignConfig(map[string]any{
		"pool_distributed":   true,
		"orders_settle_pull": false,
		"check_semantics":    "pow_gate",
	}, "fuzz")
	if err := svc.RegisterCampaign(ctx, Campaign{ID: "skip-http", CampaignType: "fuzz", Status: "running", BudgetRuns: 10, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	relay := &RelaySettler{
		Service:          svc,
		DefaultOrdersURL: "http://127.0.0.1:1",
		AdminToken:       func() string { return "tok" },
		SkipInlineHTTP:   true,
	}
	res, err := relay.PayRun(ctx, "skip-http", "HMC-aaaaaaaaaaaaaaaa", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied || res.OutboxID <= 0 {
		t.Fatalf("enqueue-only want pending outbox, got %+v", res)
	}
	items, err := svc.ListPendingSettleOutbox(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("pending=%d", len(items))
	}
}
