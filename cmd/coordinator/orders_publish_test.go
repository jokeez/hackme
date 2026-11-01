package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"hackme/internal/poolfuzz"
	"hackme/internal/store"
)

func ordersPublishMux(t *testing.T) *http.ServeMux {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "pub.db"))
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
	addFuzzPoolRoutes(mux, "admin-tok", "worker-tok", "publish-tok", false, wm, pf)
	return mux
}

func publishReq(t *testing.T, mux *http.ServeMux, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/campaigns", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Hackme-Admin-Token", token)
	}
	req.RemoteAddr = "203.0.113.10:4444"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func publishReqBearer(t *testing.T, mux *http.ServeMux, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/campaigns", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.RemoteAddr = "203.0.113.11:4444"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestOrdersPublishTokenCanRegister(t *testing.T) {
	mux := ordersPublishMux(t)
	rec := publishReq(t, mux, "publish-tok", map[string]any{
		"id": "camp-audit-1", "campaign_type": "property", "status": "running",
		"budget_runs": 64, "budget_seconds": 900,
		"config": map[string]any{"dig_package": "scan", "guard_pack": "filter_utf8"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("publish create want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOrdersPublishBearerAuthWorks(t *testing.T) {
	mux := ordersPublishMux(t)
	rec := publishReqBearer(t, mux, "publish-tok", map[string]any{
		"id": "camp-bearer-1", "budget_runs": 8, "budget_seconds": 60,
		"config": map[string]any{"dig_package": "scan", "guard_pack": "secrets"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("bearer publish want 200, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestOrdersPublishTokenDeniedOnAdminRoutes(t *testing.T) {
	mux := ordersPublishMux(t)
	cases := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodPost, "/api/fuzz/pool/campaigns/status", map[string]any{"id": "x", "status": "cancelled"}},
		{http.MethodPost, "/api/fuzz/pool/settle/replay", map[string]any{"campaign_id": "x"}},
		{http.MethodPost, "/api/fuzz/pool/campaigns/cleanup-stale", map[string]any{}},
		{http.MethodPost, "/api/fuzz/pool/campaigns/cleanup-gates", map[string]any{}},
		{http.MethodPost, "/api/fuzz/pool/campaigns/repair-zombies", map[string]any{}},
	}
	for _, tc := range cases {
		raw, _ := json.Marshal(tc.body)
		req := httptest.NewRequest(tc.method, tc.path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hackme-Admin-Token", "publish-tok")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s: publish must be 401, got %d", tc.method, tc.path, rec.Code)
		}
	}
}

func TestOrdersPublishTokenMayUploadHarness(t *testing.T) {
	mux := ordersPublishMux(t)
	payload := []byte("hunt-customer-harness-v1")
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	b64Body, _ := json.Marshal(map[string]any{
		"harness_hash": hash,
		"source_rel":   "customer/harness",
		"binary_b64":   base64.StdEncoding.EncodeToString(payload),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/hunt/harness", bytes.NewReader(b64Body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", "publish-tok")
	req.RemoteAddr = "203.0.113.50:9"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("publish harness JSON want 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	hash2 := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	bin := []byte("octet-harness-blob")
	req2 := httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/hunt/harness", bytes.NewReader(bin))
	req2.Header.Set("Content-Type", "application/octet-stream")
	req2.Header.Set("X-Hackme-Harness-Hash", hash2)
	req2.Header.Set("X-Hackme-Admin-Token", "publish-tok")
	req2.RemoteAddr = "203.0.113.51:9"
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("publish harness octet want 200, got %d body=%s", rec2.Code, rec2.Body.String())
	}

	req3 := httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/hunt/harness", bytes.NewReader(bin))
	req3.Header.Set("Content-Type", "application/octet-stream")
	req3.Header.Set("X-Hackme-Harness-Hash", hash2)
	req3.Header.Set("X-Hackme-Admin-Token", "worker-tok")
	rec3 := httptest.NewRecorder()
	mux.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusUnauthorized {
		t.Fatalf("worker harness upload want 401, got %d", rec3.Code)
	}
}

func TestOrdersPublishTokenMaySettleOutbox(t *testing.T) {
	mux := ordersPublishMux(t)
	// Global outbox forbidden for publish.
	req := httptest.NewRequest(http.MethodGet, "/api/fuzz/pool/settle/outbox?limit=8", nil)
	req.Header.Set("X-Hackme-Admin-Token", "publish-tok")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("publish global outbox want 400, got %d %s", rec.Code, rec.Body.String())
	}
	// Register orders_publish campaign then scoped GET/ACK.
	rec = publishReq(t, mux, "publish-tok", map[string]any{
		"id": "camp-settle-1", "budget_runs": 8, "budget_seconds": 60,
		"config": map[string]any{"dig_package": "scan", "guard_pack": "secrets"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("register want 200, got %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/fuzz/pool/settle/outbox?campaign_id=camp-settle-1&limit=8", nil)
	req.Header.Set("X-Hackme-Admin-Token", "publish-tok")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("publish scoped outbox GET want 200, got %d %s", rec.Code, rec.Body.String())
	}
	raw, _ := json.Marshal(map[string]any{"ids": []int64{}, "campaign_id": "camp-settle-1", "confirm_applied": true})
	req = httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/settle/outbox/ack", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", "publish-tok")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("publish scoped ACK want 200, got %d %s", rec.Code, rec.Body.String())
	}
	// ACK without confirm_applied rejected.
	raw, _ = json.Marshal(map[string]any{"ids": []int64{}, "campaign_id": "camp-settle-1"})
	req = httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/settle/outbox/ack", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", "publish-tok")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("ACK without confirm_applied want 400, got %d", rec.Code)
	}
	// ACK without campaign_id rejected.
	raw, _ = json.Marshal(map[string]any{"ids": []int64{1}, "confirm_applied": true})
	req = httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/settle/outbox/ack", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", "publish-tok")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("publish ACK without campaign_id want 400, got %d", rec.Code)
	}
	// Replay remains admin-only.
	raw, _ = json.Marshal(map[string]any{"campaign_id": "x"})
	req = httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/settle/replay", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", "publish-tok")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("publish must not settle/replay: got %d", rec.Code)
	}
}

