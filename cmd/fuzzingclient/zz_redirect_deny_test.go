package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Token-carrying fuzzingclient HTTP helpers must not follow 3xx (X-Hackme-* headers
// are not stripped cross-origin by Go's default client).
func TestAPIDoRefusesRedirectTokenLeak(t *testing.T) {
	var collectorSawToken bool
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Hackme-Developer-Token") != "" {
			collectorSawToken = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true,"leaked":true}`)
	}))
	defer collector.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, collector.URL+"/collect", http.StatusFound)
	}))
	defer redirector.Close()

	_, _, err := apiDo(redirector.URL, "devsecret-must-not-leak", http.MethodGet, "/api/wallet", nil)
	if err == nil {
		t.Fatal("expected redirect refusal")
	}
	if !strings.Contains(err.Error(), "refusing HTTP redirect") {
		t.Fatalf("unexpected err: %v", err)
	}
	if collectorSawToken {
		t.Fatal("developer token leaked to redirect target")
	}
}

func TestAPIDoAdminRefusesRedirectTokenLeak(t *testing.T) {
	var collectorSawToken bool
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Hackme-Admin-Token") != "" {
			collectorSawToken = true
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer collector.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, collector.URL+"/collect", http.StatusFound)
	}))
	defer redirector.Close()

	_, _, err := apiDoAdmin(redirector.URL, "adminsecret-must-not-leak", http.MethodGet, "/api/fuzz/campaigns", nil)
	if err == nil || !strings.Contains(err.Error(), "refusing HTTP redirect") {
		t.Fatalf("expected redirect refusal, got %v", err)
	}
	if collectorSawToken {
		t.Fatal("admin token leaked to redirect target")
	}
}

func TestFetchJSONAuthRefusesRedirectTokenLeak(t *testing.T) {
	t.Setenv("HACKME_ADMIN_TOKEN", "adminsecret-must-not-leak")
	var collectorSawToken bool
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Hackme-Admin-Token") != "" || r.Header.Get("X-Hackme-Report-Token") != "" {
			collectorSawToken = true
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer collector.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, collector.URL+"/collect", http.StatusFound)
	}))
	defer redirector.Close()

	_, err := fetchJSONAuth(redirector.URL, "reportsecret-must-not-leak", "/api/fuzz/campaigns/x/pulse")
	if err == nil || !strings.Contains(err.Error(), "refusing HTTP redirect") {
		t.Fatalf("expected redirect refusal, got %v", err)
	}
	if collectorSawToken {
		t.Fatal("report/admin token leaked to redirect target")
	}
}
