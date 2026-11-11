package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"hackme/internal/chain"
	"hackme/internal/poolfuzz"
	"hackme/internal/poolsync"
	"hackme/internal/store"
)

func ordersPublishMux(t *testing.T) *http.ServeMux {
	t.Helper()
	// Fresh limiter per test: publishRateAllow is process-global and shared IP
	// 203.0.113.10 hits HACKME_ORDERS_PUBLISH_MAX_PER_* under -shuffle=on.
	globalPublishRate = &publishRateLimiter{hour: map[string][]time.Time{}, day: map[string][]time.Time{}}
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

func publishBodyMap(body any) map[string]any {
	switch v := body.(type) {
	case map[string]any:
		return v
	default:
		raw, _ := json.Marshal(body)
		out := map[string]any{}
		_ = json.Unmarshal(raw, &out)
		return out
	}
}

func stampPublishPrepaid(token string, body map[string]any) {
	token = strings.TrimSpace(token)
	if token == "" || token == "admin-tok" || token == "worker-tok" {
		return
	}
	owner, _ := body["owner_ref"].(string)
	owner = strings.TrimSpace(owner)
	if owner == "" {
		owner = "customer:test"
		body["owner_ref"] = owner
	}
	id, _ := body["id"].(string)
	budget := 0.0
	switch v := body["budget_hmc"].(type) {
	case float64:
		budget = v
	case int:
		budget = float64(v)
	}
	if budget <= 0 {
		budget = 1.0
		if cfg, ok := body["config"].(map[string]any); ok {
			if hp, _ := cfg["hunt_package"].(string); strings.Contains(strings.ToLower(hp), "heavy") {
				budget = 150
			} else if hp != "" {
				budget = 20
			} else if dp, _ := cfg["dig_package"].(string); strings.EqualFold(dp, "deep") || strings.EqualFold(dp, "enterprise") {
				budget = 25
			}
		}
		body["budget_hmc"] = budget
	}
	runs := 0
	switch v := body["budget_runs"].(type) {
	case float64:
		runs = int(v)
	case int:
		runs = v
	}
	// Hub verifies against the real publish token ("publish-tok" in tests).
	macTok := token
	if strings.HasPrefix(token, "publish-tok") || token == "publish-tok" {
		macTok = "publish-tok"
	}
	// Always re-sign: callers often mutate id on a shared map (rate-limit loops).
	if hub := poolsync.OrdersPrepaidHubSecret(); hub != "" || poolsync.RequireHubPrepaidTicket() {
		fp := poolsync.EscrowPrepaidFingerprintParts(id, budget, runs, "open")
		body["escrow_fingerprint"] = fp
		body["prepaid_attestation"] = poolsync.SignHubPrepaidTicket(hub, owner, id, budget, runs, fp, time.Now().Unix()+3600)
		return
	}
	body["prepaid_attestation"] = poolsync.SignPrepaidAttestation(macTok, owner, id, budget, runs)
}

func publishReq(t *testing.T, mux *http.ServeMux, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	m := publishBodyMap(body)
	stampPublishPrepaid(token, m)
	raw, _ := json.Marshal(m)
	req := httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/campaigns", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Hackme-Admin-Token", token)
	}
	if owner, _ := m["owner_ref"].(string); strings.TrimSpace(owner) != "" {
		req.Header.Set("X-Hackme-Orders-Owner-Ref", strings.TrimSpace(owner))
	}
	req.RemoteAddr = "203.0.113.10:4444"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func publishReqBearer(t *testing.T, mux *http.ServeMux, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	m := publishBodyMap(body)
	stampPublishPrepaid(token, m)
	raw, _ := json.Marshal(m)
	req := httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/campaigns", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	if owner, _ := m["owner_ref"].(string); strings.TrimSpace(owner) != "" {
		req.Header.Set("X-Hackme-Orders-Owner-Ref", strings.TrimSpace(owner))
	}
	req.RemoteAddr = "203.0.113.11:4444"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func publishOwnerSecretFromResp(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	sec, _ := resp["orders_owner_secret"].(string)
	return strings.TrimSpace(sec)
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
	sum := sha256.Sum256(payload)
	fp := hex.EncodeToString(sum[:])
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	b64Body, _ := json.Marshal(map[string]any{
		"harness_hash":           hash,
		"source_rel":             "customer/harness",
		"binary_b64":             base64.StdEncoding.EncodeToString(payload),
		"harness_content_sha256": fp,
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

	// HUNT-01: publish without content attestation refused.
	missing, _ := json.Marshal(map[string]any{
		"harness_hash": "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		"binary_b64":   base64.StdEncoding.EncodeToString([]byte("no-attest")),
	})
	reqMiss := httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/hunt/harness", bytes.NewReader(missing))
	reqMiss.Header.Set("Content-Type", "application/json")
	reqMiss.Header.Set("X-Hackme-Admin-Token", "publish-tok")
	reqMiss.RemoteAddr = "203.0.113.50:9"
	recMiss := httptest.NewRecorder()
	mux.ServeHTTP(recMiss, reqMiss)
	if recMiss.Code != http.StatusBadRequest || !strings.Contains(recMiss.Body.String(), "harness_content_sha256") {
		t.Fatalf("publish without content sha want 400, got %d %s", recMiss.Code, recMiss.Body.String())
	}

	hash2 := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	bin := []byte("octet-harness-blob")
	sum2 := sha256.Sum256(bin)
	fp2 := hex.EncodeToString(sum2[:])
	req2 := httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/hunt/harness", bytes.NewReader(bin))
	req2.Header.Set("Content-Type", "application/octet-stream")
	req2.Header.Set("X-Hackme-Harness-Hash", hash2)
	req2.Header.Set("X-Hackme-Harness-Content-SHA256", fp2)
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
		"id": "camp-settle-1", "budget_runs": 8, "budget_seconds": 60, "owner_ref": "customer:settle",
		"config": map[string]any{"dig_package": "scan", "guard_pack": "secrets"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("register want 200, got %d %s", rec.Code, rec.Body.String())
	}
	ownerSecret := publishOwnerSecretFromResp(t, rec)
	if ownerSecret == "" {
		t.Fatal("expected orders_owner_secret on register")
	}
	req = httptest.NewRequest(http.MethodGet, "/api/fuzz/pool/settle/outbox?campaign_id=camp-settle-1&limit=8", nil)
	req.Header.Set("X-Hackme-Admin-Token", "publish-tok")
	req.Header.Set("X-Hackme-Orders-Owner-Ref", "customer:settle")
	req.Header.Set("X-Hackme-Orders-Owner-Secret", ownerSecret)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("publish scoped outbox GET want 200, got %d %s", rec.Code, rec.Body.String())
	}
	// Empty receipts ACK (no pending ids) OK.
	raw, _ := json.Marshal(map[string]any{"ids": []int64{}, "campaign_id": "camp-settle-1", "receipts": []any{}})
	req = httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/settle/outbox/ack", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", "publish-tok")
	req.Header.Set("X-Hackme-Orders-Owner-Ref", "customer:settle")
	req.Header.Set("X-Hackme-Orders-Owner-Secret", ownerSecret)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	// Empty receipts with empty ids → missing receipts error (never trust bare bool).
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("ACK without receipts want 400, got %d %s", rec.Code, rec.Body.String())
	}
	// Honor-system confirm_applied alone rejected.
	raw, _ = json.Marshal(map[string]any{"ids": []int64{1}, "campaign_id": "camp-settle-1", "confirm_applied": true})
	req = httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/settle/outbox/ack", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", "publish-tok")
	req.Header.Set("X-Hackme-Orders-Owner-Ref", "customer:settle")
	req.Header.Set("X-Hackme-Orders-Owner-Secret", ownerSecret)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("ACK with confirm_applied alone want 400, got %d", rec.Code)
	}
	// ACK without campaign_id rejected.
	raw, _ = json.Marshal(map[string]any{"ids": []int64{1}, "receipts": []any{map[string]any{"outbox_id": 1}}})
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

