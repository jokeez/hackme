package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/store"
)

// Same-tenant campaigns with the same owner_ref+pack may share corpus.
func TestCorpusPersistCrossCampaignE2E(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "corpus-persist.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	svc := &Service{DB: db}
	ctx := context.Background()
	now := time.Now().Unix()

	cfg := map[string]any{
		"guided_scheduling": true,
		"guard_pack":        "bounds_smoke",
		"owner_ref":         "customer:same-tenant",
		"corpus_persist":    true,
		"input_mode":        "u64",
		"seed_corpus":       []any{uint64(10_000_000)},
	}
	ns := fuzzengine.CorpusPersistNamespace(cfg)
	wantNS := "owner:customer:same-tenant:pack:bounds_smoke"
	if ns != wantNS {
		t.Fatalf("namespace=%q want %q", ns, wantNS)
	}

	const exportedInput = uint64(0x0123456789ABCDEF0)
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: "persist-a", CampaignType: "property", Title: "export", Status: "running",
		OwnerRef:   "customer:same-tenant",
		BudgetRuns: 8, BudgetSeconds: 120, Config: cfg,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.observePoolCorpus(ctx, "persist-a", exportedInput, nil, false, now); err != nil {
		t.Fatal(err)
	}

	var nsCount int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM fuzz_corpus_namespace WHERE namespace=? AND input_u64=?`,
		ns, int64(exportedInput)).Scan(&nsCount); err != nil {
		t.Fatal(err)
	}
	if nsCount != 1 {
		t.Fatalf("expected namespace row for exported input, got count=%d", nsCount)
	}

	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: "persist-b", CampaignType: "property", Title: "import", Status: "running",
		OwnerRef:   "customer:same-tenant",
		BudgetRuns: 8, BudgetSeconds: 120, Config: cfg,
	}); err != nil {
		t.Fatal(err)
	}
	seeds, err := svc.loadPoolCorpusSeeds(ctx, "persist-b", 64)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range seeds {
		if s.Input == exportedInput {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("campaign B corpus missing exported seed %#x from namespace %q (seeds=%d)", exportedInput, ns, len(seeds))
	}
}
