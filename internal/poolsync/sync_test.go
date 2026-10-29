package poolsync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestRegisterWithRetryOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/fuzz/pool/campaigns" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-Hackme-Admin-Token") != "tok" {
			http.Error(w, "auth", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()

	t.Setenv("HACKME_POOL_COORDINATOR_URL", srv.URL)
	t.Setenv("HACKME_POOL_SYNC_PREFER_LOOPBACK", "0")
	t.Setenv("HACKME_COORDINATOR_ADMIN_TOKEN", "tok")
	t.Setenv("HACKME_POOL_SYNC_MAX_ATTEMPTS", "2")

	err := RegisterWithRetry(context.Background(), RegisterRequest{
		ID: "c1", CampaignType: "fuzz", Title: "t", Status: "running", BudgetRuns: 8,
		Config: map[string]any{"wasm_check_hex": "ab"},
	})
	if err != nil {
		t.Fatal(err)
	}
	snap := Snapshot()
	if snap.TotalOK < 1 {
		t.Fatalf("metrics: %+v", snap)
	}
}

func TestResolveCoordinatorURLSkipsLoopbackWhenDisabled(t *testing.T) {
	t.Setenv("HACKME_POOL_COORDINATOR_URL", "https://hackme.tech/pool/coordinator")
	t.Setenv("HACKME_POOL_SYNC_PREFER_LOOPBACK", "0")
	if got := ResolveCoordinatorURL(); got != "https://hackme.tech/pool/coordinator" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveCoordinatorURLPrefersLoopbackFor18083(t *testing.T) {
	t.Setenv("HACKME_POOL_COORDINATOR_URL", "http://127.0.0.1:18083")
	t.Setenv("HACKME_POOL_SYNC_PREFER_LOOPBACK", "1")
	t.Setenv("HACKME_POOL_SYNC_LOOPBACK_URL", "http://127.0.0.1:18081")
	// Without a live coordinator, health check fails and URL stays on :18083.
	got := ResolveCoordinatorURL()
	if got != "http://127.0.0.1:18083" && got != "http://127.0.0.1:18081" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveCoordinatorURLFallsBackPublicWhen18083AdminDenied(t *testing.T) {
	pub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer pub.Close()

	t.Setenv("HACKME_POOL_COORDINATOR_URL", "http://132.243.112.100:18083")
	t.Setenv("HACKME_POOL_SYNC_PREFER_LOOPBACK", "1")
	t.Setenv("HACKME_POOL_SYNC_LOOPBACK_URL", "http://127.0.0.1:9") // closed port → health fail
	t.Setenv("HACKME_POOL_ADMIN_COORDINATOR_URL", pub.URL)
	t.Setenv("HACKME_PUBLIC_AUTHORITY_BASE", "")
	got := ResolveCoordinatorURL()
	if got != pub.URL {
		t.Fatalf("want public admin fallback %q, got %q", pub.URL, got)
	}
}

func TestRegisterOnceRejectsNonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "busy", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	_, err := RegisterOnce(context.Background(), srv.URL, "tok", RegisterRequest{ID: "x", BudgetRuns: 8})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMain(m *testing.M) {
	// reset metrics between tests in package
	os.Exit(m.Run())
}
