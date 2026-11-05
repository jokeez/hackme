package workersettle

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

type fakeChain struct {
	next  uint64
	slots map[uint64][]byNonceTx
	pool  []map[string]any
}

func (f *fakeChain) server(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/address/", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"next_nonce": f.next})
	})
	mux.HandleFunc("/api/tx/by-nonce", func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.ParseUint(r.URL.Query().Get("nonce"), 10, 64)
		txs := f.slots[n]
		if txs == nil {
			txs = []byNonceTx{}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"txs": txs})
	})
	mux.HandleFunc("/api/tx/pool", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"txs": f.pool})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

const (
	rpPayer = "HMC-payer00000000001"
	rpTo    = "HMC-worker0000000001"
	rpOther = "HMC-worker0000000002"
)

func rpPend(nonce int64, units uint64) pendingSettle {
	return pendingSettle{DeltaUnits: units, PayoutAddress: rpTo, Nonce: nonce}
}

func TestResolveStranded_PaidAtLaterNonce(t *testing.T) {
	f := &fakeChain{next: 103, slots: map[uint64][]byNonceTx{
		100: {{TxHash: "a", Status: "included", To: rpOther, AmountUnits: 5}},
		101: {{TxHash: "mine", Status: "included", To: rpTo, AmountUnits: 777}},
		102: {{TxHash: "c", Status: "included", To: rpOther, AmountUnits: 777}},
	}}
	srv := f.server(t)
	res := resolveStrandedPending(context.Background(), srv.Client(), srv.URL, rpPayer, rpTo, rpPend(100, 777))
	if res.Outcome != strandedPaid || res.TxHash != "mine" || res.Nonce != 101 {
		t.Fatalf("got %+v", res)
	}
}

func TestResolveStranded_UnsentWhenEverySlotTakenByOthers(t *testing.T) {
	f := &fakeChain{next: 102, slots: map[uint64][]byNonceTx{
		100: {{TxHash: "a", Status: "included", To: rpOther, AmountUnits: 777}},
		// same recipient, different amount: not this intent
		101: {{TxHash: "b", Status: "included", To: rpTo, AmountUnits: 778}},
	}}
	srv := f.server(t)
	res := resolveStrandedPending(context.Background(), srv.Client(), srv.URL, rpPayer, rpTo, rpPend(100, 777))
	if res.Outcome != strandedUnsent {
		t.Fatalf("got %+v", res)
	}
}

func TestResolveStranded_UnsentWhenNonceNeverMoved(t *testing.T) {
	f := &fakeChain{next: 100}
	srv := f.server(t)
	res := resolveStrandedPending(context.Background(), srv.Client(), srv.URL, rpPayer, rpTo, rpPend(100, 777))
	if res.Outcome != strandedUnsent {
		t.Fatalf("got %+v", res)
	}
}

func TestResolveStranded_KeepWhenMatchingTxInMempool(t *testing.T) {
	f := &fakeChain{next: 100, pool: []map[string]any{{"from": rpPayer, "to": rpTo, "amount_units": 777}}}
	srv := f.server(t)
	res := resolveStrandedPending(context.Background(), srv.Client(), srv.URL, rpPayer, rpTo, rpPend(100, 777))
	if res.Outcome != strandedKeep {
		t.Fatalf("mempool match must keep, got %+v", res)
	}
}

func TestResolveStranded_KeepWhenSlotEmptyOrRejectedOnly(t *testing.T) {
	for name, slot := range map[string][]byNonceTx{
		"empty":    nil,
		"rejected": {{TxHash: "r", Status: "rejected", To: rpTo, AmountUnits: 777}},
	} {
		f := &fakeChain{next: 101, slots: map[uint64][]byNonceTx{100: slot}}
		srv := f.server(t)
		res := resolveStrandedPending(context.Background(), srv.Client(), srv.URL, rpPayer, rpTo, rpPend(100, 777))
		if res.Outcome != strandedKeep {
			t.Fatalf("%s: got %+v", name, res)
		}
	}
}

func TestResolveStranded_KeepWhenMatchingStillPendingOrRangeTooWide(t *testing.T) {
	f := &fakeChain{next: 101, slots: map[uint64][]byNonceTx{100: {{TxHash: "p", Status: "pending", To: rpTo, AmountUnits: 777}}}}
	srv := f.server(t)
	if res := resolveStrandedPending(context.Background(), srv.Client(), srv.URL, rpPayer, rpTo, rpPend(100, 777)); res.Outcome != strandedKeep {
		t.Fatalf("pending match: %+v", res)
	}
	f2 := &fakeChain{next: 100 + strandedMaxScan + 1}
	srv2 := f2.server(t)
	if res := resolveStrandedPending(context.Background(), srv2.Client(), srv2.URL, rpPayer, rpTo, rpPend(100, 777)); res.Outcome != strandedKeep {
		t.Fatalf("wide range: %+v", res)
	}
	// legacy intent without a recorded nonce is never auto-resolved
	if res := resolveStrandedPending(context.Background(), srv.Client(), srv.URL, rpPayer, rpTo, rpPend(0, 777)); res.Outcome != strandedKeep {
		t.Fatalf("no nonce: %+v", res)
	}
}
