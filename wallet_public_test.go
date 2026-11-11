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