func TestOrdersPublishCannotHijackAdminCampaign(t *testing.T) {
	mux := ordersPublishMux(t)
	rec := publishReq(t, mux, "admin-tok", map[string]any{
		"id": "admin-owned", "budget_runs": 8,
		"config": map[string]any{"pool_distributed": true},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("admin create want 200, got %d", rec.Code)
	}
	rec = publishReq(t, mux, "publish-tok", map[string]any{
		"id": "admin-owned", "budget_runs": 8,
		"config": map[string]any{"dig_package": "scan", "guard_pack": "secrets"},
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("hijack want 409, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestOrdersPublishRejectsOversizedBudget(t *testing.T) {
	mux := ordersPublishMux(t)
	rec := publishReq(t, mux, "publish-tok", map[string]any{
		"id": "camp-huge", "budget_runs": 999999, "budget_seconds": 900,
		"config": map[string]any{"dig_package": "deep"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 oversized, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestOrdersPublishRejectsMissingPackage(t *testing.T) {
	mux := ordersPublishMux(t)
	rec := publishReq(t, mux, "publish-tok", map[string]any{
		"id": "camp-nopkg", "budget_runs": 8,
		"config": map[string]any{"pool_distributed": true},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 missing package, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestOrdersPublishWorkerStillDenied(t *testing.T) {
	mux := ordersPublishMux(t)
	rec := publishReq(t, mux, "worker-tok", map[string]any{
		"id": "camp-w", "budget_runs": 8,
		"config": map[string]any{"dig_package": "scan"},
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("worker want 401, got %d", rec.Code)
	}
}

func TestOrdersPublishRateLimit(t *testing.T) {
	t.Setenv("HACKME_ORDERS_PUBLISH_MAX_PER_HOUR", "2")
	t.Setenv("HACKME_ORDERS_PUBLISH_MAX_PER_DAY", "2")
	globalPublishRate = &publishRateLimiter{hour: map[string][]time.Time{}, day: map[string][]time.Time{}}
	mux := ordersPublishMux(t)
	body := map[string]any{
		"budget_runs": 8, "budget_seconds": 60,
		"config": map[string]any{"dig_package": "scan", "guard_pack": "secrets"},
	}
	for i := 0; i < 2; i++ {
		body["id"] = "rate-" + strconv.Itoa(i)
		rec := publishReq(t, mux, "publish-tok", body)
		if rec.Code != http.StatusOK {
			t.Fatalf("iter %d want 200 got %d %s", i, rec.Code, rec.Body.String())
		}
	}
	body["id"] = "rate-overflow"
	rec := publishReq(t, mux, "publish-tok", body)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestOrdersPublishRejectsCancelViaUpsert(t *testing.T) {
	mux := ordersPublishMux(t)
	rec := publishReq(t, mux, "publish-tok", map[string]any{
		"id": "camp-cancel-bypass", "status": "cancelled", "budget_runs": 8,
		"config": map[string]any{"dig_package": "scan", "guard_pack": "secrets"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("cancel via upsert must 400, got %d %s", rec.Code, rec.Body.String())
	}
	rec = publishReq(t, mux, "publish-tok", map[string]any{
		"id": "camp-complete-bypass", "status": "completed", "budget_runs": 8,
		"config": map[string]any{"dig_package": "scan", "guard_pack": "secrets"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("completed via upsert must 400, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestOrdersPublishStripsPoHAttach(t *testing.T) {
	mux := ordersPublishMux(t)
	rec := publishReq(t, mux, "publish-tok", map[string]any{
		"id": "camp-poh-strip", "budget_runs": 8, "budget_seconds": 60,
		"config": map[string]any{
			"dig_package": "scan", "guard_pack": "secrets",
			"attach_poh_order": true, "create_poh_order": true,
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 after strip, got %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	attach, _ := resp["attach_poh_order"].(map[string]any)
	if attach == nil {
		t.Fatal("missing attach_poh_order in response")
	}
	reason, _ := attach["reason"].(string)
	if reason != "orders_publish_no_poh_attach" {
		t.Fatalf("PoH attach must be blocked for publish, got %#v", attach)
	}
}

func TestOrdersPublishRejectsPathID(t *testing.T) {
	mux := ordersPublishMux(t)
	for _, id := range []string{"../etc", "camp/../x", "camp with space", "camp\nnewline", "кириллица"} {
		rec := publishReq(t, mux, "publish-tok", map[string]any{
			"id": id, "budget_runs": 8,
			"config": map[string]any{"dig_package": "scan"},
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("id %q want 400, got %d", id, rec.Code)
		}
	}
}

func TestOrdersPublishRejectsPrefixToken(t *testing.T) {
	mux := ordersPublishMux(t)
	// Longer/shorter secrets must not match (constant-time length gate).
	for _, tok := range []string{"publish-tok-extra", "publish-to", "PUBLISH-TOK"} {
		rec := publishReq(t, mux, tok, map[string]any{
			"id": "camp-prefix", "budget_runs": 8,
			"config": map[string]any{"dig_package": "scan"},
		})
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("token %q want 401, got %d", tok, rec.Code)
		}
	}
	// Leading/trailing spaces are trimmed by extractCoordAdminSecret (same as admin).
	rec := publishReq(t, mux, " publish-tok ", map[string]any{
		"id": "camp-trim", "budget_runs": 8,
		"config": map[string]any{"dig_package": "scan", "guard_pack": "secrets"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("trimmed publish token want 200, got %d", rec.Code)
	}
}

func TestOrdersPublishAdminBypassesCaps(t *testing.T) {
	mux := ordersPublishMux(t)
	rec := publishReq(t, mux, "admin-tok", map[string]any{
		"id": "camp-admin-huge", "budget_runs": 999999, "budget_seconds": 999999,
		"config": map[string]any{"pool_distributed": true},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("admin must bypass publish caps, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestOrdersPublishNoTokenDenied(t *testing.T) {
	mux := ordersPublishMux(t)
	rec := publishReq(t, mux, "", map[string]any{
		"id": "camp-anon", "budget_runs": 8,
		"config": map[string]any{"dig_package": "scan"},
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon want 401, got %d", rec.Code)
	}
}

func TestValidatePublishCampaignID(t *testing.T) {
	if err := validatePublishCampaignID("ok-id_1"); err != nil {
		t.Fatal(err)
	}
	if err := validatePublishCampaignID("../etc"); err == nil {
		t.Fatal("expected invalid id")
	}
	if err := validatePublishCampaignID("a..b"); err == nil {
		t.Fatal("expected .. rejected")
	}
	if publishStatusAllowed("cancelled") {
		t.Fatal("cancelled must be denied")
	}
	if !publishStatusAllowed("running") {
		t.Fatal("running ok")
	}
}

func TestStripPublishEscalationConfig(t *testing.T) {
	cfg := map[string]any{
		"dig_package": "scan", "attach_poh_order": true, "create_poh_order": 1,
		"admin_token": "x", "force_settle": true,
	}
	stripPublishEscalationConfig(cfg)
	for _, k := range []string{"attach_poh_order", "create_poh_order", "admin_token", "force_settle"} {
		if _, ok := cfg[k]; ok {
			t.Fatalf("%s not stripped", k)
		}
	}
	if cfg["dig_package"] != "scan" {
		t.Fatal("dig_package must remain")
	}
}

func TestOrdersPublishMethodNotAllowed(t *testing.T) {
	mux := ordersPublishMux(t)
	req := httptest.NewRequest(http.MethodGet, "/api/fuzz/pool/campaigns", nil)
	req.Header.Set("X-Hackme-Admin-Token", "publish-tok")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET want 405, got %d", rec.Code)
	}
}

func TestOrdersPublishWorkerHeaderDoesNotElevate(t *testing.T) {
	mux := ordersPublishMux(t)
	raw, _ := json.Marshal(map[string]any{
		"id": "camp-wheader", "budget_runs": 8,
		"config": map[string]any{"dig_package": "scan"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/campaigns", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Worker-Token", "publish-tok")
	req.RemoteAddr = "203.0.113.12:1"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	// extractCoordAdminSecret accepts Worker-Token header as secret carrier — publish value still matches publishToken.
	if rec.Code != http.StatusOK {
		t.Fatalf("publish via worker-header carrier should still auth as publish: got %d %s", rec.Code, rec.Body.String())
	}
	// Worker token value itself must not register.
	req = httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/campaigns", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Worker-Token", "worker-tok")
	req.RemoteAddr = "203.0.113.12:1"
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("worker value want 401, got %d", rec.Code)
	}
}

func TestOrdersPublishOwnerRefTooLong(t *testing.T) {
	mux := ordersPublishMux(t)
	rec := publishReq(t, mux, "publish-tok", map[string]any{
		"id": "camp-owner", "budget_runs": 8, "owner_ref": strings.Repeat("o", 300),
		"config": map[string]any{"dig_package": "scan"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 owner_ref, got %d", rec.Code)
	}
}
