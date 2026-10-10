package poolfuzz

// Report #34: campaign config harness_content_sha256 must not override the
// published artifact fingerprint (harness_hash bind / supply-chain lie).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/hunt"
	"hackme/internal/store"
)

func TestReport34ConfigContentSHACannotOverrideArtifact(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "r34.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := &Service{DB: db}
	ctx := context.Background()

	const hash = "cafebabecafebabecafebabecafebabecafebabecafebabecafebabecafebabe"
	bin := []byte("r34-real-harness-bytes")
	sum := sha256.Sum256(bin)
	realFP := hex.EncodeToString(sum[:])
	fakeFP := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	hunt.SetHarnessObjectDir("")
	t.Cleanup(func() { hunt.SetHarnessObjectDir("") })
	if err := hunt.PutHarnessArtifact(ctx, db, hash, bin, "jsmn"); err != nil {
		t.Fatal(err)
	}

	cfg := map[string]any{
		"pool_distributed":       true,
		"work_kind":              "hunt_shard",
		"campaign_type":          "hunt",
		"upstream_target_id":     "jsmn",
		"harness_hash":           hash,
		"harness_content_sha256": fakeFP,
		"iterations_per_shard":   2,
		"max_input_bytes":        256,
		"depth_tier":             "oss_cve",
		"check_semantics":        "native_crash",
	}
	id := "r34-camp"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "hunt", Status: "running", BudgetRuns: 1, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, id, time.Now().Unix())

	// Mismatch → campaign is skipped pre-lease (no forged attestation, no fleet error-starve).
	_, ok, err := svc.Claim(ctx, "w34", time.Now().Unix())
	if err != nil {
		t.Fatalf("mismatched campaign must be skipped not hard-error: %v", err)
	}
	if ok {
		t.Fatal("claim must not succeed when config content sha disagrees with artifact")
	}

	// Agreeing config still serves the DB fingerprint.
	cfg["harness_content_sha256"] = realFP
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id + "-ok", CampaignType: "hunt", Status: "running", BudgetRuns: 1, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, id+"-ok", time.Now().Unix())
	w, ok, err := svc.Claim(ctx, "w34b", time.Now().Unix())
	if err != nil || !ok {
		t.Fatalf("agreeing claim: ok=%v err=%v", ok, err)
	}
	if w.CampaignID != id+"-ok" {
		t.Fatalf("claimed campaign=%q want %s-ok", w.CampaignID, id)
	}
	if w.HarnessContentSHA256 != realFP {
		t.Fatalf("claim attestation=%q want DB fp %q", w.HarnessContentSHA256, realFP)
	}
}

func TestReport34ValidCorpusNamespace(t *testing.T) {
	if !ValidCorpusNamespace("guard_pack_msgpack") {
		t.Fatal("expected valid")
	}
	if ValidCorpusNamespace("../etc") || ValidCorpusNamespace("a/b") || ValidCorpusNamespace("") {
		t.Fatal("path-like namespace must be rejected")
	}
}
