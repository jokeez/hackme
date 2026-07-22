package poolfuzz

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"hackme/internal/store"
)

func TestRelaySettlerEnqueueFirstNoDoubleEnqueueOnTimeout(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: "relay-camp", CampaignType: "property", Status: "running", BudgetRuns: 2,
		Config: map[string]any{"orders_settle_base": "http://127.0.0.1:9", "orders_settle_pull": false},
	}); err != nil {
		t.Fatal(err)
	}
	// Force non-loopback DefaultOrdersURL so relay attempts HTTP (then fails → leave pending).
	// Campaign orders_settle_base is intentionally ignored (SSRF hardening).
	_, _ = db.ExecContext(ctx, `UPDATE fuzz_campaigns SET config_json=? WHERE id=?`,
		`{"pool_distributed":true,"orders_settle_pull":false}`, "relay-camp")

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		if body["event_id"] == nil || body["event_id"] == "" {
			t.Errorf("missing event_id in settle body: %s", raw)
		}
		// Simulate slow origin that already paid: hang until client times out.
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	relay := &RelaySettler{
		Service:          svc,
		DefaultOrdersURL: server.URL,
		AdminToken:       func() string { return "tok" },
		HTTPClient:       &http.Client{Timeout: 50 * time.Millisecond},
	}
	if _, err := relay.PayRun(ctx, "relay-camp", "HMC-2222222222222222", 0); err != nil {
		t.Fatal(err)
	}
	// Timeout leaves exactly one pending outbox row (no second enqueue).
	items, err := svc.ListPendingSettleOutbox(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("pending outbox=%d want 1 (no double enqueue)", len(items))
	}
	if hits.Load() < 1 {
		t.Fatal("expected at least one HTTP attempt")
	}
	// Retry same outbox id — must not enqueue a second row.
	if _, err := relay.PayRun(ctx, "relay-camp", "HMC-2222222222222222", items[0].ID); err != nil {
		t.Fatal(err)
	}
	items2, err := svc.ListPendingSettleOutbox(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items2) != 1 || items2[0].ID != items[0].ID {
		t.Fatalf("reuse must keep same outbox id, got %+v want id=%d", items2, items[0].ID)
	}
	// Second PayRun for another work item enqueues a second distinct event.
	if _, err := relay.PayRun(ctx, "relay-camp", "HMC-3333333333333333", 0); err != nil {
		t.Fatal(err)
	}
	items, err = svc.ListPendingSettleOutbox(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("pending outbox=%d want 2", len(items))
	}
	if items[0].ID == items[1].ID {
		t.Fatal("outbox ids must differ")
	}
}

func TestRelaySettlerAckOnHTTPSuccess(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "relay-ok.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: "ok-camp", CampaignType: "property", Status: "running", BudgetRuns: 1,
		Config: map[string]any{},
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	_, _ = db.ExecContext(ctx, `UPDATE fuzz_campaigns SET config_json=? WHERE id=?`,
		`{"pool_distributed":true,"orders_settle_pull":false}`, "ok-camp")

	relay := &RelaySettler{
		Service:          svc,
		DefaultOrdersURL: server.URL,
		AdminToken:       func() string { return "tok" },
		HTTPClient:       &http.Client{Timeout: 5 * time.Second},
	}
	res, err := relay.PayRun(ctx, "ok-camp", "HMC-2222222222222222", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Applied {
		t.Fatal("successful relay must report Applied")
	}
	items, err := svc.ListPendingSettleOutbox(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("successful relay must ACK outbox, pending=%d", len(items))
	}
}

func TestRelaySettlerQueuedNotAppliedOnPull(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "relay-pull.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: "pull-camp", CampaignType: "property", Status: "running", BudgetRuns: 1,
		Config: map[string]any{"orders_settle_pull": true},
	}); err != nil {
		t.Fatal(err)
	}
	relay := &RelaySettler{Service: svc, AdminToken: func() string { return "tok" }}
	res, err := relay.PayRun(ctx, "pull-camp", "HMC-2222222222222222", 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied || res.OutboxID <= 0 {
		t.Fatalf("pull mode must queue only: %+v", res)
	}
	items, err := svc.ListPendingSettleOutbox(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("pending outbox=%d want 1", len(items))
	}
}
