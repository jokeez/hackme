package poolsync

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFuzzSettlePullEnabled(t *testing.T) {
	t.Setenv("HACKME_FUZZ_SETTLE_PULL", "")
	t.Setenv("HACKME_COORDINATOR_ADMIN_TOKEN", "")
	t.Setenv("HACKME_POOL_COORDINATOR_ADMIN_TOKEN", "")
	t.Setenv("HACKME_POOL_COORDINATOR_TOKEN", "")
	t.Setenv("HACKME_COORDINATOR_ORDERS_PUBLISH_TOKEN", "")
	t.Setenv("HACKME_POOL_ORDERS_PUBLISH_TOKEN", "")
	if FuzzSettlePullEnabled() {
		t.Fatal("want disabled without customer token")
	}
	// Worker pool token must NOT enable settle pull (public pool returns 401).
	t.Setenv("HACKME_POOL_COORDINATOR_TOKEN", "worker-only-token")
	if FuzzSettlePullEnabled() {
		t.Fatal("worker pool token must not enable settle pull")
	}
	t.Setenv("HACKME_COORDINATOR_ORDERS_PUBLISH_TOKEN", "publish-test")
	if !FuzzSettlePullEnabled() {
		t.Fatal("want enabled with orders publish token")
	}
	t.Setenv("HACKME_COORDINATOR_ORDERS_PUBLISH_TOKEN", "")
	t.Setenv("HACKME_COORDINATOR_ADMIN_TOKEN", "admin-test")
	if !FuzzSettlePullEnabled() {
		t.Fatal("want enabled with admin token")
	}
	t.Setenv("HACKME_FUZZ_SETTLE_PULL", "0")
	if FuzzSettlePullEnabled() {
		t.Fatal("want disabled when HACKME_FUZZ_SETTLE_PULL=0")
	}
}

func TestPreferPoolSyncDirect(t *testing.T) {
	t.Setenv("HACKME_POOL_DIRECT", "")
	t.Setenv("HACKME_DESKTOP_GPU_POOL", "1")
	if !preferPoolSyncDirect() {
		t.Fatal("desktop gpu pool should prefer direct direct")
	}
}

func TestPrepaidLocalSignAllowed(t *testing.T) {
	t.Setenv("HACKME_ORDERS_PREPAID_LOCAL_SIGN", "")
	if !PrepaidLocalSignAllowed() {
		t.Fatal("default must allow local sign (ops-compatible)")
	}
	t.Setenv("HACKME_ORDERS_PREPAID_LOCAL_SIGN", "0")
	if PrepaidLocalSignAllowed() {
		t.Fatal("LOCAL_SIGN=0 must keep signing key hub-only")
	}
}

func TestOrdersPrepaidHubSecretFromFile(t *testing.T) {
	t.Setenv("HACKME_ORDERS_PREPAID_HUB_SECRET", "")
	t.Setenv("HACKME_COORDINATOR_ORDERS_PREPAID_HUB_SECRET", "")
	t.Setenv("HACKME_POOL_ORDERS_PREPAID_HUB_SECRET", "")
	dir := t.TempDir()
	const want = "file-backed-prepaid-hub-secret!!"
	if err := os.WriteFile(filepath.Join(dir, "pool.orders.prepaid_hub.secret"), []byte(want+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HACKME_REPO_ROOT", dir)
	if got := OrdersPrepaidHubSecret(); got != want {
		t.Fatalf("OrdersPrepaidHubSecret()=%q want %q", got, want)
	}
}
