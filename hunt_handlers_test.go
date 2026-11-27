package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"hackme/internal/chain"
	"hackme/internal/fuzzescrow"
	"hackme/internal/store"
)

func TestHuntPackagesPublic(t *testing.T) {
	a, _ := newWalletTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/api/hunt/packages", nil)
	rec := httptest.NewRecorder()
	a.handleHuntAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	pkgs, _ := resp["packages"].([]any)
	if len(pkgs) < 2 {
		t.Fatalf("packages=%v", pkgs)
	}
}

func TestHuntInventoryRequiresAdmin(t *testing.T) {
	a, _ := newWalletTestApp(t)
	body := []byte(`{"path":"."}`)
	req := httptest.NewRequest(http.MethodPost, "/api/hunt/inventory", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	a.handleHuntAPI(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestHuntCampaignCreate5050Escrow(t *testing.T) {
	a, db := newWalletTestApp(t)
	t.Setenv("HACKME_ADMIN_TOKEN", "hunt-admin-test")
	t.Setenv("HACKME_REPO_ROOT", a.repoRoot())
	ctx := context.Background()
	addr, _, err := a.chain.Wallet(ctx)
	if err != nil {
		t.Fatal(err)
	}
	credit := uint64(50 * chain.UnitsPerHMC)
	if _, err := db.ExecContext(ctx, `UPDATE wallet SET balance_units=?`, credit); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE accounts SET balance_units=? WHERE address=?`, credit, addr); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{
		"package":   "hunt_lite",
		"target_id": "jsmn",
		"catalog":   true,
		"title":     "Hunt test jsmn",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/hunt/campaigns", bytes.NewReader(body))
	req.Header.Set("X-Hackme-Admin-Token", "hunt-admin-test")
	rec := httptest.NewRecorder()
	a.handleHuntAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["escrow_split"] != fuzzescrow.EscrowSplit5050 {
		t.Fatalf("split=%v", resp["escrow_split"])
	}
	esc, _ := resp["escrow"].(map[string]any)
	if esc == nil {
		t.Fatal("missing escrow")
	}
	runsPool, _ := esc["runs_pool_hmc"].(float64)
	bountyPool, _ := esc["bounty_pool_hmc"].(float64)
	if runsPool < 9.9 || runsPool > 10.1 || bountyPool < 9.9 || bountyPool > 10.1 {
		t.Fatalf("not 50/50: runs=%v bounty=%v", runsPool, bountyPool)
	}
	if resp["report_url"] == nil || resp["gate_url"] == nil || resp["pulse_url"] == nil {
		t.Fatalf("missing deliverable urls: %v", resp)
	}
	if !strings.Contains(toString(resp["report_url"]), "/report.html") {
		t.Fatalf("report_url=%v", resp["report_url"])
	}
}

func TestHuntCreateAbortRefundsEscrow(t *testing.T) {
	a, db := newWalletTestApp(t)
	t.Setenv("HACKME_ADMIN_TOKEN", "hunt-admin-test")
	t.Setenv("HACKME_REPO_ROOT", a.repoRoot())
	ctx := context.Background()
	addr, _, err := a.chain.Wallet(ctx)
	if err != nil {
		t.Fatal(err)
	}
	credit := uint64(50 * chain.UnitsPerHMC)
	if _, err := db.ExecContext(ctx, `UPDATE wallet SET balance_units=?`, credit); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE accounts SET balance_units=? WHERE address=?`, credit, addr); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"package":   "hunt_lite",
		"target_id": "jsmn",
		"catalog":   true,
		"title":     "Hunt abort jsmn",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/hunt/campaigns", bytes.NewReader(body))
	req.Header.Set("X-Hackme-Admin-Token", "hunt-admin-test")
	rec := httptest.NewRecorder()
	a.handleHuntAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	camp, _ := resp["campaign"].(map[string]any)
	id, _ := camp["id"].(string)
	if id == "" {
		t.Fatal("missing campaign id")
	}
	a.rollbackNewCampaignEscrow(ctx, id)
	var bal uint64
	if err := db.QueryRowContext(ctx, `SELECT balance_units FROM wallet WHERE id=1`).Scan(&bal); err != nil {
		t.Fatal(err)
	}
	if bal != credit {
		t.Fatalf("wallet after abort=%d want %d", bal, credit)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_campaigns WHERE id=?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("campaign row still present")
	}
	row, err := a.chain.GetFuzzEscrow(ctx, id)
	if err == nil && row != nil && row.Status != "closed" {
		t.Fatalf("escrow status=%s", row.Status)
	}
}

func TestRollbackKeepsCampaignWhenEscrowUndoFails(t *testing.T) {
	a, db := newWalletTestApp(t)
	t.Setenv("HACKME_ADMIN_TOKEN", "hunt-admin-test")
	t.Setenv("HACKME_REPO_ROOT", a.repoRoot())
	ctx := context.Background()
	addr, _, err := a.chain.Wallet(ctx)
	if err != nil {
		t.Fatal(err)
	}
	credit := uint64(50 * chain.UnitsPerHMC)
	if _, err := db.ExecContext(ctx, `UPDATE wallet SET balance_units=?`, credit); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE accounts SET balance_units=? WHERE address=?`, credit, addr); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"package":   "hunt_lite",
		"target_id": "jsmn",
		"catalog":   true,
		"title":     "Hunt rollback keep",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/hunt/campaigns", bytes.NewReader(body))
	req.Header.Set("X-Hackme-Admin-Token", "hunt-admin-test")
	rec := httptest.NewRecorder()
	a.handleHuntAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	camp, _ := resp["campaign"].(map[string]any)
	id, _ := camp["id"].(string)
	if id == "" {
		t.Fatal("missing campaign id")
	}
	live := a.chain
	bad, err := store.Open(filepath.Join(t.TempDir(), "closed.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := bad.Close(); err != nil {
		t.Fatal(err)
	}
	a.chain = chain.New(bad)
	a.rollbackNewCampaignEscrow(ctx, id)
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM fuzz_campaigns WHERE id=?`, id).Scan(&status); err != nil {
		t.Fatalf("campaign row deleted while escrow undo failed: %v", err)
	}
	if status != "rollback_failed" {
		t.Fatalf("status=%s", status)
	}
	row, err := live.GetFuzzEscrow(ctx, id)
	if err != nil || row == nil || row.Status != "open" {
		t.Fatalf("escrow after failed undo: err=%v row=%+v", err, row)
	}
	a.chain = live
	a.retryFailedCampaignRollbacks(ctx)
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_campaigns WHERE id=?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("retry left campaign row status still set, count=%d", n)
	}
	var bal uint64
	if err := db.QueryRowContext(ctx, `SELECT balance_units FROM wallet WHERE id=1`).Scan(&bal); err != nil {
		t.Fatal(err)
	}
	if bal != credit {
		t.Fatalf("wallet after retry=%d want %d", bal, credit)
	}
}

func TestAllowedCampaignTypeHunt(t *testing.T) {
	if !allowedCampaignType("hunt") {
		t.Fatal("hunt must be allowed")
	}
}
