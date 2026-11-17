package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleWalletPublicRedacted(t *testing.T) {
	a, _ := newWalletTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/api/wallet", nil)
	rec := httptest.NewRecorder()
	a.handleWallet(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["public_redacted"] != true {
		t.Fatalf("expected public_redacted")
	}
	if _, ok := out["address"]; ok {
		t.Fatalf("address must not be public: %v", out["address"])
	}
	if _, ok := out["data_directory"]; ok {
		t.Fatalf("data_directory leaked")
	}
}

func TestHandleWalletDesktopLoopbackShowsTreasury(t *testing.T) {
	a, _ := newWalletTestApp(t)
	tok := "test-admin-wallet-redact"
	t.Setenv("HACKME_ADMIN_TOKEN", tok)
	t.Setenv("HACKME_DESKTOP_MODE", "1")
	req := httptest.NewRequest(http.MethodGet, "/api/wallet", nil)
	req.RemoteAddr = "127.0.0.1:54321" // loopback trust is RemoteAddr-only (Host is spoofable)
	req.Host = "127.0.0.1:8080"
	rec := httptest.NewRecorder()
	a.handleWallet(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["public_redacted"] == true {
		t.Fatalf("loopback desktop should see full wallet")
	}
	if out["address"] == nil || out["address"] == "" {
		t.Fatalf("expected address on loopback desktop")
	}
}

func TestHandleWalletAdminShowsTreasury(t *testing.T) {
	a, _ := newWalletTestApp(t)
	tok := "test-admin-wallet-redact"
	t.Setenv("HACKME_ADMIN_TOKEN", tok)
	req := httptest.NewRequest(http.MethodGet, "/api/wallet", nil)
	req.Header.Set("X-Hackme-Admin-Token", tok)
	rec := httptest.NewRecorder()
	a.handleWallet(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["public_redacted"] == true {
		t.Fatalf("admin should see full wallet")
	}
	if out["address"] == nil || out["address"] == "" {
		t.Fatalf("expected address for admin")
	}
}

func TestHandleWalletOrdersSpendableTriadFields(t *testing.T) {
	a, _ := newWalletTestApp(t)
	tok := "test-admin-wallet-triad"
	t.Setenv("HACKME_ADMIN_TOKEN", tok)
	req := httptest.NewRequest(http.MethodGet, "/api/wallet", nil)
	req.Header.Set("X-Hackme-Admin-Token", tok)
	rec := httptest.NewRecorder()
	a.handleWallet(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range []string{
		"balance_orders_spendable_hmc",
		"balance_orders_escrow_locked_hmc",
		"orders_spendable_note",
	} {
		if _, ok := out[key]; !ok {
			t.Fatalf("missing %s in wallet JSON", key)
		}
	}
	note, _ := out["orders_spendable_note"].(string)
	for _, needle := range []string{"Available to start", "On-chain", "In escrow"} {
		if !strings.Contains(note, needle) {
			t.Fatalf("orders_spendable_note missing %q: %q", needle, note)
		}
	}
	if _, ok := out["balance_orders_spendable_hmc"].(float64); !ok {
		t.Fatalf("balance_orders_spendable_hmc type=%T", out["balance_orders_spendable_hmc"])
	}
	if _, ok := out["balance_orders_escrow_locked_hmc"].(float64); !ok {
		t.Fatalf("balance_orders_escrow_locked_hmc type=%T", out["balance_orders_escrow_locked_hmc"])
	}
}

func TestHandleWalletHubPrepaidUnavailableFailsClosed(t *testing.T) {
	a, _ := newWalletTestApp(t)
	tok := "test-admin-wallet-hub-prepaid"
	t.Setenv("HACKME_ADMIN_TOKEN", tok)
	// Hub URL set but unreachable → spendable must be 0 (not local mirror).
	t.Setenv("HACKME_ORDERS_HUB_URL", "http://127.0.0.1:1")
	t.Setenv("HACKME_POOL_ORDERS_PUBLISH_TOKEN", "review-publish-token-for-local-tests")
	t.Setenv("HACKME_CHAIN_MINTING_HUB", "0")
	req := httptest.NewRequest(http.MethodGet, "/api/wallet", nil)
	req.Header.Set("X-Hackme-Admin-Token", tok)
	rec := httptest.NewRecorder()
	a.handleWallet(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	src, _ := out["balance_orders_spendable_source"].(string)
	if src != "hub_prepaid_unavailable" {
		t.Fatalf("spendable_source=%q want hub_prepaid_unavailable", src)
	}
	spend, _ := out["balance_orders_spendable_hmc"].(float64)
	if spend != 0 {
		t.Fatalf("spendable_hmc=%v want 0 when hub unreachable", spend)
	}
	note, _ := out["orders_spendable_note"].(string)
	if !strings.Contains(note, "Hub prepaid unreachable") {
		t.Fatalf("note=%q", note)
	}
}

func TestHandleWalletHubPrepaidAuthoritative(t *testing.T) {
	a, _ := newWalletTestApp(t)
	tok := "test-admin-wallet-hub-prepaid-ok"
	t.Setenv("HACKME_ADMIN_TOKEN", tok)
	t.Setenv("HACKME_POOL_ORDERS_PUBLISH_TOKEN", "review-publish-token-for-local-tests")
	t.Setenv("HACKME_CHAIN_MINTING_HUB", "0")
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/orders/pool/prepaid" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "spendable_hmc": 42.5,
		})
	}))
	t.Cleanup(hub.Close)
	t.Setenv("HACKME_ORDERS_HUB_URL", hub.URL)
	req := httptest.NewRequest(http.MethodGet, "/api/wallet", nil)
	req.Header.Set("X-Hackme-Admin-Token", tok)
	rec := httptest.NewRecorder()
	a.handleWallet(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	src, _ := out["balance_orders_spendable_source"].(string)
	if src != "hub_prepaid" {
		t.Fatalf("spendable_source=%q want hub_prepaid body=%v", src, out)
	}
	spend, _ := out["balance_orders_spendable_hmc"].(float64)
	if spend != 42.5 {
		t.Fatalf("spendable_hmc=%v want 42.5", spend)
	}
}
