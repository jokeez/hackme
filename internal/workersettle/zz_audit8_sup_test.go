package workersettle

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
)

// A3 regression: lost /api/sup/tx/send reply while the tx sits in mempool must NOT clear the
// intent (findSUPInflight attaches the hash). Clearing + re-send after include = double-pay.
func TestAudit8SUPLostResponseDoublePays(t *testing.T) {
	const payer = "HMC-payer00000000001"
	const to = "HMC-worker0000000001"
	var mu sync.Mutex
	var nonce uint64 = 7
	var mempool []uint64 // amounts accepted but not included
	var included uint64  // total SUP units delivered to worker
	sends := 0

	mux := http.NewServeMux()
	mux.HandleFunc("/api/sup/economics", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"economics": map[string]any{"admin_mint_retired": true}})
	})
	mux.HandleFunc("/api/wallet", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"address": payer})
	})
	mux.HandleFunc("/api/address/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"balance_sup_units": uint64(100_00000000), "sup_next_nonce": nonce})
	})
	mux.HandleFunc("/api/work/stats", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"workers": map[string]any{
			"w1": map[string]any{"payout_sup": 1.0, "payout_address": to},
		}})
	})
	mux.HandleFunc("/api/sup/tx/send", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Amount uint64 `json:"amount_units"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		sends++
		first := sends == 1
		mempool = append(mempool, body.Amount)
		mu.Unlock()
		if first {
			// Accepted into the mempool, but the response is lost (proxy reset / timeout).
			hj, _ := w.(http.Hijacker)
			c, _, _ := hj.Hijack()
			_ = c.Close()
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "tx_hash": "sup-tx-2", "status": "pending"})
	})
	mux.HandleFunc("/api/sup/tx/pool", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		txs := []map[string]any{}
		for i, a := range mempool {
			txs = append(txs, map[string]any{"tx_hash": fmt.Sprintf("sup-tx-%d", i+1), "from": payer, "to": to, "amount_units": a})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"txs": txs})
	})
	mux.HandleFunc("/api/sup/tx/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "included"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	t.Setenv("HACKME_SETTLE_PAYER_LOCK_DIR", dir)
	cfg := Config{
		CoordURL: srv.URL, ChainBase: srv.URL, AdminToken: "a", CoordAdminToken: "c",
		StateJSON: filepath.Join(dir, "state.json"), StateDB: filepath.Join(dir, "state.db"),
		TxWaitSec: 1, HTTPTimeoutSec: 5, Log: log.New(io.Discard, "", 0),
	}
	if _, err := RunSUP(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	// Block lands between runs: the mempool tx is included, nonce advances.
	mu.Lock()
	for _, a := range mempool {
		included += a
		nonce++
	}
	mempool = nil
	mu.Unlock()
	if _, err := RunSUP(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	for _, a := range mempool {
		included += a
	}
	mu.Unlock()
	if included > HMCToUnits(1.0) {
		t.Errorf("BUG: worker accrued 1 SUP but received %d units over %d sends", included, sends)
	}
}
