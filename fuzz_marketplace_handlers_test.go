package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMergeCoordinatorMarketplaceFallsBackToRemote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/fuzz/pool/campaigns/list") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"campaigns": []map[string]any{
				{
					"id": "campaign-remote-1", "status": "running", "title": "Remote Audit",
					"budget_runs": 32, "runs_done": 4, "budget_hmc": 2.0, "per_run_hmc": 0.0125,
				},
				{
					"id": "campaign-remote-done", "status": "completed", "title": "Done",
					"budget_runs": 8, "runs_done": 8,
				},
			},
		})
	}))
	defer srv.Close()

	a := &app{}
	t.Setenv("HACKME_POOL_COORDINATOR_URL", srv.URL)
	out := a.mergeCoordinatorPoolMarketplace(context.Background(), nil)
	if len(out) != 1 {
		t.Fatalf("want 1 running remote campaign, got %d: %#v", len(out), out)
	}
	if out[0]["id"] != "campaign-remote-1" {
		t.Fatalf("id=%v", out[0]["id"])
	}
}

func TestHandleFuzzMarketplaceLocalDBErrorStillOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"campaigns": []map[string]any{
				{"id": "campaign-c1", "status": "running", "title": "C1", "budget_runs": 16, "runs_done": 1},
			},
		})
	}))
	defer srv.Close()

	a, _ := newWalletTestApp(t)
	t.Setenv("HACKME_POOL_COORDINATOR_URL", srv.URL)
	// Drop/break fuzz_campaigns path by closing DB mid-test is hard; instead force empty
	// by renaming table via exec that makes ListPublicCampaigns fail.
	if _, err := a.db.Exec(`ALTER TABLE fuzz_campaigns RENAME TO fuzz_campaigns_broken_test`); err != nil {
		t.Skip("no fuzz_campaigns table:", err)
	}
	t.Cleanup(func() {
		_, _ = a.db.Exec(`ALTER TABLE fuzz_campaigns_broken_test RENAME TO fuzz_campaigns`)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/fuzz/marketplace", nil)
	rec := httptest.NewRecorder()
	a.handleFuzzMarketplace(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["ok"] != true {
		t.Fatalf("ok=%v", resp["ok"])
	}
	camps, _ := resp["campaigns"].([]any)
	if len(camps) < 1 {
		t.Fatalf("expected coordinator fallback campaigns, got %v", resp)
	}
}
