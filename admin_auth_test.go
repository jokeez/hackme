package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestRequestFromLoopbackUsesRemoteAddrNotHost(t *testing.T) {
	req := httptestReq(t, "127.0.0.1:54321", "evil.example")
	if !requestFromLoopback(req) {
		t.Fatal("loopback RemoteAddr must be trusted even when Host is spoofed")
	}

	req = httptestReq(t, "203.0.113.9:443", "localhost")
	if requestFromLoopback(req) {
		t.Fatal("Host: localhost from non-loopback RemoteAddr must NOT get loopback trust")
	}

	req = httptestReq(t, "10.0.0.5:9999", "127.0.0.1")
	if requestFromLoopback(req) {
		t.Fatal("Host: 127.0.0.1 from non-loopback RemoteAddr must NOT get loopback trust")
	}

	req = httptestReq(t, "[::1]:8080", "example.com")
	if !requestFromLoopback(req) {
		t.Fatal("::1 RemoteAddr must be loopback")
	}
}

func TestRequestFromLoopbackRejectsForwardedHeaders(t *testing.T) {
	req := httptestReq(t, "127.0.0.1:1", "127.0.0.1")
	req.Header.Set("X-Forwarded-For", "203.0.113.1")
	if requestFromLoopback(req) {
		t.Fatal("X-Forwarded-For must deny loopback trust")
	}

	req = httptestReq(t, "127.0.0.1:1", "127.0.0.1")
	req.Header.Set("X-Real-IP", "203.0.113.1")
	if requestFromLoopback(req) {
		t.Fatal("X-Real-IP must deny loopback trust")
	}
}

func TestRequireAdminAuthStrictFailClosed(t *testing.T) {
	t.Setenv("HACKME_ADMIN_TOKEN", "")
	_ = os.Unsetenv("HACKME_ADMIN_TOKEN")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/from_code", nil)
	if requireAdminAuthStrict(rec, req) {
		t.Fatal("strict auth must fail-closed when token unset")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}

	t.Setenv("HACKME_ADMIN_TOKEN", "secret-admin")
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/tasks", nil)
	if requireAdminAuthStrict(rec, req) {
		t.Fatal("missing header must fail")
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/tasks", nil)
	req.Header.Set("X-Hackme-Admin-Token", "secret-admin")
	if !requireAdminAuthStrict(rec, req) {
		t.Fatal("valid token must pass")
	}
}

func TestDesktopMutatingOriginOK(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/tx/send", nil)
	req.Host = "127.0.0.1:8080"
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	if desktopMutatingOriginOK(req) {
		t.Fatal("cross-site must be rejected")
	}
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Origin", "http://evil.example")
	if desktopMutatingOriginOK(req) {
		t.Fatal("mismatched Origin must be rejected")
	}
	req.Header.Set("Origin", "http://127.0.0.1:8080")
	if !desktopMutatingOriginOK(req) {
		t.Fatal("same-origin Origin must pass")
	}
	// DNS rebind: Origin matches Host but Host is not a loopback literal.
	req.Host = "evil.example"
	req.Header.Set("Origin", "http://evil.example")
	if desktopMutatingOriginOK(req) {
		t.Fatal("rebind Host must be rejected even when Origin matches Host")
	}
	if requestHostIsLoopbackLiteral(req) {
		t.Fatal("evil.example must not count as loopback Host")
	}
	req.Host = "127.0.0.1:8080"
	req.Header.Del("Sec-Fetch-Site")
	req.Header.Del("Origin")
	if !desktopMutatingOriginOK(req) {
		t.Fatal("non-browser clients without fetch metadata must pass")
	}
}

func TestDesktopLocalAuthRequiresExposeFlag(t *testing.T) {
	t.Setenv("HACKME_DESKTOP_MODE", "1")
	t.Setenv("HACKME_ADMIN_TOKEN", "desktop-secret")
	t.Setenv("HACKME_DESKTOP_EXPOSE_ADMIN_TOKEN", "0")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/desktop/local-auth", nil)
	req.RemoteAddr = "127.0.0.1:9"
	req.Host = "127.0.0.1:8080"
	handleDesktopLocalAuth(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["admin_token"]; ok {
		t.Fatal("admin_token must not be returned without HACKME_DESKTOP_EXPOSE_ADMIN_TOKEN=1")
	}

	t.Setenv("HACKME_DESKTOP_EXPOSE_ADMIN_TOKEN", "1")
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/desktop/local-auth", nil)
	req.RemoteAddr = "127.0.0.1:9"
	req.Host = "127.0.0.1:8080"
	handleDesktopLocalAuth(rec, req)
	body = map[string]any{}
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if body["admin_token"] != "desktop-secret" {
		t.Fatalf("expose=1 should return token, got %#v", body["admin_token"])
	}
}

func TestDesktopAdminTokenEmbedRequiresExposeFlag(t *testing.T) {
	t.Setenv("HACKME_DESKTOP_MODE", "1")
	t.Setenv("HACKME_ADMIN_TOKEN", "desktop-secret")
	t.Setenv("HACKME_DESKTOP_EXPOSE_ADMIN_TOKEN", "0")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:9"
	req.Host = "127.0.0.1:8080"
	if got := desktopAdminTokenEmbedScript(req); got != "" {
		t.Fatalf("HTML embed must be empty without EXPOSE flag, got %q", got)
	}
	t.Setenv("HACKME_DESKTOP_EXPOSE_ADMIN_TOKEN", "1")
	got := desktopAdminTokenEmbedScript(req)
	if !strings.Contains(got, "__HACKME_EMBEDDED_ADMIN_TOKEN__") || !strings.Contains(got, "desktop-secret") {
		t.Fatalf("expose=1 should embed token script, got %q", got)
	}
	req.RemoteAddr = "203.0.113.9:9"
	if got := desktopAdminTokenEmbedScript(req); got != "" {
		t.Fatal("non-loopback must never embed admin token")
	}
	req.RemoteAddr = "127.0.0.1:9"
	req.Host = "evil.example"
	if got := desktopAdminTokenEmbedScript(req); got != "" {
		t.Fatal("non-loopback Host must never embed admin token")
	}
}

func httptestReq(t *testing.T, remoteAddr, host string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+host+"/api/desktop/local-auth", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.RemoteAddr = remoteAddr
	req.Host = host
	return req
}
