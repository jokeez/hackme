package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"hackme/internal/chain"
	"hackme/internal/nodecrypto"
	"hackme/internal/store"
)

func reviewRC20Customer(t *testing.T, hub string) *app {
	t.Helper()
	t.Setenv("HACKME_ORDERS_HUB_URL", hub)
	t.Setenv("HACKME_POOL_ORDERS_PUBLISH_TOKEN", "review-publish-token-for-local-tests")
	t.Setenv("HACKME_ORDERS_PREPAID_HUB_SECRET", "")
	t.Setenv("HACKME_CHAIN_MINTING_HUB", "0")
	t.Setenv("HACKME_ADMIN_TOKEN", "review-local-admin-token-for-tests")
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "review.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	signer, err := nodecrypto.LoadOrCreate(filepath.Join(dir, "keys"))
	if err != nil {
		t.Fatal(err)
	}
	a := &app{db: db, chain: chain.New(db), signer: signer}
	if _, _, err := a.chain.InitGenesis(context.Background(), signer.Address()); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestReviewRC20CancelFailureIsReportedToCustomer(t *testing.T) {
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"ok":false,"error":"temporary maintenance"}`))
	}))
	defer hub.Close()
	a := reviewRC20Customer(t, hub.URL)
	ctx := context.Background()
	if err := a.mirrorHubPoolCampaignLocal(ctx, []byte(`{"campaign_id":"review-cancel","hub_pool":true,"pool_distributed":true}`)); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/fuzz/campaigns/review-cancel/status", bytes.NewBufferString(`{"status":"cancelled"}`))
	r.Header.Set("X-Hackme-Admin-Token", "review-local-admin-token-for-tests")
	w := httptest.NewRecorder()
	a.handleFuzzCampaignStatus(w, r, "review-cancel")
	if w.Code < 400 {
		t.Errorf("cancel was not accepted by hub: want error/pending response, got HTTP %d", w.Code)
	}
	c, err := a.getFuzzCampaign(ctx, "review-cancel")
	if err != nil {
		t.Fatal(err)
	}
	if c.Status == "cancelled" {
		t.Errorf("unconfirmed hub cancel is stored locally as cancelled")
	}
}

func TestReviewRC20CreateReturnsAuthoritativeReportURL(t *testing.T) {
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"campaign_id":"review-report","hub_pool":true,"pool_distributed":true,"customer_report_token":"local-test-report-token","report_url":"/api/fuzz/campaigns/review-report/report.html","gate_url":"/api/fuzz/campaigns/review-report/gate","pulse_url":"/api/fuzz/campaigns/review-report/pulse"}`))
	}))
	defer hub.Close()
	a := reviewRC20Customer(t, hub.URL)
	w := httptest.NewRecorder()
	a.proxyOrdersPoolHub(w, context.Background(), "/api/orders/pool/dig", []byte(`{"title":"review"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
	var d map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"report", "gate", "pulse"} {
		key := kind + "_url"
		path := "/api/fuzz/campaigns/review-report/" + kind
		if kind == "report" {
			path += ".html"
		}
		got, _ := d[key].(string)
		parsed, err := url.Parse(got)
		expected, _ := url.Parse(hub.URL + path)
		if err != nil || parsed.Scheme != expected.Scheme || parsed.Host != expected.Host || parsed.Path != expected.Path {
			t.Errorf("%s must identify hub origin; got %v", key, d[key])
		}
	}
}
