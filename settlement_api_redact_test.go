package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWorkerSettlementBreakdownExposePolicy(t *testing.T) {
	t.Parallel()
	// Public remote host: no admin, no loopback → redact.
	req := httptest.NewRequest(http.MethodGet, "/api/worker/settlement", nil)
	req.RemoteAddr = "203.0.113.9:9"
	req.Host = "hackme.tech"
	if workerSettlementExposeSensitive(req) {
		t.Fatal("public remote must not expose workers_breakdown")
	}

	// Loopback desktop Host: expose for local dashboard.
	req = httptest.NewRequest(http.MethodGet, "/api/worker/settlement", nil)
	req.RemoteAddr = "127.0.0.1:9"
	req.Host = "127.0.0.1:8080"
	if !workerSettlementExposeSensitive(req) {
		t.Fatal("loopback desktop must expose workers_breakdown")
	}
}

func TestWorkerSettlementSourceNotLeakedPublicly(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/api/worker/settlement", nil)
	req.RemoteAddr = "203.0.113.9:9"
	req.Host = "hackme.tech"
	if got := settlementSourceLabel(req, "http://127.0.0.1:18083"); got != "pool" {
		t.Fatalf("public source=%q, want opaque pool label", got)
	}
	req = httptest.NewRequest(http.MethodGet, "/api/worker/settlement", nil)
	req.RemoteAddr = "127.0.0.1:9"
	req.Host = "127.0.0.1:8080"
	if got := settlementSourceLabel(req, "http://127.0.0.1:18083"); got != "http://127.0.0.1:18083" {
		t.Fatalf("loopback desktop source=%q", got)
	}
}
