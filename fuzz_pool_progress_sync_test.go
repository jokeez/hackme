package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"hackme/internal/chain"
	"hackme/internal/store"
)

// tryClose must refresh coordinator progress before finalize without sync→close recursion.
func TestTryCloseRefreshesPoolProgressBeforeFinalize(t *testing.T) {
	coord := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/fuzz/pool/campaigns/progress":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true, "id": "pool-sync-camp", "status": "completed",
				"runs_done": 8, "budget_runs": 8,
			})
		case "/api/fuzz/pool/settle/outbox":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
		case "/api/fuzz/pool/settle/outbox/ack":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer coord.Close()

	t.Setenv("HACKME_POOL_COORDINATOR_URL", coord.URL)
	t.Setenv("HACKME_COORDINATOR_ADMIN_TOKEN", "coord-admin")

	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := chain.New(db)
	payer := "HMC-1111111111111111"
	if _, _, err := svc.InitGenesis(ctx, payer); err != nil {
		t.Fatal(err)
	}
	preFundMainEscrow(t, ctx, db, payer, 50)
	const camp = "pool-sync-camp"
	if _, err := db.ExecContext(ctx,
		`INSERT INTO fuzz_campaigns
		 (id, campaign_type, status, title, description, owner_ref, task_id, target_ref, budget_runs, budget_seconds, config_json, summary_json, report_token_hash, report_token_issued_at, created_at, started_at, completed_at)
		 VALUES (?, 'hunt', 'running', 't', '', '', '', '', 8, 3600, '{"pool_distributed":true}', '{}', '', 0, 1, 0, 0)`,
		camp); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.OpenHuntEscrow(ctx, camp, 20.0, 8); err != nil {
		t.Fatal(err)
	}
	a := &app{db: db, chain: svc}

	a.tryCloseFuzzEscrowForStatus(ctx, camp, "completed")

	var summaryJSON string
	if err := db.QueryRowContext(ctx, `SELECT summary_json FROM fuzz_campaigns WHERE id=?`, camp).Scan(&summaryJSON); err != nil {
		t.Fatal(err)
	}
	sum := parseMapJSON(summaryJSON)
	if intFromAny(sum["runs_done"]) != 8 {
		t.Fatalf("summary runs_done=%v want 8 from coordinator refresh", sum["runs_done"])
	}
}

// Progress mirror must advance local runs_done from coordinator even when settle outbox
// is empty/delayed (customer pulse must not sit at 0 while fleet works).
func TestPoolProgressMirrorWhileSettleLagging(t *testing.T) {
	coord := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/fuzz/pool/campaigns/progress":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true, "id": "pool-lag-camp", "status": "running",
				"runs_done": 42, "budget_runs": 128,
				"findings": 0, "findings_total": 3, "failed_checks": 1,
				"work_pending": 80, "work_leased": 6,
			})
		case "/api/fuzz/pool/settle/outbox":
			// Settle delayed / empty — progress must still mirror.
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`rate limited`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer coord.Close()

	t.Setenv("HACKME_POOL_COORDINATOR_URL", coord.URL)
	t.Setenv("HACKME_COORDINATOR_ADMIN_TOKEN", "coord-admin")

	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "lag.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	const camp = "pool-lag-camp"
	if _, err := db.ExecContext(ctx,
		`INSERT INTO fuzz_campaigns
		 (id, campaign_type, status, title, description, owner_ref, task_id, target_ref, budget_runs, budget_seconds, config_json, summary_json, report_token_hash, report_token_issued_at, created_at, started_at, completed_at)
		 VALUES (?, 'property', 'running', 'dig lag', '', 'owner-1', '', '', 128, 3600, '{"pool_distributed":true}', '{"runs_done":0}', '', 0, 1, 1, 0)`,
		camp); err != nil {
		t.Fatal(err)
	}
	a := &app{db: db}
	if err := a.syncPoolCampaignProgressFromCoordinator(ctx, camp); err != nil {
		t.Fatal(err)
	}
	var summaryJSON string
	if err := db.QueryRowContext(ctx, `SELECT summary_json FROM fuzz_campaigns WHERE id=?`, camp).Scan(&summaryJSON); err != nil {
		t.Fatal(err)
	}
	sum := parseMapJSON(summaryJSON)
	if intFromAny(sum["runs_done"]) != 42 {
		t.Fatalf("runs_done=%v want 42 pool-attributed mirror", sum["runs_done"])
	}
	if intFromAny(sum["pool_runs_done"]) != 42 {
		t.Fatalf("pool_runs_done=%v want 42", sum["pool_runs_done"])
	}
	if intFromAny(sum["pool_work_leased"]) != 6 {
		t.Fatalf("pool_work_leased=%v want 6", sum["pool_work_leased"])
	}
	if intFromAny(sum["pool_work_pending"]) != 80 {
		t.Fatalf("pool_work_pending=%v want 80", sum["pool_work_pending"])
	}
	// Crash-class honesty: findings_total noise must not become unique_crashes.
	if intFromAny(sum["unique_crashes"]) != 0 {
		t.Fatalf("unique_crashes=%v want 0 (crash-class only; coordinator findings=0)", sum["unique_crashes"])
	}
	if intFromAny(sum["findings_total"]) != 3 {
		t.Fatalf("findings_total=%v want 3 hint", sum["findings_total"])
	}
	if intFromAny(sum["heartbeat_at"]) <= 0 {
		t.Fatal("expected heartbeat_at stamped while fleet active")
	}
}

func TestPoolProgressMirrorStampsFleetActivityAtZeroRuns(t *testing.T) {
	coord := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/fuzz/pool/campaigns/progress" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "id": "pool-lease-camp", "status": "running",
			"runs_done": 0, "budget_runs": 64,
			"work_pending": 60, "work_leased": 4,
			"findings": 0, "findings_total": 0,
		})
	}))
	defer coord.Close()
	t.Setenv("HACKME_POOL_COORDINATOR_URL", coord.URL)
	t.Setenv("HACKME_COORDINATOR_ADMIN_TOKEN", "coord-admin")

	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "lease.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	const camp = "pool-lease-camp"
	if _, err := db.ExecContext(ctx,
		`INSERT INTO fuzz_campaigns
		 (id, campaign_type, status, title, description, owner_ref, task_id, target_ref, budget_runs, budget_seconds, config_json, summary_json, report_token_hash, report_token_issued_at, created_at, started_at, completed_at)
		 VALUES (?, 'hunt', 'running', 'hunt lease', '', '', '', '', 64, 3600, '{"pool_distributed":true}', '{}', '', 0, 1, 1, 0)`,
		camp); err != nil {
		t.Fatal(err)
	}
	a := &app{db: db}
	if err := a.syncPoolCampaignProgressFromCoordinator(ctx, camp); err != nil {
		t.Fatal(err)
	}
	var summaryJSON string
	if err := db.QueryRowContext(ctx, `SELECT summary_json FROM fuzz_campaigns WHERE id=?`, camp).Scan(&summaryJSON); err != nil {
		t.Fatal(err)
	}
	sum := parseMapJSON(summaryJSON)
	if intFromAny(sum["runs_done"]) != 0 {
		t.Fatalf("must not invent runs_done, got %v", sum["runs_done"])
	}
	if intFromAny(sum["pool_work_leased"]) != 4 || intFromAny(sum["pool_work_pending"]) != 60 {
		t.Fatalf("queue hints missing: %+v", sum)
	}
	if intFromAny(sum["heartbeat_at"]) <= 0 {
		t.Fatal("fleet leased/pending should stamp heartbeat so pulse is not stale")
	}
}
