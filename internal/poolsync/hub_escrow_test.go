package poolsync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"hackme/internal/chain"
)

func TestMatchOpenHubEscrowForPrepaid(t *testing.T) {
	fp := EscrowPrepaidFingerprintParts("camp-a", 1.5, 8, "open")
	row := &chain.FuzzEscrowRow{
		CampaignID: "camp-a", BudgetHMC: 2.0, BudgetRuns: 16, Status: "open",
	}
	if err := MatchOpenHubEscrowForPrepaid(row, "camp-a", 1.5, 8, fp); err != nil {
		t.Fatalf("want match: %v", err)
	}
	if err := MatchOpenHubEscrowForPrepaid(row, "camp-a", 3.0, 8, fp); err == nil {
		t.Fatal("budget above escrow must fail")
	}
	closed := *row
	closed.Status = "closed"
	if err := MatchOpenHubEscrowForPrepaid(&closed, "camp-a", 1.5, 8, fp); err == nil {
		t.Fatal("closed escrow must fail")
	}
}

func TestFetchHubFuzzEscrowAndVerify(t *testing.T) {
	fp := EscrowPrepaidFingerprintParts("camp-live", 1.0, 8, "open")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Hackme-Admin-Token") != "adm" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/api/fuzz/campaigns/camp-live/escrow" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"escrow": chain.FuzzEscrowRow{
				CampaignID: "camp-live", BudgetHMC: 1.0, BudgetRuns: 8, Status: "open",
			},
		})
	}))
	t.Cleanup(srv.Close)

	row, err := FetchHubFuzzEscrow(context.Background(), srv.URL, "adm", "camp-live")
	if err != nil || row == nil {
		t.Fatalf("fetch: %v %#v", err, row)
	}
	if err := MatchOpenHubEscrowForPrepaid(row, "camp-live", 1.0, 8, fp); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HACKME_ORDERS_PREPAID_ALLOW_ALGEBRAIC_MINT", "")
	if err := VerifyHubPrepaidEscrowLive(context.Background(), srv.URL, "adm", "camp-live", 1.0, 8, fp); err != nil {
		t.Fatalf("live verify: %v", err)
	}
	t.Setenv("HACKME_ORDERS_PREPAID_ALLOW_ALGEBRAIC_MINT", "1")
	if err := VerifyHubPrepaidEscrowLive(context.Background(), "http://127.0.0.1:9", "", "missing", 1.0, 8, fp); err != nil {
		t.Fatalf("algebraic break-glass must skip RPC: %v", err)
	}
}

func TestPrepaidLocalSignDefaultsHubOnlyOnMintingHub(t *testing.T) {
	t.Setenv("HACKME_ORDERS_PREPAID_LOCAL_SIGN", "")
	t.Setenv("HACKME_CHAIN_MINTING_HUB", "1")
	t.Setenv("HACKME_ORDERS_REQUIRE_HUB_PREPAID_TICKET", "")
	if PrepaidLocalSignAllowed() {
		t.Fatal("MINTING_HUB default must keep signing hub-only")
	}
	t.Setenv("HACKME_ORDERS_PREPAID_LOCAL_SIGN", "1")
	if !PrepaidLocalSignAllowed() {
		t.Fatal("explicit LOCAL_SIGN=1 must allow")
	}
	t.Setenv("HACKME_ORDERS_PREPAID_LOCAL_SIGN", "")
	t.Setenv("HACKME_CHAIN_MINTING_HUB", "")
	if !PrepaidLocalSignAllowed() {
		t.Fatal("lab default without minting hub must allow local sign")
	}
}
