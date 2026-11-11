package poolfuzz

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"hackme/internal/store"
)

// High: concurrent first-publish must not hijack owner_ref or wipe owner secret hash.
func TestRegisterCampaignConcurrentOwnerPreserved(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "reg-race.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i, owner := range []string{"customer:alice", "customer:bob"} {
		wg.Add(1)
		go func(i int, owner string) {
			defer wg.Done()
			cfg := map[string]any{
				"pool_distributed": true, "orders_publish": true, "prepaid_attested": true,
				"orders_owner_secret_hash": "hash-" + owner,
				"budget_hmc":               10.0,
			}
			errs <- svc.RegisterCampaign(ctx, Campaign{
				ID: "camp-race", Status: "running", BudgetRuns: 8, OwnerRef: owner, Config: cfg,
			})
		}(i, owner)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	owner, err := svc.CampaignOwnerRef(ctx, "camp-race")
	if err != nil {
		t.Fatal(err)
	}
	if owner != "customer:alice" && owner != "customer:bob" {
		t.Fatalf("unexpected owner %q", owner)
	}
	hash, err := svc.CampaignOwnerSecretHash(ctx, "camp-race")
	if err != nil {
		t.Fatal(err)
	}
	wantHash := "hash-" + owner
	if hash != wantHash {
		t.Fatalf("secret hash=%q want first-writer %q (owner=%q)", hash, wantHash, owner)
	}
	// Second writer's hash must not have replaced first writer's.
	other := "hash-customer:alice"
	if owner == "customer:alice" {
		other = "hash-customer:bob"
	}
	if hash == other {
		t.Fatalf("racing UPSERT replaced first-writer secret hash")
	}
}
