package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"hackme/internal/chain"
	"hackme/internal/store"
)

func TestTransferByNonceAPI(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "bynonce.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	from := "HMC-1111111111111111"
	if _, err := db.ExecContext(ctx, `INSERT INTO tx_history (tx_hash, tx_json, from_address, to_address, nonce, fee_units, amount_units, status, applied_at)
		VALUES ('h5', '{}', ?, 'HMC-2222222222222222', 5, 1, 42, 'included', 1)`, from); err != nil {
		t.Fatal(err)
	}
	a := &app{chain: chain.New(db)}
	call := func(target string) (*httptest.ResponseRecorder, map[string]any) {
		rec := httptest.NewRecorder()
		a.handleTransferByNonce(rec, httptest.NewRequest(http.MethodGet, target, nil))
		var m map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &m)
		return rec, m
	}
	rec, m := call("/api/tx/by-nonce?from=" + from + "&nonce=5")
	txs, _ := m["txs"].([]any)
	if rec.Code != http.StatusOK || len(txs) != 1 {
		t.Fatalf("hit: %d %s", rec.Code, rec.Body.String())
	}
	rec, m = call("/api/tx/by-nonce?from=" + from + "&nonce=6")
	if txs, ok := m["txs"].([]any); rec.Code != http.StatusOK || !ok || len(txs) != 0 {
		t.Fatalf("miss must be an empty list: %d %s", rec.Code, rec.Body.String())
	}
	for _, bad := range []string{"?from=x&nonce=5", "?from=" + from + "&nonce=-1", "?from=" + from} {
		if rec, _ := call("/api/tx/by-nonce" + bad); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: want 400, got %d", bad, rec.Code)
		}
	}
	// SQLite cannot bind uint64 with high bit (same class as from_height / reports index).
	rec, _ = call("/api/tx/by-nonce?from=" + from + "&nonce=9223372036854775808")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("nonce 2^63: want 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
