package poolfuzz

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/fuzzreport"
	"hackme/internal/store"
)

func TestSealedFindingStoresNoPlaintext(t *testing.T) {
	t.Setenv("HACKME_REPORT_SEAL_PEPPER", "pool-seal-pepper")
	dir := t.TempDir()
	t.Setenv("HACKME_REPO_ROOT", dir)
	db, err := store.Open(filepath.Join(dir, "seal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	pub, priv, err := fuzzreport.NewKey()
	if err != nil {
		t.Fatal(err)
	}
	cfg := map[string]any{
		"campaign_type":        "hunt",
		"work_kind":            "hunt_shard",
		"hunt_source":          "catalog",
		"upstream_target_id":   "tinyexr",
		"report_seal_pub":      hex.EncodeToString(pub[:]),
		"native_repro_enabled": false,
		"seed_byte_corpus":     []any{"736563726574"},
	}
	if err := svc.RegisterCampaign(ctx, Campaign{ID: "seal-camp", CampaignType: "hunt", Status: "running", BudgetRuns: 1, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	secret := []byte("SECRET-BYTES-91")
	req := SubmitRequest{
		CampaignID:  "seal-camp",
		ItemID:      1,
		WorkerID:    "w",
		InputBytes:  secret,
		Trap:        "stack-buffer-overflow in parse_exr",
		CheckResult: 1,
	}
	id, _, _, err := svc.insertFinding(ctx, req, cfg, fuzzengine.SemanticsDetector, false, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	var title, sha, artifact, repro, detail string
	if err := db.QueryRowContext(ctx, `SELECT title, input_sha256, artifact_path, repro_cmd, detail_json FROM fuzz_findings WHERE id=?`, id).
		Scan(&title, &sha, &artifact, &repro, &detail); err != nil {
		t.Fatal(err)
	}
	if title != "sealed" || artifact != "" || repro != "" {
		t.Fatalf("plaintext columns title=%q artifact=%q repro=%q", title, artifact, repro)
	}
	rawSHA := fuzzengine.InputBytesSHA256(secret)
	if sha == rawSHA || sha == "" {
		t.Fatalf("stored fingerprint is the raw sha: %s", sha)
	}
	for _, leak := range []string{"SECRET-BYTES-91", "parse_exr", "tinyexr", rawSHA, "736563726574"} {
		if strings.Contains(detail, leak) {
			t.Fatalf("detail leaks %q: %s", leak, detail)
		}
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(detail), &env); err != nil {
		t.Fatal(err)
	}
	opened, err := fuzzreport.Open(pub, priv, env["box"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(opened), hex.EncodeToString(secret)) || !strings.Contains(string(opened), "parse_exr") {
		t.Fatalf("customer key did not recover the finding: %s", opened)
	}
	known, err := svc.crashFindingAlreadyKnown(ctx, "seal-camp", req, cfg)
	if err != nil || !known {
		t.Fatalf("sealed dedup known=%v err=%v", known, err)
	}
}
