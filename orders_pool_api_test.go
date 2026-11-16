package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"hackme/internal/chain"
	"hackme/internal/store"
)

func TestOrdersPoolFundConfirmAuthAndCredit(t *testing.T) {
	t.Setenv("HACKME_POOL_ORDERS_PUBLISH_TOKEN", "pool-publish-test-token")
	t.Setenv("HACKME_ORDERS_PREPAID_HUB_SECRET", "hub-prepaid-secret-for-tests-only")
	t.Setenv("HACKME_CHAIN_MINTING_HUB", "1")

	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "node.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := chain.New(db)
	ctx := context.Background()
	if _, _, err := svc.InitGenesis(ctx, "HMC-poolapitestnode01"); err != nil {
		t.Fatal(err)
	}

	a := &app{chain: svc}
	owner := "HMC-aabbccddeeff0011"
	units := chain.HMCToUnits(20)

	body, _ := json.Marshal(map[string]any{"tx_hash": "aa00112233445566778899aabbccddee", "amount_units": units})
	req := httptest.NewRequest(http.MethodPost, "/api/orders/pool/fund/confirm", bytes.NewReader(body))
	req.Header.Set("X-Hackme-Admin-Token", "pool-publish-test-token")
	req.Header.Set("X-Hackme-Orders-Owner-Ref", owner)
	rec := httptest.NewRecorder()
	a.handleOrdersPoolAPI(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("auth should pass, got %d %s", rec.Code, rec.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/orders/pool/fund/confirm", bytes.NewReader(body))
	req2.Header.Set("X-Hackme-Admin-Token", "pool-publish-test-token")
	rec2 := httptest.NewRecorder()
	a.handleOrdersPoolAPI(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("want 400 without owner-ref, got %d %s", rec2.Code, rec2.Body.String())
	}

	req3 := httptest.NewRequest(http.MethodPost, "/api/orders/pool/fund/confirm", bytes.NewReader(body))
	req3.Header.Set("X-Hackme-Admin-Token", "wrong")
	req3.Header.Set("X-Hackme-Orders-Owner-Ref", owner)
	rec3 := httptest.NewRecorder()
	a.handleOrdersPoolAPI(rec3, req3)
	if rec3.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec3.Code)
	}

	if _, err := svc.CreditOrdersFundForOwner(ctx, owner, "bb00112233445566778899aabbccddee", units); err != nil {
		t.Fatal(err)
	}
	bal, err := svc.GetOrdersPrepaidHMC(ctx, owner)
	if err != nil || bal < 19.9 {
		t.Fatalf("bal=%.4f err=%v", bal, err)
	}
}

func TestOrdersAllowLocalFallbackDefaultOff(t *testing.T) {
	t.Setenv("HACKME_ORDERS_ALLOW_LOCAL_FALLBACK", "")
	if ordersAllowLocalFallback() {
		t.Fatal("default must be off")
	}
	t.Setenv("HACKME_ORDERS_ALLOW_LOCAL_FALLBACK", "1")
	if !ordersAllowLocalFallback() {
		t.Fatal("want on")
	}
}
