package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/store"
)

// TestCorpusPersistCrossCampaignE2E verifies namespace export from campaign A
// is imported when campaign B registers with the same guard_pack namespace.
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
		"corpus_persist":    true,
		"input_mode":        "u64",
		"seed_corpus":       []any{uint64(10_000_000)},
	}
	ns := fuzzengine.CorpusPersistNamespace(cfg)
	if ns != "pack:bounds_smoke" {
		t.Fatalf("namespace=%q", ns)
	}

	const exportedInput = uint64(0x0123456789ABCDEF0)
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: "persist-a", CampaignType: "property", Title: "export", Status: "running",
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
