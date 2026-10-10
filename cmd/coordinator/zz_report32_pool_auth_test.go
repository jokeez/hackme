package main

// Report #32: worker token must not elevate to admin on /api/fuzz/pool/* mutating
// routes (campaigns, settle, harness upload, corpus namespace). Claim/harness GET OK.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"hackme/internal/poolfuzz"
	"hackme/internal/store"
)

func report32Mux(t *testing.T) *http.ServeMux {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "r32-auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	pf := &poolfuzz.Service{DB: db}
	wm := &workManager{
		hybridSignerEnabled: true,
		claimRequirePubKey:  true,
		claimPerMin:         60,
		submitPerMin:        60,
		abuse:               make(map[string]workerAbuseState),
		ipAbuse:             make(map[string]workerAbuseState),
		worker:              map[string]workerPayoutStat{},
		dropReasonCount:     make(map[string]uint64),
	}
	mux := http.NewServeMux()
	addFuzzPoolRoutes(mux, "admin-tok", "worker-tok", false, wm, pf)
	addCorpusNamespaceRoute(mux, "admin-tok", false, pf)
	return mux
}

func report32Req(t *testing.T, mux *http.ServeMux, method, path, token string, body any) int {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rdr = bytes.NewReader(raw)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("X-Hackme-Admin-Token", token)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec.Code
}

func TestReport32WorkerTokenDeniedOnAdminPoolRoutes(t *testing.T) {
	mux := report32Mux(t)
	worker := "worker-tok"

	cases := []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/api/fuzz/pool/campaigns", map[string]any{"id": "x", "config": map[string]any{}}},
		{http.MethodPost, "/api/fuzz/pool/campaigns/status", map[string]any{"id": "x", "status": "cancelled"}},
		{http.MethodPost, "/api/fuzz/pool/settle/outbox/ack", map[string]any{"ids": []int{1}}},
		{http.MethodPost, "/api/fuzz/pool/settle/replay", map[string]any{"campaign_id": "x"}},
		{http.MethodGet, "/api/fuzz/pool/settle/outbox", nil},
		{http.MethodPost, "/api/fuzz/pool/hunt/harness", map[string]any{
			"harness_hash": "aabbccdd", "binary_b64": base64.StdEncoding.EncodeToString([]byte("bin")),
		}},
		{http.MethodPost, "/api/fuzz/pool/corpus/namespace", map[string]any{
			"namespace": "../etc", "seeds": []map[string]any{},
		}},
	}
	for _, tc := range cases {
		code := report32Req(t, mux, tc.method, tc.path, worker, tc.body)
		if code != http.StatusUnauthorized {
			t.Fatalf("%s %s: worker token want 401, got %d", tc.method, tc.path, code)
		}
	}
}

func TestReport32AdminTokenAllowedCampaignCreate(t *testing.T) {
	mux := report32Mux(t)
	code := report32Req(t, mux, http.MethodPost, "/api/fuzz/pool/campaigns", "admin-tok", map[string]any{
		"id": "r32-camp", "campaign_type": "property", "status": "running", "budget_runs": 1,
		"config": map[string]any{"pool_distributed": true},
	})
	if code != http.StatusOK {
		t.Fatalf("admin create want 200, got %d", code)
	}
}

func TestReport32WorkerTokenMayClaim(t *testing.T) {
	mux := report32Mux(t)
	code := report32Req(t, mux, http.MethodPost, "/api/fuzz/work/claim", "worker-tok", map[string]any{
		"worker_id": "rig-1",
	})
	// 403 claim_pubkey_required or 429 no_fuzz_work — not 401.
	if code == http.StatusUnauthorized {
		t.Fatalf("worker claim must authenticate, got 401")
	}
}

func TestReport32CorpusNamespaceRejectsPathInjection(t *testing.T) {
	mux := report32Mux(t)
	code := report32Req(t, mux, http.MethodPost, "/api/fuzz/pool/corpus/namespace", "admin-tok", map[string]any{
		"namespace": "../etc/passwd",
		"seeds":     []map[string]any{{"input_u64": 1}},
	})
	if code != http.StatusBadRequest {
		t.Fatalf("path namespace want 400, got %d", code)
	}
}
