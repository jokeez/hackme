package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIPFromRemoteAddrWhenNotTrusted(t *testing.T) {
	trustClientForwardedFor = false
	req := httptest.NewRequest(http.MethodPost, "/api/work/claim", nil)
	req.RemoteAddr = "10.10.10.10:9999"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := clientIP(req); got != "10.10.10.10" {
		t.Fatalf("clientIP=%q want 10.10.10.10", got)
	}
}

func TestClientIPFromXForwardedForWhenTrusted(t *testing.T) {
	trustClientForwardedFor = true
	req := httptest.NewRequest(http.MethodPost, "/api/work/claim", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")
	if got := clientIP(req); got != "203.0.113.9" {
		t.Fatalf("clientIP=%q want 203.0.113.9", got)
	}
}

func TestClientIPPrefersCFConnectingIP(t *testing.T) {
	trustClientForwardedFor = true
	req := httptest.NewRequest(http.MethodPost, "/api/work/claim", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	req.Header.Set("CF-Connecting-IP", "45.142.32.81")
	req.Header.Set("X-Forwarded-For", "198.51.100.1")
	if got := clientIP(req); got != "45.142.32.81" {
		t.Fatalf("clientIP=%q want 45.142.32.81", got)
	}
}

func TestInitClientIPTrustLoopbackAuto(t *testing.T) {
	t.Setenv("HACKME_COORDINATOR_TRUST_X_FORWARDED_FOR", "")
	t.Setenv("HACKME_TRUST_X_FORWARDED_FOR", "")
	initClientIPTrust("127.0.0.1:18081")
	if !trustClientForwardedFor {
		t.Fatal("expected auto-trust on loopback bind")
	}
	initClientIPTrust("0.0.0.0:18081")
	if trustClientForwardedFor {
		t.Fatal("expected no auto-trust on 0.0.0.0 bind")
	}
}