func TestOrdersPublishCannotOverwritePeerOrdersCampaign(t *testing.T) {
	mux := ordersPublishMux(t)
	rec := publishReq(t, mux, "publish-tok", map[string]any{
		"id": "camp-peer", "budget_runs": 8, "owner_ref": "customer:alice",
		"config": map[string]any{"dig_package": "scan", "guard_pack": "secrets"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("alice create want 200, got %d %s", rec.Code, rec.Body.String())
	}
	aliceSecret := publishOwnerSecretFromResp(t, rec)
	// Bob tries to retarget the same id with a hostile dig_package.
	rec = publishReq(t, mux, "publish-tok", map[string]any{
		"id": "camp-peer", "budget_runs": 64, "owner_ref": "customer:bob", "budget_hmc": 25.0,
		"config": map[string]any{"dig_package": "deep", "guard_pack": "secrets"},
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("bob overwrite want 409, got %d %s", rec.Code, rec.Body.String())
	}
	// Alice idempotent re-POST keeps original budget (no upsert).
	// Body may claim dig deep + higher runs; prepaid floor must still match the claim SKU.
	m := map[string]any{
		"id": "camp-peer", "budget_runs": 64, "owner_ref": "customer:alice", "budget_hmc": 25.0,
		"config": map[string]any{"dig_package": "deep", "guard_pack": "secrets"},
	}
	stampPublishPrepaid("publish-tok", m)
	raw, _ := json.Marshal(m)
	req := httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/campaigns", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", "publish-tok")
	req.Header.Set("X-Hackme-Orders-Owner-Ref", "customer:alice")
	req.Header.Set("X-Hackme-Orders-Owner-Secret", aliceSecret)
	req.RemoteAddr = "203.0.113.10:4444"
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("alice idempotent want 200, got %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/fuzz/pool/campaigns/progress?id=camp-peer", nil)
	req.Header.Set("X-Hackme-Admin-Token", "publish-tok")
	req.Header.Set("X-Hackme-Orders-Owner-Ref", "customer:alice")
	req.Header.Set("X-Hackme-Orders-Owner-Secret", aliceSecret)
	req.RemoteAddr = "203.0.113.10:4444"
	prog := httptest.NewRecorder()
	mux.ServeHTTP(prog, req)
	if prog.Code != http.StatusOK {
		t.Fatalf("progress want 200, got %d %s", prog.Code, prog.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(prog.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if int(body["budget_runs"].(float64)) != 8 {
		t.Fatalf("budget_runs mutated after idempotent re-POST: %v", body["budget_runs"])
	}
}

func TestOrdersPublishOwnerRefGatesProgress(t *testing.T) {
	mux := ordersPublishMux(t)
	rec := publishReq(t, mux, "publish-tok", map[string]any{
		"id": "camp-owned", "budget_runs": 8, "owner_ref": "customer:acme",
		"config": map[string]any{"dig_package": "scan", "guard_pack": "secrets"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create want 200, got %d", rec.Code)
	}
	secret := publishOwnerSecretFromResp(t, rec)
	req := httptest.NewRequest(http.MethodGet, "/api/fuzz/pool/campaigns/progress?id=camp-owned", nil)
	req.Header.Set("X-Hackme-Admin-Token", "publish-tok")
	req.Header.Set("X-Hackme-Orders-Owner-Ref", "customer:evil")
	req.Header.Set("X-Hackme-Orders-Owner-Secret", secret)
	req.RemoteAddr = "203.0.113.10:4444"
	prog := httptest.NewRecorder()
	mux.ServeHTTP(prog, req)
	if prog.Code != http.StatusForbidden {
		t.Fatalf("wrong owner want 403, got %d %s", prog.Code, prog.Body.String())
	}
	req.Header.Set("X-Hackme-Orders-Owner-Ref", "customer:acme")
	prog = httptest.NewRecorder()
	mux.ServeHTTP(prog, req)
	if prog.Code != http.StatusOK {
		t.Fatalf("matching owner want 200, got %d %s", prog.Code, prog.Body.String())
	}
	req.Header.Set("X-Hackme-Orders-Owner-Secret", "wrong-secret")
	prog = httptest.NewRecorder()
	mux.ServeHTTP(prog, req)
	if prog.Code != http.StatusForbidden {
		t.Fatalf("wrong secret want 403, got %d", prog.Code)
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

func TestOrdersPublishAcceptsHuntHeavyBudget(t *testing.T) {
	t.Setenv("HACKME_ORDERS_PUBLISH_MAX_BUDGET_RUNS", "") // default 16384
	mux := ordersPublishMux(t)
	rec := publishReq(t, mux, "publish-tok", map[string]any{
		"id": "camp-heavy", "budget_runs": 12000, "budget_seconds": 86400,
		"config": map[string]any{"hunt_package": "hunt_heavy", "pool_distributed": true},
	})
	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("hunt_heavy 12000 must fit default publish cap, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestOrdersPublishProgressOwnerAfterHeavyRegister(t *testing.T) {
	t.Setenv("HACKME_ORDERS_PUBLISH_MAX_BUDGET_RUNS", "")
	mux := ordersPublishMux(t)
	id := "hunt-20261102t001542"
	rec := publishReq(t, mux, "publish-tok", map[string]any{
		"id": id, "budget_runs": 12000, "budget_seconds": 86400, "owner_ref": "customer:heavy",
		"config": map[string]any{"hunt_package": "hunt_heavy", "pool_distributed": true},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("register want 200, got %d %s", rec.Code, rec.Body.String())
	}
	secret := publishOwnerSecretFromResp(t, rec)
	req := httptest.NewRequest(http.MethodGet, "/api/fuzz/pool/campaigns/progress?id="+id, nil)
	req.Header.Set("X-Hackme-Admin-Token", "publish-tok")
	req.Header.Set("X-Hackme-Orders-Owner-Ref", "customer:heavy")
	req.Header.Set("X-Hackme-Orders-Owner-Secret", secret)
	req.RemoteAddr = "203.0.113.10:4444"
	prog := httptest.NewRecorder()
	mux.ServeHTTP(prog, req)
	if prog.Code != http.StatusOK {
		t.Fatalf("owner progress want 200, got %d %s", prog.Code, prog.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(prog.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["id"] != id {
		t.Fatalf("progress id=%v", body["id"])
	}
	if int(body["budget_runs"].(float64)) != 12000 {
		t.Fatalf("budget_runs=%v", body["budget_runs"])
	}
}

func TestOrdersPublishRejectsHuntHeavyWhenCapLow(t *testing.T) {
	t.Setenv("HACKME_ORDERS_PUBLISH_MAX_BUDGET_RUNS", "2048")
	mux := ordersPublishMux(t)
	rec := publishReq(t, mux, "publish-tok", map[string]any{
		"id": "camp-heavy-blocked", "budget_runs": 12000, "budget_seconds": 86400,
		"config": map[string]any{"hunt_package": "hunt_heavy", "pool_distributed": true},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 when cap=2048, got %d %s", rec.Code, rec.Body.String())
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
		"admin_token": "x", "force_settle": true, "budget_hmc": 999.0, "prepaid_attested": true,
	}
	stripPublishEscalationConfig(cfg)
	for _, k := range []string{"attach_poh_order", "create_poh_order", "admin_token", "force_settle", "budget_hmc", "prepaid_attested"} {
		if _, ok := cfg[k]; ok {
			t.Fatalf("%s not stripped", k)
		}
	}
	if cfg["dig_package"] != "scan" {
		t.Fatal("dig_package must remain")
	}
}

func TestOrdersPublishRejectsEmptyOwnerRef(t *testing.T) {
	mux := ordersPublishMux(t)
	raw, _ := json.Marshal(map[string]any{
		"id": "camp-no-owner", "budget_runs": 8, "budget_hmc": 1.0,
		"prepaid_attestation": poolsync.SignPrepaidAttestation("publish-tok", "customer:x", "camp-no-owner", 1.0, 8),
		"config":              map[string]any{"dig_package": "scan", "guard_pack": "secrets"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/campaigns", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", "publish-tok")
	// Header required; without owner_ref body+header mismatch path.
	req.RemoteAddr = "203.0.113.10:4444"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 missing owner header, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestOrdersPublishHubEscrowGate(t *testing.T) {
	t.Setenv("HACKME_CHAIN_MINTING_HUB", "1")
	t.Setenv("HACKME_ORDERS_PREPAID_HUB_SECRET", "test-hub-prepaid-secret")
	t.Setenv("HACKME_ORDERS_PREPAID_ALLOW_ALGEBRAIC_MINT", "")
	t.Setenv("HACKME_COORDINATOR_ORDERS_URL", "")

	escrowOK := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/escrow") {
			http.NotFound(w, r)
			return
		}
		camp := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/fuzz/campaigns/"), "/escrow")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"escrow": chain.FuzzEscrowRow{
				CampaignID: camp, BudgetHMC: 2.0, BudgetRuns: 16, Status: "open",
			},
		})
	}))
	t.Cleanup(escrowOK.Close)

	globalPublishRate = &publishRateLimiter{hour: map[string][]time.Time{}, day: map[string][]time.Time{}}
	db, err := store.Open(filepath.Join(t.TempDir(), "escrow-gate.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	wm := &workManager{
		ordersProbeURL:      escrowOK.URL,
		hybridSignerEnabled: true, claimRequirePubKey: true, claimPerMin: 60, submitPerMin: 60,
		abuse: make(map[string]workerAbuseState), ipAbuse: make(map[string]workerAbuseState),
		worker: map[string]workerPayoutStat{}, dropReasonCount: make(map[string]uint64),
	}
	mux := http.NewServeMux()
	addFuzzPoolRoutes(mux, "admin-tok", "worker-tok", "publish-tok", false, wm, &poolfuzz.Service{DB: db})

	// Mint without matching escrow coverage → 409.
	mintBody, _ := json.Marshal(map[string]any{
		"campaign_id": "camp-tiny", "owner_ref": "customer:hub",
		"budget_hmc": 99.0, "budget_runs": 16,
	})
	mreq := httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/prepaid-ticket", bytes.NewReader(mintBody))
	mreq.Header.Set("Content-Type", "application/json")
	mreq.Header.Set("X-Hackme-Admin-Token", "admin-tok")
	mrec := httptest.NewRecorder()
	mux.ServeHTTP(mrec, mreq)
	if mrec.Code != http.StatusConflict {
		t.Fatalf("mint over budget want 409, got %d %s", mrec.Code, mrec.Body.String())
	}

	// Mint covered by live escrow → 200.
	mintBody2, _ := json.Marshal(map[string]any{
		"campaign_id": "camp-ok", "owner_ref": "customer:hub",
		"budget_hmc": 2.0, "budget_runs": 16,
	})
	mreq2 := httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/prepaid-ticket", bytes.NewReader(mintBody2))
	mreq2.Header.Set("Content-Type", "application/json")
	mreq2.Header.Set("X-Hackme-Admin-Token", "admin-tok")
	mrec2 := httptest.NewRecorder()
	mux.ServeHTTP(mrec2, mreq2)
	if mrec2.Code != http.StatusOK {
		t.Fatalf("mint with escrow: %d %s", mrec2.Code, mrec2.Body.String())
	}
	var mintResp map[string]any
	_ = json.Unmarshal(mrec2.Body.Bytes(), &mintResp)
	ticket, _ := mintResp["prepaid_attestation"].(string)
	fp, _ := mintResp["escrow_fingerprint"].(string)
	if ticket == "" || fp == "" {
		t.Fatalf("mint response missing ticket/fp: %#v", mintResp)
	}

	rec := publishReq(t, mux, "publish-tok", map[string]any{
		"id": "camp-ok", "budget_runs": 16, "owner_ref": "customer:hub", "budget_hmc": 2.0,
		"prepaid_attestation": ticket, "escrow_fingerprint": fp,
		"config": map[string]any{"dig_package": "scan", "guard_pack": "secrets"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("publish with live escrow ticket: %d %s", rec.Code, rec.Body.String())
	}
}

func TestOrdersPublishMintingHubRefusesSelfMAC(t *testing.T) {
	t.Setenv("HACKME_CHAIN_MINTING_HUB", "1")
	t.Setenv("HACKME_ORDERS_PREPAID_HUB_SECRET", "test-hub-prepaid-secret")
	// Unit mux has no linked hub escrow RPC — use documented algebraic break-glass.
	t.Setenv("HACKME_ORDERS_PREPAID_ALLOW_ALGEBRAIC_MINT", "1")
	mux := ordersPublishMux(t)

	// Publish-token self-MAC must be refused on minting hub (even with matching escrow FP).
	self := poolsync.SignPrepaidAttestation("publish-tok", "customer:hub", "camp-hub-self", 1.0, 8)
	fpSelf := poolsync.EscrowPrepaidFingerprintParts("camp-hub-self", 1.0, 8, "open")
	raw, _ := json.Marshal(map[string]any{
		"id": "camp-hub-self", "budget_runs": 8, "owner_ref": "customer:hub", "budget_hmc": 1.0,
		"prepaid_attestation": self, "escrow_fingerprint": fpSelf,
		"config": map[string]any{"dig_package": "scan", "guard_pack": "secrets"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/campaigns", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", "publish-tok")
	req.Header.Set("X-Hackme-Orders-Owner-Ref", "customer:hub")
	req.RemoteAddr = "203.0.113.55:4444"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "hub-signed") {
		t.Fatalf("want 400 hub-signed refuse, got %d %s", rec.Code, rec.Body.String())
	}

	// Matching hub ticket + escrow fingerprint must succeed.
	rec2 := publishReq(t, mux, "publish-tok", map[string]any{
		"id": "camp-hub-ok", "budget_runs": 8, "owner_ref": "customer:hub", "budget_hmc": 1.0,
		"config": map[string]any{"dig_package": "scan", "guard_pack": "secrets"},
	})
	if rec2.Code != http.StatusOK {
		t.Fatalf("hub ticket publish: %d %s", rec2.Code, rec2.Body.String())
	}

	// Admin ticket mint endpoint.
	fp := poolsync.EscrowPrepaidFingerprintParts("camp-mint", 2.0, 16, "open")
	mintBody, _ := json.Marshal(map[string]any{
		"campaign_id": "camp-mint", "owner_ref": "customer:hub",
		"budget_hmc": 2.0, "budget_runs": 16, "escrow_fingerprint": fp,
	})
	mreq := httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/prepaid-ticket", bytes.NewReader(mintBody))
	mreq.Header.Set("Content-Type", "application/json")
	mreq.Header.Set("X-Hackme-Admin-Token", "admin-tok")
	mrec := httptest.NewRecorder()
	mux.ServeHTTP(mrec, mreq)
	if mrec.Code != http.StatusOK {
		t.Fatalf("admin mint: %d %s", mrec.Code, mrec.Body.String())
	}
	// Publish token cannot mint tickets.
	mreq2 := httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/prepaid-ticket", bytes.NewReader(mintBody))
	mreq2.Header.Set("Content-Type", "application/json")
	mreq2.Header.Set("X-Hackme-Admin-Token", "publish-tok")
	mrec2 := httptest.NewRecorder()
	mux.ServeHTTP(mrec2, mreq2)
	if mrec2.Code != http.StatusUnauthorized {
		t.Fatalf("publish must not mint tickets, got %d", mrec2.Code)
	}
}

func TestOrdersPublishRejectsMissingBudgetHMC(t *testing.T) {
	mux := ordersPublishMux(t)
	raw, _ := json.Marshal(map[string]any{
		"id": "camp-no-budget", "budget_runs": 8, "owner_ref": "customer:x",
		"config": map[string]any{"dig_package": "scan", "guard_pack": "secrets"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/campaigns", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", "publish-tok")
	req.Header.Set("X-Hackme-Orders-Owner-Ref", "customer:x")
	req.RemoteAddr = "203.0.113.10:4444"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 budget_hmc_required, got %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "budget_hmc") {
		t.Fatalf("want budget_hmc error, got %s", rec.Body.String())
	}
}

func TestOrdersPublishSettleAckRequiresValidReceipt(t *testing.T) {
	globalPublishRate = &publishRateLimiter{hour: map[string][]time.Time{}, day: map[string][]time.Time{}}
	db, err := store.Open(filepath.Join(t.TempDir(), "ack.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	pf := &poolfuzz.Service{DB: db}
	mux := http.NewServeMux()
	addFuzzPoolRoutes(mux, "admin-tok", "worker-tok", "publish-tok", false, &workManager{
		hybridSignerEnabled: true, claimRequirePubKey: true, claimPerMin: 60, submitPerMin: 60,
		abuse: make(map[string]workerAbuseState), ipAbuse: make(map[string]workerAbuseState),
		worker: map[string]workerPayoutStat{}, dropReasonCount: make(map[string]uint64),
	}, pf)

	rec := publishReq(t, mux, "publish-tok", map[string]any{
		"id": "camp-ack-rcpt", "budget_runs": 8, "owner_ref": "customer:rcpt",
		"config": map[string]any{"dig_package": "scan", "guard_pack": "secrets"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("register want 200, got %d %s", rec.Code, rec.Body.String())
	}
	secret := publishOwnerSecretFromResp(t, rec)
	ctx := context.Background()
	outboxID, err := pf.EnqueueSettleOutbox(ctx, "run", "camp-ack-rcpt", "HMC-1111111111111111", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	eventID := chain.FuzzSettleEventID("camp-ack-rcpt", outboxID)
	rc := poolsync.SettleApplyReceipt{
		OutboxID: outboxID, EventID: eventID, CampaignID: "camp-ack-rcpt",
		Kind: "run", Outcome: poolsync.SettleOutcomeApplied,
		UnitsHash: poolsync.UnitsHashFromOutbox(eventID, "camp-ack-rcpt", "run", "HMC-1111111111111111", "", 0, poolsync.SettleOutcomeApplied),
	}
	poolsync.SignSettleReceipt("publish-tok", "customer:rcpt", secret, &rc)
	raw, _ := json.Marshal(map[string]any{
		"ids": []int64{outboxID}, "campaign_id": "camp-ack-rcpt", "receipts": []poolsync.SettleApplyReceipt{rc},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/settle/outbox/ack", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", "publish-tok")
	req.Header.Set("X-Hackme-Orders-Owner-Ref", "customer:rcpt")
	req.Header.Set("X-Hackme-Orders-Owner-Secret", secret)
	ack := httptest.NewRecorder()
	mux.ServeHTTP(ack, req)
	if ack.Code != http.StatusOK {
		t.Fatalf("valid receipt ACK want 200, got %d %s", ack.Code, ack.Body.String())
	}
	// Invented escrow units_hash (PUB-07) rejected even with valid MAC.
	outboxIDInvent, err := pf.EnqueueSettleOutbox(ctx, "run", "camp-ack-rcpt", "HMC-1111111111111111", "", 7)
	if err != nil {
		t.Fatal(err)
	}
	eventInvent := chain.FuzzSettleEventID("camp-ack-rcpt", outboxIDInvent)
	invented := poolsync.SettleApplyReceipt{
		OutboxID: outboxIDInvent, EventID: eventInvent, CampaignID: "camp-ack-rcpt",
		Kind: "run", Outcome: poolsync.SettleOutcomeApplied,
		UnitsHash: poolsync.UnitsHashFromEscrow(eventInvent, "camp-ack-rcpt", &chain.FuzzEscrowRow{
			BudgetHMC: 999, RunsPaidHMC: 999, Status: "open",
		}),
	}
	poolsync.SignSettleReceipt("publish-tok", "customer:rcpt", secret, &invented)
	raw, _ = json.Marshal(map[string]any{
		"ids": []int64{outboxIDInvent}, "campaign_id": "camp-ack-rcpt", "receipts": []poolsync.SettleApplyReceipt{invented},
	})
	req = httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/settle/outbox/ack", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", "publish-tok")
	req.Header.Set("X-Hackme-Orders-Owner-Ref", "customer:rcpt")
	req.Header.Set("X-Hackme-Orders-Owner-Secret", secret)
	ack = httptest.NewRecorder()
	mux.ServeHTTP(ack, req)
	if ack.Code != http.StatusBadRequest || !strings.Contains(ack.Body.String(), "units_hash") {
		t.Fatalf("invented units_hash want 400 units_hash, got %d %s", ack.Code, ack.Body.String())
	}
	// Tampered MAC rejected on a fresh pending row.
	outboxID2, err := pf.EnqueueSettleOutbox(ctx, "finding", "camp-ack-rcpt", "HMC-1111111111111111", "high", 0)
	if err != nil {
		t.Fatal(err)
	}
	eventID2 := chain.FuzzSettleEventID("camp-ack-rcpt", outboxID2)
	bad := poolsync.SettleApplyReceipt{
		OutboxID: outboxID2, EventID: eventID2, CampaignID: "camp-ack-rcpt",
		Kind: "finding", Outcome: poolsync.SettleOutcomeApplied,
		UnitsHash: poolsync.UnitsHashFromOutbox(eventID2, "camp-ack-rcpt", "finding", "HMC-1111111111111111", "high", 0, poolsync.SettleOutcomeApplied),
		MAC:       strings.Repeat("ab", 32),
	}
	raw, _ = json.Marshal(map[string]any{
		"ids": []int64{outboxID2}, "campaign_id": "camp-ack-rcpt", "receipts": []poolsync.SettleApplyReceipt{bad},
	})
	req = httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/settle/outbox/ack", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", "publish-tok")
	req.Header.Set("X-Hackme-Orders-Owner-Ref", "customer:rcpt")
	req.Header.Set("X-Hackme-Orders-Owner-Secret", secret)
	ack = httptest.NewRecorder()
	mux.ServeHTTP(ack, req)
	if ack.Code != http.StatusBadRequest {
		t.Fatalf("tampered receipt want 400, got %d %s", ack.Code, ack.Body.String())
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
	m := map[string]any{
		"id": "camp-wheader", "budget_runs": 8, "owner_ref": "customer:wh",
		"config": map[string]any{"dig_package": "scan", "guard_pack": "secrets"},
	}
	stampPublishPrepaid("publish-tok", m)
	raw, _ := json.Marshal(m)
	req := httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/campaigns", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Worker-Token", "publish-tok")
	req.Header.Set("X-Hackme-Orders-Owner-Ref", "customer:wh")
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
	req.Header.Set("X-Hackme-Orders-Owner-Ref", "customer:wh")
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

func TestHubAppliedRefusedForOrdersPublishOutbox(t *testing.T) {
	globalPublishRate = &publishRateLimiter{hour: map[string][]time.Time{}, day: map[string][]time.Time{}}
	db, err := store.Open(filepath.Join(t.TempDir(), "huback.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	pf := &poolfuzz.Service{DB: db}
	mux := http.NewServeMux()
	addFuzzPoolRoutes(mux, "admin-tok", "worker-tok", "publish-tok", false, &workManager{
		hybridSignerEnabled: true, claimRequirePubKey: true, claimPerMin: 60, submitPerMin: 60,
		abuse: make(map[string]workerAbuseState), ipAbuse: make(map[string]workerAbuseState),
		worker: map[string]workerPayoutStat{}, dropReasonCount: make(map[string]uint64),
	}, pf)

	rec := publishReq(t, mux, "publish-tok", map[string]any{
		"id": "camp-hub-ack", "budget_runs": 8, "owner_ref": "customer:huback",
		"config": map[string]any{"dig_package": "scan", "guard_pack": "secrets"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("register want 200, got %d %s", rec.Code, rec.Body.String())
	}
	ctx := context.Background()
	outboxID, err := pf.EnqueueSettleOutbox(ctx, "run", "camp-hub-ack", "HMC-1111111111111111", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"ids": []int64{outboxID}, "hub_applied": true})
	req := httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/settle/outbox/ack", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", "admin-tok")
	ack := httptest.NewRecorder()
	mux.ServeHTTP(ack, req)
	if ack.Code != http.StatusBadRequest || !strings.Contains(ack.Body.String(), "orders_publish") {
		t.Fatalf("SETTLE-01 hub_applied on orders_publish want 400, got %d %s", ack.Code, ack.Body.String())
	}
}

func TestPublishOwnerGateRejectsQueryOwnerRefAndLegacyNoSecret(t *testing.T) {
	if publishOwnerAccessOK(httptest.NewRequest(http.MethodGet, "/x?owner_ref=customer:a", nil), "customer:a", "deadbeef") {
		t.Fatal("query owner_ref must not satisfy gate (PUB-10b)")
	}
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Hackme-Orders-Owner-Ref", "customer:a")
	if publishOwnerAccessOK(req, "customer:a", "") {
		t.Fatal("legacy owner_ref without secret hash must deny (PUB-05b)")
	}
	req.Header.Set("X-Hackme-Orders-Owner-Secret", "secret")
	hash := poolsync.OwnerSecretHash("secret")
	if !publishOwnerAccessOK(req, "customer:a", hash) {
		t.Fatal("header owner_ref + matching secret must pass")
	}
}

func TestPoolStatsRedactsFleetForAnon(t *testing.T) {
	mux := ordersPublishMux(t)
	req := httptest.NewRequest(http.MethodGet, "/api/fuzz/pool/stats", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("public stats want 200, got %d", rec.Code)
	}
	var anon map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &anon); err != nil {
		t.Fatal(err)
	}
	if _, ok := anon["fleet_capacity"]; ok {
		t.Fatal("POOL-01 anon must not see fleet_capacity")
	}
	if _, ok := anon["fleet_hashrate_gh_s"]; ok {
		t.Fatal("POOL-01 anon must not see fleet_hashrate_gh_s")
	}
	req = httptest.NewRequest(http.MethodGet, "/api/fuzz/pool/stats", nil)
	req.Header.Set("X-Hackme-Admin-Token", "admin-tok")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin stats want 200, got %d %s", rec.Code, rec.Body.String())
	}
	var admin map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &admin); err != nil {
		t.Fatal(err)
	}
	if _, ok := admin["fleet_capacity"]; !ok {
		t.Fatal("admin stats must include fleet_capacity")
	}
}
