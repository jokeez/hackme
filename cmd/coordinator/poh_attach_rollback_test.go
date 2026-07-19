package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"hackme/internal/poolfuzz"
	"hackme/internal/store"
)

func TestPoHAttachFailureRollsBackCampaign(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "poh-rb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	pf := &poolfuzz.Service{DB: db}
	wm := &workManager{
		ordersProbeURL: "", // forces attach failure (orders_url_missing, skipped=false)
	}
	mux := http.NewServeMux()
	addFuzzPoolRoutes(mux, "admintok", "", false, wm, pf)

	body := `{"id":"poh-fail-1","campaign_type":"property","title":"t","status":"running","budget_runs":2,"config":{"attach_poh_order":true,"wasm_check_hex":"00"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/campaigns", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", "admintok")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusPaymentRequired {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["rolled_back"] != true {
		t.Fatalf("expected rolled_back: %v", resp)
	}
	ctx := context.Background()
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM fuzz_campaigns WHERE id=?`, "poh-fail-1").Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" {
		t.Fatalf("campaign status=%q want cancelled", status)
	}
	var pending int
	_ = db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM fuzz_work_items WHERE campaign_id=? AND status IN ('pending','leased')`, "poh-fail-1").Scan(&pending)
	if pending != 0 {
		t.Fatalf("pending/leased work after rollback=%d", pending)
	}
}
