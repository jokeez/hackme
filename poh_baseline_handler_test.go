package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"hackme/internal/chain"
	"hackme/internal/store"
)

func TestHandlePohSolveBaselineRequiresAdmin(t *testing.T) {
	// A1
	t.Setenv("HACKME_ADMIN_TOKEN", "baseline-admin-secret")
	a := &app{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/poh/solve-baseline", bytes.NewReader([]byte(`{}`)))
	a.handlePohSolveBaseline(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 without token, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlePohSolveBaselineRejectsBadJSON(t *testing.T) {
	t.Setenv("HACKME_ADMIN_TOKEN", "baseline-admin-secret")
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := chain.New(db)
	if _, _, err := svc.InitGenesis(t.Context(), "HMC-primarywal07"); err != nil {
		t.Fatal(err)
	}
	a := &app{chain: svc}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/poh/solve-baseline", bytes.NewReader([]byte(`not-json`)))
	req.Header.Set("X-Hackme-Admin-Token", "baseline-admin-secret")
	a.handlePohSolveBaseline(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}

func TestHandlePohSolveBaselineOKCreditsPrimary(t *testing.T) {
	t.Setenv("HACKME_ADMIN_TOKEN", "baseline-admin-secret")
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "h2.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := chain.New(db)
	primary := "HMC-primarywal08"
	miner := "HMC-poolworker55"
	if _, _, err := svc.InitGenesis(t.Context(), primary); err != nil {
		t.Fatal(err)
	}
	mod, err := svc.PoHTargetMod(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var tip string
	_ = db.QueryRow(`SELECT value FROM meta WHERE key = 'tip_hash'`).Scan(&tip)
	var maxIdx int64
	_ = db.QueryRow(`SELECT MAX(block_index) FROM blocks`).Scan(&maxIdx)
	next := uint64(maxIdx) + 1
	var nonce uint64
	for n := uint64(0); n < 80_000_000; n++ {
		ev := chain.PohWorkForIndex(next, n, tip, miner)
		if mod > 0 && ev%mod == 0 {
			nonce = n
			break
		}
	}
	if nonce == 0 && mod > 1 {
		t.Fatal("no hit")
	}
	var before uint64
	_ = db.QueryRow(`SELECT balance_units FROM accounts WHERE address = ?`, primary).Scan(&before)

	body, _ := json.Marshal(map[string]any{
		"miner_address": miner,
		"found_nonce":   nonce,
		"target_mod":    uint64(2), // must be ignored / forced to chain mod
	})
	a := &app{chain: svc}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/poh/solve-baseline", bytes.NewReader(body))
	req.Header.Set("X-Hackme-Admin-Token", "baseline-admin-secret")
	a.handlePohSolveBaseline(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out["ok"] != true || out["baseline"] != true {
		t.Fatalf("out=%v", out)
	}
	var after uint64
	_ = db.QueryRow(`SELECT balance_units FROM accounts WHERE address = ?`, primary).Scan(&after)
	if after <= before {
		t.Fatalf("primary balance before=%d after=%d", before, after)
	}
}
