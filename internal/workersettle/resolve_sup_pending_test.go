package workersettle

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveStrandedSUPPendingRequiresMatchingTransfer(t *testing.T) {
	// Nonce advanced by an unrelated SUP payment must NOT count as paid.
	mux := http.NewServeMux()
	mux.HandleFunc("/api/address/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "address": "HMC-payer", "balance_sup_units": 1_000_00000,
			"sup_next_nonce": uint64(4),
		})
	})
	mux.HandleFunc("/api/sup/tx/by-nonce", func(w http.ResponseWriter, r *http.Request) {
		nonce := r.URL.Query().Get("nonce")
		txs := []map[string]any{}
		// Intent nonce=3 amount=5000000 to payout — slot taken by different amount/to.
		if nonce == "3" {
			txs = append(txs, map[string]any{
				"tx_hash": "unrelated-sup", "status": "included",
				"to": "HMC-other", "amount_units": uint64(999),
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"txs": txs})
	})
	mux.HandleFunc("/api/sup/tx/pool", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"txs": []any{}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	pend := pendingSettle{DeltaUnits: 5000000, PayoutAddress: "HMC-pay", Nonce: 3}
	res := resolveStrandedSUPPending(context.Background(), srv.Client(), srv.URL, "HMC-payer", "HMC-pay", pend)
	if res.Outcome != strandedUnsent {
		t.Fatalf("unrelated nonce burn must be unsent (clear+retry), got %+v", res)
	}
}

func TestResolveStrandedSUPPendingMatchingIncluded(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/address/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "sup_next_nonce": uint64(4),
		})
	})
	mux.HandleFunc("/api/sup/tx/by-nonce", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"txs": []map[string]any{{
			"tx_hash": "sup-paid-1", "status": "included",
			"to": "HMC-pay", "amount_units": uint64(5000000),
		}}})
	})
	mux.HandleFunc("/api/sup/tx/pool", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"txs": []any{}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	pend := pendingSettle{DeltaUnits: 5000000, PayoutAddress: "HMC-pay", Nonce: 3}
	res := resolveStrandedSUPPending(context.Background(), srv.Client(), srv.URL, "HMC-payer", "HMC-pay", pend)
	if res.Outcome != strandedPaid || res.TxHash != "sup-paid-1" {
		t.Fatalf("matching included must be paid, got %+v", res)
	}
}
