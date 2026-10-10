package poolfuzz

// Report #33: after an empty-miner hunt replay claim is recorded, a later submit
// must not late-bind miner_address (payout hijack when hybrid is not strict).

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hackme/internal/hunt"
	"hackme/internal/store"
)

func TestReport33RefuseMinerLateBind(t *testing.T) {
	t.Setenv("HACKME_POOL_HUNT_REPLAY", "1")
	t.Setenv("HACKME_POOL_HUNT_REPLAY_ASYNC", "1")

	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "r33.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := &Service{DB: db}
	ctx := context.Background()

	cfg := map[string]any{
		"pool_distributed":     true,
		"work_kind":            "hunt_shard",
		"campaign_type":        "hunt",
		"upstream_target_id":   "jsmn",
		"harness_hash":         "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		"iterations_per_shard": 2,
		"max_input_bytes":      256,
		"depth_tier":           "oss_cve",
		"check_semantics":      "native_crash",
	}
	id := "r33-camp"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "hunt", Status: "running", BudgetRuns: 1, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	hunt.SetHarnessObjectDir("")
	t.Cleanup(func() { hunt.SetHarnessObjectDir("") })
	if err := hunt.PutHarnessArtifact(ctx, db, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef", []byte("r33-harness"), "jsmn"); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, id, time.Now().Unix())
	w, ok, err := svc.Claim(ctx, "victim-w33", time.Now().Unix())
	if err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}

	empty := SubmitRequest{
		WorkerID: "victim-w33", WorkID: w.WorkID, CampaignID: w.CampaignID,
		ItemID: w.ItemID, InputN: w.InputN, ActualInput: w.ActualInput, InputBytes: w.InputBytes,
		CheckResult: 0, Trap: "", DurationMS: 5, SegmentExecDone: 2,
		MinerAddress: "",
	}
	out, err := svc.SubmitWithOutcome(ctx, empty)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Async {
		t.Fatalf("want async pending, got %+v", out)
	}

	hijack := empty
	hijack.MinerAddress = "HMC-aabbccddeeff0011"
	_, err = svc.SubmitWithOutcome(ctx, hijack)
	if err == nil || !strings.Contains(err.Error(), "refuse miner_address bind") {
		t.Fatalf("late miner bind must be refused, got %v", err)
	}

	var miner string
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(miner_address,'') FROM fuzz_hunt_replay_queue WHERE campaign_id=? AND item_id=?`,
		id, w.ItemID).Scan(&miner); err != nil {
		t.Fatal(err)
	}
	if miner != "" {
		t.Fatalf("miner_address must stay empty after refused late bind, got %q", miner)
	}
}
