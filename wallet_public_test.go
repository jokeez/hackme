package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
