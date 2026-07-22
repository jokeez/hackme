package main

import (
	"bytes"
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"hackme/internal/chain"
	"hackme/internal/store"
)

func TestHandleFuzzPoolSettleEmptyEventIDRejected(t *testing.T) {
	t.Setenv("HACKME_ADMIN_TOKEN", "")
	a, db := newFuzzSettleTestApp(t)
	ctx := context.Background()
	miner := "HMC-9876543210987654"
	fundFuzzSettleEscrow(t, ctx, db, a.chain, "evt-empty", miner)

	req := httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/settle",
		bytes.NewBufferString(`{"kind":"run","campaign_id":"evt-empty","miner_address":"`+miner+`"}`))
	rec := httptest.NewRecorder()
	a.handleFuzzPoolSettle(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty event_id: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("event_id")) {
		t.Fatalf("expected event_id error body, got %s", rec.Body.String())
	}
}

func TestHandleFuzzPoolSettleReplaySameEventIDNoDoublePay(t *testing.T) {
	t.Setenv("HACKME_ADMIN_TOKEN", "")
	a, db := newFuzzSettleTestApp(t)
	ctx := context.Background()
	miner := "HMC-9876543210987654"
	fundFuzzSettleEscrow(t, ctx, db, a.chain, "evt-replay", miner)

	raw := []byte(`{"kind":"run","campaign_id":"evt-replay","miner_address":"` + miner + `","event_id":"outbox:evt-replay:42"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/settle", bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	a.handleFuzzPoolSettle(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first settle: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var bal1 uint64
	if err := db.QueryRowContext(ctx, `SELECT balance_units FROM accounts WHERE address=?`, miner).Scan(&bal1); err != nil {
		t.Fatal(err)
	}
	if bal1 == 0 {
		t.Fatal("expected miner credited on first settle")
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/settle", bytes.NewReader(raw))
	rec2 := httptest.NewRecorder()
	a.handleFuzzPoolSettle(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("replay settle: status=%d body=%s", rec2.Code, rec2.Body.String())
	}
	var bal2 uint64
	if err := db.QueryRowContext(ctx, `SELECT balance_units FROM accounts WHERE address=?`, miner).Scan(&bal2); err != nil {
		t.Fatal(err)
	}
	if bal2 != bal1 {
		t.Fatalf("replay double-paid: bal1=%d bal2=%d", bal1, bal2)
	}
}

func newFuzzSettleTestApp(t *testing.T) (*app, *sql.DB) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "settle-http.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := chain.New(db)
	return &app{chain: svc, db: db}, db
}

func fundFuzzSettleEscrow(t *testing.T, ctx context.Context, db *sql.DB, svc *chain.Service, campaignID, miner string) {
	t.Helper()
	payer := "HMC-1234567890123456"
	if _, _, err := svc.InitGenesis(ctx, payer); err != nil {
		t.Fatal(err)
	}
	units := chain.HMCToUnits(20)
	if _, err := db.ExecContext(ctx, `UPDATE wallet SET balance_hmc=?, balance_units=? WHERE id=1`, 20.0, units); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE accounts SET balance_units=? WHERE address=?`, units, payer); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO accounts(address, balance_units, next_nonce, updated_at) VALUES(?,0,0,0)`, miner); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.OpenFuzzEscrow(ctx, campaignID, 10.0, 100); err != nil {
		t.Fatal(err)
	}
}
