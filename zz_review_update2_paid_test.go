package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"hackme/internal/chain"
	"hackme/internal/sandbox"
)

// Ordinary first purchase: the included per-order payment is present, there is
// no legacy prepaid account, and the coordinator is available.
func TestReviewUpdate2FirstPaidOrderDoesNotRequireLegacyPrepaid(t *testing.T) {
	f := newPoolEscrowFixture(t)
	t.Setenv("HACKME_CHAIN_MINTING_HUB", "1")
	t.Setenv("HACKME_ORDERS_PREPAID_HUB_SECRET", "review-only-prepaid-hub-secret")
	t.Setenv("HACKME_POOL_ORDERS_PUBLISH_TOKEN", "review-only-publish-token")
	t.Setenv("HACKME_COORDINATOR_ADMIN_TOKEN", "review-only-coordinator-admin")
	t.Setenv("HACKME_ORDERS_HUB_URL", "")
	coord := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"orders_owner_secret":"review-only-owner-secret"}`))
	}))
	defer coord.Close()
	t.Setenv("HACKME_POOL_COORDINATOR_URL", coord.URL)
	ctx := context.Background()
	owner := f.owner.Address()
	prepaid, err := f.a.chain.GetOrdersPrepaidHMC(ctx, owner)
	if err != nil || prepaid != 0 {
		t.Fatalf("new customer prepaid=%v err=%v", prepaid, err)
	}
	f.order, _ = json.Marshal(map[string]any{
		"dig_package": "scan", "wasm_check_hex": sandbox.MinimalGateWasmHex,
		"title": "First paid scan", "label": "review-first-order",
	})
	// Post-inclusion state: funding is recorded and the recipient's chain balance
	// contains it. The proof is signed by the same customer key as the transfer.
	hub := f.a.signer.Address()
	units := chain.HMCToUnits(10)
	if _, err := f.a.db.Exec(`UPDATE wallet SET balance_units=?, balance_hmc=10 WHERE id=1`, units); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.db.Exec(`UPDATE accounts SET balance_units=? WHERE address=?`, units, hub); err != nil {
		t.Fatal(err)
	}
	camp := "campaign-review-first-paid"
	hash := poolEscrowHash(9001)
	f.putTransfer(hash, "included", owner, hub, 1, ordersPoolEscrowMemoPrefix+camp)
	r := f.post(camp, hash, f.owner, nil)
	if r.Code != http.StatusOK {
		t.Fatalf("included per-order payment must create the order without Fund prepaid: HTTP %d %s", r.Code, r.Body.String())
	}
}
