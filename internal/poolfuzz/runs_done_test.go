package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/store"
)

func TestListPublicCampaignsHidesClosedEscrowZombie(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "zomb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	cid := "campaign-zombie-closed"
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: cid, CampaignType: "property", Title: "audit still running label", Status: "running",
		BudgetRuns: 100, Config: map[string]any{"pool_distributed": true, "budget_hmc": 5.0},
	}); err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO fuzz_campaign_escrow
		 (campaign_id, budget_units, runs_pool_units, bounty_pool_units, runs_paid_units, bounty_paid_units,
		  runs_done, budget_runs, per_run_units, finding_winner, status, created_at)
		 VALUES (?, 500000000, 100000000, 400000000, 0, 0, 0, 100, 1000000, '', 'closed', ?)`,
		cid, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	items, err := svc.ListPublicCampaigns(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if toStringAny(it["id"]) == cid {
			t.Fatalf("closed-escrow zombie still listed: %+v", it)
		}
	}
}

func toStringAny(v any) string {
	s, _ := v.(string)
	return s
}

func TestRunsDoneForCampaignPrefersWorkItems(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "runs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	cid := "campaign-pool-runs-done"
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: cid, CampaignType: "property", Title: "audit", Status: "running",
		BudgetRuns: 8, Config: map[string]any{"pool_distributed": true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureWorkItems(ctx, cid, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		_, err = db.ExecContext(ctx,
			`UPDATE fuzz_work_items SET status='done', updated_at=? WHERE campaign_id=? AND input_n=?`,
			time.Now().Unix(), cid, i)
		if err != nil {
			t.Fatal(err)
		}
	}
	got := runsDoneForCampaign(ctx, db, cid, map[string]any{"runs_done": 0})
	if got != 3 {
		t.Fatalf("runs_done=%d want 3", got)
	}
	// Marketplace list prefers summary_json (hot-path); seed it then list.
	_, err = db.ExecContext(ctx,
		`UPDATE fuzz_campaigns SET summary_json=? WHERE id=?`,
		`{"runs_done":3}`, cid)
	if err != nil {
		t.Fatal(err)
	}
	items, err := svc.ListPublicCampaigns(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || intFromJSON(items[0]["runs_done"]) != 3 {
		t.Fatalf("marketplace runs_done=%v want 3", items[0])
	}
}

func TestCampaignProgressRunsOKExcludesFailed(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "prog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	cid := "campaign-progress-runs-ok"
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: cid, CampaignType: "hunt", Title: "hunt progress", Status: "running",
		BudgetRuns: 8, Config: map[string]any{"pool_distributed": true, "work_kind": "hunt_shard"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureWorkItems(ctx, cid, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	_, err = db.ExecContext(ctx,
		`UPDATE fuzz_work_items SET status='done', result_ok=1, updated_at=? WHERE campaign_id=? AND input_n IN (1,2)`,
		now, cid)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx,
		`UPDATE fuzz_work_items SET status='done', result_ok=0, updated_at=? WHERE campaign_id=? AND input_n=3`,
		now, cid)
	if err != nil {
		t.Fatal(err)
	}
	prog, err := svc.CampaignProgress(ctx, cid)
	if err != nil {
		t.Fatal(err)
	}
	if intFromJSON(prog["runs_done"]) != 3 {
		t.Fatalf("runs_done=%v want 3", prog["runs_done"])
	}
	if intFromJSON(prog["failed_checks"]) != 1 {
		t.Fatalf("failed_checks=%v want 1", prog["failed_checks"])
	}
	if intFromJSON(prog["runs_ok"]) != 2 {
		t.Fatalf("runs_ok=%v want 2", prog["runs_ok"])
	}
}

func TestCampaignProgressIncludesWorkQueueHints(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "prog-queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	cid := "campaign-progress-queue"
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: cid, CampaignType: "hunt", Title: "hunt queue", Status: "running",
		BudgetRuns: 8, Config: map[string]any{"pool_distributed": true, "work_kind": "hunt_shard"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureWorkItems(ctx, cid, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	_, err = db.ExecContext(ctx,
		`UPDATE fuzz_work_items SET status='done', result_ok=1, updated_at=? WHERE campaign_id=? AND input_n=1`,
		now, cid)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx,
		`UPDATE fuzz_work_items SET status='leased', lease_owner='w1', lease_until=?, updated_at=? WHERE campaign_id=? AND input_n IN (2,3)`,
		now+60, now, cid)
	if err != nil {
		t.Fatal(err)
	}
	prog, err := svc.CampaignProgress(ctx, cid)
	if err != nil {
		t.Fatal(err)
	}
	if intFromJSON(prog["runs_done"]) != 1 {
		t.Fatalf("runs_done=%v want 1", prog["runs_done"])
	}
	if intFromJSON(prog["work_leased"]) != 2 {
		t.Fatalf("work_leased=%v want 2", prog["work_leased"])
	}
	if intFromJSON(prog["work_pending"]) < 1 {
		t.Fatalf("work_pending=%v want >=1", prog["work_pending"])
	}
}

func TestCampaignProgressFindingsAreCrashClassOnly(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "prog-findings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	cid := "campaign-progress-findings-class"
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: cid, CampaignType: "property", Title: "dig progress", Status: "running",
		BudgetRuns: 4, Config: map[string]any{"pool_distributed": true},
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	for _, row := range []struct {
		id, ft string
	}{
		{"f-noise-1", "property_violation"},
		{"f-noise-2", "security_violation"},
		{"f-crash-1", "native_crash"},
	} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO fuzz_findings (id, campaign_id, finding_type, severity, title, input_sha256, created_at)
			 VALUES (?,?,?,'high',?,?,?)`,
			row.id, cid, row.ft, row.id, row.id, now); err != nil {
			t.Fatal(err)
		}
	}
	prog, err := svc.CampaignProgress(ctx, cid)
	if err != nil {
		t.Fatal(err)
	}
	if intFromJSON(prog["findings"]) != 1 {
		t.Fatalf("findings=%v want 1 crash-class", prog["findings"])
	}
	if intFromJSON(prog["findings_total"]) != 3 {
		t.Fatalf("findings_total=%v want 3", prog["findings_total"])
	}
}
