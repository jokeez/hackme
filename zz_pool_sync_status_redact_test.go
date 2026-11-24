package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hackme/internal/poolsync"
)

func poolSyncRedactFixture() map[string]any {
	return map[string]any{
		"pool_sync": map[string]any{
			"failed_campaigns": map[string]string{
				"hunt-bootstrap-duktape-20261120t013917z":  "hunt harness sync: local artifact: hunt artifact: de2b275b7f165d679bdcf5722626311a not found",
				"campaign-bootstrap-zlib-20261120t095044z": "pool sync: prepaid_attestation required (HACKME_ORDERS_PREPAID_LOCAL_SIGN=0 — use hub admin mint; signing key stays hub-only)",
				"hunt-bootstrap-jsmn-20261120t095044z":     "pool sync: prepaid_attestation required (HACKME_ORDERS_PREPAID_LOCAL_SIGN=0 — use hub admin mint; signing key stays hub-only)",
			},
			"metrics": poolsync.Metrics{
				TotalFail:      14,
				LastStatus:     "fail",
				LastError:      "pool sync: prepaid_attestation required (HACKME_ORDERS_PREPAID_LOCAL_SIGN=0 — use hub admin mint; signing key stays hub-only)",
				LastCampaignID: "campaign-bootstrap-cjson-20261120t095044z",
			},
		},
	}
}

func TestPublicStatusHidesPoolSyncFailureDetail(t *testing.T) {
	body := poolSyncRedactFixture()
	redactPoolSyncFailureDetail(body)

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	blob := string(raw)
	for _, leak := range []string{
		"HACKME_ORDERS_PREPAID_LOCAL_SIGN",
		"hub admin mint",
		"de2b275b7f165d679bdcf5722626311a",
		"hunt-bootstrap-duktape-20261120t013917z",
		"campaign-bootstrap-cjson-20261120t095044z",
	} {
		if strings.Contains(blob, leak) {
			t.Fatalf("public status still leaks %q: %s", leak, blob)
		}
	}

	ps, ok := body["pool_sync"].(map[string]any)
	if !ok {
		t.Fatal("pool_sync dropped")
	}
	if _, still := ps["failed_campaigns"]; still {
		t.Fatal("failed_campaigns must be removed for public callers")
	}
	if got := ps["failed_campaigns_count"]; got != 3 {
		t.Fatalf("failed_campaigns_count = %v, want 3", got)
	}
	reasons, ok := ps["failed_campaign_reasons"].(map[string]int)
	if !ok {
		t.Fatalf("failed_campaign_reasons missing: %#v", ps["failed_campaign_reasons"])
	}
	if reasons["prepaid_attestation_required"] != 2 || reasons["harness_sync_failed"] != 1 {
		t.Fatalf("reason counts = %#v", reasons)
	}
	snap, ok := ps["metrics"].(poolsync.Metrics)
	if !ok {
		t.Fatalf("metrics type changed: %T", ps["metrics"])
	}
	if snap.LastError != "prepaid_attestation_required" {
		t.Fatalf("last_error = %q", snap.LastError)
	}
	if snap.LastCampaignID != "" {
		t.Fatalf("last_campaign_id must be blank, got %q", snap.LastCampaignID)
	}
	if snap.TotalFail != 14 {
		t.Fatalf("counters must survive redaction: %#v", snap)
	}
}

func TestStatusCallerSeesInternalDetail(t *testing.T) {
	t.Setenv("HACKME_ADMIN_TOKEN", "tok-redact-test")

	pub := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	pub.RemoteAddr = "203.0.113.9:40000"
	if statusCallerSeesInternalDetail(pub) {
		t.Fatal("public remote caller must not see operator detail")
	}

	admin := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	admin.RemoteAddr = "203.0.113.9:40001"
	admin.Header.Set("X-Hackme-Admin-Token", "tok-redact-test")
	if !statusCallerSeesInternalDetail(admin) {
		t.Fatal("admin token caller must keep the detail")
	}

	loop := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	loop.RemoteAddr = "127.0.0.1:40002"
	if !statusCallerSeesInternalDetail(loop) {
		t.Fatal("loopback caller must keep the detail")
	}

	if statusCallerSeesInternalDetail(nil) {
		t.Fatal("nil request must be treated as public")
	}
}

func TestPoolSyncFailureReasonCode(t *testing.T) {
	cases := map[string]string{
		"": "",
		"pool sync: prepaid_attestation required":                         "prepaid_attestation_required",
		"hunt harness sync: local artifact: hunt artifact: abc not found": "harness_sync_failed",
		"escrow budget/runs below publish claim":                          "escrow_gate_failed",
		"coordinator register HTTP 429: rate limited":                     "coordinator_rate_limited",
		"coordinator register HTTP 403: forbidden":                        "coordinator_auth_failed",
		"Post \"https://pool/api\": dial tcp: connection refused":         "coordinator_unreachable",
		"something else entirely":                                         "sync_failed",
	}
	for in, want := range cases {
		if got := poolSyncFailureReasonCode(in); got != want {
			t.Fatalf("poolSyncFailureReasonCode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStatusDetailGateFailsClosedWithoutAdminToken(t *testing.T) {
	// adminRequestAuthed fail-opens when the token is unset; the status gate must not.
	t.Setenv("HACKME_ADMIN_TOKEN", "")

	pub := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	pub.RemoteAddr = "198.51.100.7:51000"
	if statusCallerSeesInternalDetail(pub) {
		t.Fatal("token-less node must still redact for remote callers")
	}
}
