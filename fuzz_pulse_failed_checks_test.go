package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFuzzPulseFailedChecksNotCrashCount(t *testing.T) {
	a, _ := newWalletTestApp(t)
	t.Setenv("HACKME_ADMIN_TOKEN", "pulse-admin")
	const camp = "pulse-failed-checks"
	now := time.Now().Unix()
	// unique_crashes is crash-class. failed_checks is detector/work-item rejects.
	// Pulse must not copy the crash count into coverage.failed_checks.
	summary := `{"runs_done":3,"unique_crashes":4,"failed_checks":2}`
	_, err := a.db.ExecContext(context.Background(),
		`INSERT INTO fuzz_campaigns
		 (id, campaign_type, status, title, description, owner_ref, task_id, target_ref, budget_runs, budget_seconds, config_json, summary_json, report_token_hash, report_token_issued_at, created_at, started_at, completed_at)
		 VALUES (?, 'property', 'running', 'pulse hygiene', '', 'owner-1', '', '', 64, 3600, '{}', ?, '', 0, ?, ?, 0)`,
		camp, summary, now, now)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/fuzz/campaigns/"+camp+"/pulse", nil)
	req.Header.Set("X-Hackme-Admin-Token", "pulse-admin")
	rec := httptest.NewRecorder()
	a.handleFuzzCampaigns(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	cov, _ := resp["coverage"].(map[string]any)
	if cov == nil {
		t.Fatalf("missing coverage: %s", rec.Body.String())
	}
	if got := intFromAny(cov["failed_checks"]); got != 2 {
		t.Fatalf("coverage.failed_checks=%d want 2 (detector rejects, not unique_crashes=4)", got)
	}
	if got := intFromAny(cov["unique_crashes"]); got != 0 {
		t.Fatalf("coverage.unique_crashes=%d want 0 with no crash-class finding rows", got)
	}
}
