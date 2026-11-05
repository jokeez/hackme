package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/store"
)

func TestCancelPendingOfFinishedCampaigns(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "co.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	now := time.Now().Unix()
	for _, c := range []struct{ id, st string }{{"done-camp", "completed"}, {"live-camp", "running"}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO fuzz_campaigns (id, campaign_type, status, title, config_json, summary_json, created_at, started_at, completed_at)
			VALUES (?, 'hunt', ?, ?, '{}', '{}', ?, ?, 0)`, c.id, c.st, c.id, now, now); err != nil {
			t.Fatal(err)
		}
		for i, st := range []string{"pending", "pending", "done", "leased"} {
			if _, err := db.ExecContext(ctx, `INSERT INTO fuzz_work_items (campaign_id, input_n, status, attempts, last_error, lease_owner, lease_until, result_ok, duration_ms, created_at, updated_at)
				VALUES (?, ?, ?, 0, '', '', 0, 1, 0, ?, ?)`, c.id, i, st, now, now); err != nil {
				t.Fatal(err)
			}
		}
	}
	n, err := svc.CancelPendingOfFinishedCampaigns(ctx, now)
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	count := func(id, st string) (k int) {
		_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_work_items WHERE campaign_id=? AND status=?`, id, st).Scan(&k)
		return
	}
	if count("done-camp", "pending") != 0 || count("done-camp", "done") != 1 || count("done-camp", "leased") != 1 {
		t.Fatal("finished campaign: only pending rows may be cancelled")
	}
	if count("live-camp", "pending") != 2 {
		t.Fatal("running campaign pending rows must stay")
	}
}

func TestCancelHuntCampaignsWithMismatchedAttestation(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "co.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	hash := "8e7170202510e75ed88db857ace95e748e7170202510e75ed88db857ace95e74"
	putTestHuntHarness(t, ctx, db, hash)
	old := time.Now().Unix() - 3600
	for id, fp := range map[string]string{
		"hunt-good": "",
		"hunt-bad":  "3a526a1eef4023ec32dfb662e08e0ae24aed126a563471f5483a2283036f23be",
	} {
		cfg := map[string]any{"pool_distributed": true, "work_kind": "hunt_shard", "campaign_type": "hunt", "harness_hash": hash}
		if fp != "" {
			cfg["harness_content_sha256"] = fp
		}
		if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "hunt", Title: id, Status: "running", BudgetRuns: 4, Config: cfg}); err != nil {
			t.Fatal(err)
		}
		_, _ = db.ExecContext(ctx, `UPDATE fuzz_campaigns SET created_at=? WHERE id=?`, old, id)
	}
	if _, err := svc.CancelHuntCampaignsMissingHarness(ctx, 900, 10); err != nil {
		t.Fatal(err)
	}
	st := func(id string) (s string) {
		_ = db.QueryRowContext(ctx, `SELECT status FROM fuzz_campaigns WHERE id=?`, id).Scan(&s)
		return
	}
	if st("hunt-bad") != "cancelled" {
		t.Fatalf("mismatched attestation must cancel, got %s", st("hunt-bad"))
	}
	if st("hunt-good") != "running" {
		t.Fatalf("valid harness must keep running, got %s", st("hunt-good"))
	}
}
