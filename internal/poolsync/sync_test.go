package poolsync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestOrdersPublishTokenPreferred(t *testing.T) {
	t.Setenv("HACKME_COORDINATOR_ORDERS_PUBLISH_TOKEN", "pub-only")
	t.Setenv("HACKME_COORDINATOR_ADMIN_TOKEN", "admin-only")
	t.Setenv("HACKME_POOL_COORDINATOR_TOKEN", "worker-only")
	if got := OrdersPublishToken(); got != "pub-only" {
		t.Fatalf("publish=%q", got)
	}
	if got := adminToken(); got != "pub-only" {
		t.Fatalf("register auth should prefer publish, got %q", got)
	}
	if got := CoordinatorAdminToken(); got != "admin-only" {
		t.Fatalf("admin must stay admin, got %q", got)
	}
}

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
	// Clear publish envs so ambient shell secrets cannot override adminToken().
	t.Setenv("HACKME_COORDINATOR_ORDERS_PUBLISH_TOKEN", "")
	t.Setenv("HACKME_POOL_ORDERS_PUBLISH_TOKEN", "")
	t.Setenv("HACKME_ORDERS_PUBLISH_TOKEN", "")
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

func TestRegisterOnceRejectsNonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "busy", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	_, _, err := RegisterOnce(context.Background(), srv.URL, "tok", RegisterRequest{ID: "x", BudgetRuns: 8})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMain(m *testing.M) {
	// reset metrics between tests in package
	os.Exit(m.Run())
}
