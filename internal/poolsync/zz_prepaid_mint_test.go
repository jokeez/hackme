package poolsync

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The coordinator resolves its admin token from ORDERS_ADMIN first (orders_pool.go), and
// that is the key the node deploy syncs. CoordinatorAdminToken never read it, so a mint
// request would have arrived unauthenticated — which is why nothing could script the
// documented "hub admin mint" path.
func TestOrdersAdminTokenMirrorsCoordinatorOrder(t *testing.T) {
	for _, k := range []string{
		"HACKME_COORDINATOR_ORDERS_ADMIN_TOKEN",
		"HACKME_COORDINATOR_CHAIN_ADMIN_TOKEN",
		"HACKME_COORDINATOR_ADMIN_TOKEN",
		"HACKME_POOL_COORDINATOR_ADMIN_TOKEN",
	} {
		t.Setenv(k, "")
	}
	if got := OrdersAdminToken(); got != "" {
		t.Fatalf("no token configured but got one")
	}
	t.Setenv("HACKME_POOL_COORDINATOR_ADMIN_TOKEN", "last")
	if got := OrdersAdminToken(); got != "last" {
		t.Fatalf("got %q, want the only configured token", got)
	}
	t.Setenv("HACKME_COORDINATOR_ORDERS_ADMIN_TOKEN", "orders")
	if got := OrdersAdminToken(); got != "orders" {
		t.Fatalf("got %q, want the coordinator's first choice to win", got)
	}
}

func TestMintHubPrepaidTicket(t *testing.T) {
	const adminTok = "admin-secret-token"
	var gotBody map[string]any
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/fuzz/pool/prepaid-ticket" || r.Method != http.MethodPost {
			http.Error(w, "unexpected route", http.StatusNotFound)
			return
		}
		gotAuth = r.Header.Get("X-Hackme-Admin-Token")
		if gotAuth != adminTok {
			http.Error(w, "admin authentication required", http.StatusUnauthorized)
			return
		}
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		_ = json.Unmarshal(raw, &gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":                  true,
			"prepaid_attestation": HubPrepaidTicketPrefix + "signed-by-hub",
			"escrow_fingerprint":  "fp-from-hub",
		})
	}))
	defer srv.Close()

	ticket, fp, err := MintHubPrepaidTicket(context.Background(), srv.URL, adminTok,
		"HMC-owner0000000001", "camp-1", 15.0, 4000)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if !IsHubPrepaidTicket(ticket) {
		t.Fatalf("ticket %q is not hub-signed", ticket)
	}
	// Publish verifies against the fingerprint the hub bound, not ours.
	if fp != "fp-from-hub" {
		t.Fatalf("fingerprint %q, want the hub's", fp)
	}
	if gotBody["campaign_id"] != "camp-1" || gotBody["owner_ref"] != "HMC-owner0000000001" {
		t.Fatalf("mint body lost the campaign identity: %v", gotBody)
	}
	if gotBody["budget_hmc"] != 15.0 || gotBody["budget_runs"] != float64(4000) {
		t.Fatalf("mint body lost the budget: %v", gotBody)
	}

	// Missing token must fail before any request, and never echo a secret.
	if _, _, err := MintHubPrepaidTicket(context.Background(), srv.URL, "", "o", "c", 1, 1); err == nil {
		t.Fatal("mint without an admin token succeeded")
	}
	if _, _, err := MintHubPrepaidTicket(context.Background(), srv.URL, adminTok, "o", "c", 0, 1); err == nil {
		t.Fatal("mint with a zero budget succeeded")
	}
}

// A hub that will not sign must say why, and the reason must not carry the token.
func TestMintHubPrepaidTicketSurfacesRefusal(t *testing.T) {
	const adminTok = "admin-secret-token"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "hub escrow required before prepaid mint: escrow not open\nsecond line", http.StatusConflict)
	}))
	defer srv.Close()
	_, _, err := MintHubPrepaidTicket(context.Background(), srv.URL, adminTok, "o", "c", 15, 4000)
	if err == nil {
		t.Fatal("a 409 refusal was treated as success")
	}
	if !strings.Contains(err.Error(), "escrow not open") {
		t.Fatalf("error %q hides the hub's reason", err)
	}
	if strings.Contains(err.Error(), adminTok) {
		t.Fatalf("error leaked the admin token: %q", err)
	}
	if strings.Contains(err.Error(), "second line") {
		t.Fatalf("error kept more than one line: %q", err)
	}
}

// A self-MAC would be refused at publish, so the mint must not pass one off as a ticket.
func TestMintHubPrepaidTicketRejectsNonHubTicket(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":                  true,
			"prepaid_attestation": "selfmac:not-a-hub-ticket",
		})
	}))
	defer srv.Close()
	if _, _, err := MintHubPrepaidTicket(context.Background(), srv.URL, "tok", "o", "c", 15, 4000); err == nil {
		t.Fatal("a non-hub ticket was accepted")
	}
}

// End to end: on a minting hub with local signing refused, RegisterOnce used to fail with
// "prepaid_attestation required" and the campaign sat in running with a funded escrow and
// zero runs. It must now mint and publish.
func TestRegisterOnceMintsWhenLocalSignRefused(t *testing.T) {
	const adminTok = "admin-secret-token"
	minted := false
	published := false
	var publishedAttest, publishedFP string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/fuzz/pool/prepaid-ticket":
			if r.Header.Get("X-Hackme-Admin-Token") != adminTok {
				http.Error(w, "admin authentication required", http.StatusUnauthorized)
				return
			}
			minted = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":                  true,
				"prepaid_attestation": HubPrepaidTicketPrefix + "signed-by-hub",
				"escrow_fingerprint":  "fp-from-hub",
			})
		case "/api/fuzz/pool/campaigns":
			raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			var got RegisterRequest
			_ = json.Unmarshal(raw, &got)
			publishedAttest, publishedFP = got.PrepaidAttestation, got.EscrowFingerprint
			published = true
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			http.Error(w, "unexpected route", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	t.Setenv("HACKME_ORDERS_PUBLISH_TOKEN", "publish-token")
	t.Setenv("HACKME_CHAIN_MINTING_HUB", "1")
	t.Setenv("HACKME_ORDERS_PREPAID_LOCAL_SIGN", "0")
	t.Setenv("HACKME_COORDINATOR_ORDERS_ADMIN_TOKEN", adminTok)

	_, _, err := RegisterOnce(context.Background(), srv.URL, "publish-token", RegisterRequest{
		ID: "camp-stuck-1", CampaignType: "hunt", Status: "running",
		OwnerRef: "HMC-owner0000000001", BudgetHMC: 15.0, BudgetRuns: 4000,
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if !minted {
		t.Fatal("register never asked the hub to mint a ticket")
	}
	if !published {
		t.Fatal("register never published the campaign")
	}
	if !IsHubPrepaidTicket(publishedAttest) {
		t.Fatalf("published attestation %q is not the hub ticket", publishedAttest)
	}
	if publishedFP != "fp-from-hub" {
		t.Fatalf("published fingerprint %q, want the one the hub bound", publishedFP)
	}
}
