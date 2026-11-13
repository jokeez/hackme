package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hackme/internal/poolfuzz"
	"hackme/internal/poolsync"
	"hackme/internal/store"
)

func TestIssuesLegacyOwnerSecretOnce(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "fuzz.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO fuzz_campaigns (id, campaign_type, status, title, owner_ref, budget_runs, config_json, created_at)
		VALUES ('legacy-1','hunt','completed','t','',4000,'{"pool_distributed":true}',1),
		       ('owned-1','hunt','completed','t','lab:other',4000,'{}',1)`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	out := filepath.Join(dir, "secret.txt")
	if err := run(ctx, dbPath, "legacy-1", "lab:founderb", out, false); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(out)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("secret file mode %v err %v", st.Mode().Perm(), err)
	}
	raw, _ := os.ReadFile(out)
	var secret string
	for _, l := range strings.Split(string(raw), "\n") {
		if v, ok := strings.CutPrefix(l, "owner_secret="); ok {
			secret = v
		}
	}
	db, _ = store.Open(dbPath)
	defer db.Close()
	hash, _ := (&poolfuzz.Service{DB: db}).CampaignOwnerSecretHash(ctx, "legacy-1")
	if secret == "" || !poolsync.OwnerSecretHashEqual(secret, hash) {
		t.Fatalf("stored hash does not match issued secret")
	}
	var owner string
	_ = db.QueryRow(`SELECT owner_ref FROM fuzz_campaigns WHERE id='legacy-1'`).Scan(&owner)
	if owner != "lab:founderb" {
		t.Fatalf("owner_ref %q", owner)
	}

	if err := run(ctx, dbPath, "legacy-1", "lab:founderb", filepath.Join(dir, "again.txt"), false); err == nil {
		t.Fatal("second mint must be refused")
	}
	if _, err := os.Stat(filepath.Join(dir, "again.txt")); !os.IsNotExist(err) {
		t.Fatal("refused mint must not leave a secret file")
	}
	if err := run(ctx, dbPath, "owned-1", "lab:founderb", filepath.Join(dir, "steal.txt"), false); err == nil {
		t.Fatal("campaign bound to another owner_ref must be refused")
	}
	if err := run(ctx, dbPath, "legacy-1", "lab:founderb", out, false); err == nil {
		t.Fatal("existing secret file must never be overwritten")
	}
}
