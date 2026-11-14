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

// Empty/lost send response + unchanged nonce + empty mempool must keep the SUP
// intent. Clearing and re-sending after the first transfer includes = double-pay.
func TestSUPEmptyResponseUnchangedNonceKeepsPending(t *testing.T) {
	const payer = "HMC-payer00000000001"
	const to = "HMC-worker0000000001"
	var mu sync.Mutex
	var nonce uint64 = 3
	var accepted []uint64 // amounts the stub node accepted
	var included uint64
	sends := 0
	landFirst := false

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
			"w1": map[string]any{"payout_sup": 0.5, "payout_address": to},
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
		n := sends
		accepted = append(accepted, body.Amount)
		mu.Unlock()
		if n == 1 {
			// Node accepted; HTTP body empty / connection drop before JSON.
			w.WriteHeader(http.StatusOK)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "tx_hash": fmt.Sprintf("sup-tx-%d", n), "status": "pending"})
	})
	mux.HandleFunc("/api/sup/tx/pool", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		// Ambiguity window: accepted but not yet visible in the mempool lookup.
		_ = json.NewEncoder(w).Encode(map[string]any{"txs": []any{}})
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
	mu.Lock()
	if sends != 1 {
		t.Fatalf("first run sends=%d want 1", sends)
	}
	// Between ticks: first transfer lands, nonce advances, accrual unchanged.
	if len(accepted) > 0 {
		included += accepted[0]
		nonce++
		landFirst = true
	}
	accepted = nil
	mu.Unlock()
	if !landFirst {
		t.Fatal("stub did not accept first transfer")
	}
	if _, err := RunSUP(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, a := range accepted {
		included += a
	}
	if sends > 1 {
		t.Fatalf("BUG: second send after empty response+empty mempool (sends=%d included=%d)", sends, included)
	}
	if included != HMCToUnits(0.5) {
		t.Fatalf("want single 0.5 SUP pay, got included=%d units over %d sends", included, sends)
	}
}
