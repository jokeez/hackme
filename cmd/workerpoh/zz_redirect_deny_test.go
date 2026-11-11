package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// WPOH-REDIR-01: claim/submit/push clients must not follow 3xx with Admin-Token.
func TestNewWorkerHTTPClientRefusesRedirectTokenLeak(t *testing.T) {
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

	cl := newWorkerHTTPClient(5 * time.Second)
	req, err := http.NewRequest(http.MethodPost, redirector.URL+"/api/work/claim", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Hackme-Admin-Token", "coord-admin-must-not-leak")
	_, err = cl.Do(req)
	if err == nil || !strings.Contains(err.Error(), "refusing HTTP redirect") {
		t.Fatalf("expected redirect refusal, got %v", err)
	}
	if collectorSawToken {
		t.Fatal("admin token leaked to redirect target")
	}
}
