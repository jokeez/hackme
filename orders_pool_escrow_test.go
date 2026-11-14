package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hackme/internal/chain"
	"hackme/internal/nodecrypto"
)

var poolEscrowTestIP atomic.Int64

type poolEscrowFixture struct {
	t     *testing.T
	a     *app
	owner *nodecrypto.Signer
	order []byte
}

func newPoolEscrowFixture(t *testing.T) *poolEscrowFixture {
	t.Helper()
	t.Setenv("HACKME_ORDERS_POOL_ESCROW", "1")
	a, _ := newWalletTestApp(t)
	owner, err := nodecrypto.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	order, _ := json.Marshal(map[string]any{"guard_pack": "secrets", "dig_package": "audit"})
	return &poolEscrowFixture{t: t, a: a, owner: owner, order: order}
}

// putTransfer records a transfer in the hub's tx history as the chain would after inclusion.
func (f *poolEscrowFixture) putTransfer(hash, status, from, to string, amountHMC float64, memo string) {
	f.t.Helper()
	txJSON, _ := json.Marshal(map[string]any{"memo": memo})
	_, err := f.a.db.Exec(`INSERT INTO tx_history (tx_hash, tx_json, from_address, to_address, nonce, fee_units, amount_units, status, block_index, applied_at)
		VALUES (?,?,?,?,1,0,?,?,5,?)`, hash, string(txJSON), from, to, chain.HMCToUnits(amountHMC), status, time.Now().Unix())
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *poolEscrowFixture) post(campaign, txHash string, signer *nodecrypto.Signer, mutate func(map[string]any)) *httptest.ResponseRecorder {
	f.t.Helper()
	ts := time.Now().Unix()
	owner := f.owner.Address()
	sig := signer.SignHex(ordersPoolEscrowSigningBytes(campaign, txHash, owner, ts, f.order))
	body := map[string]any{
		"campaign_id":          campaign,
		"owner_address":        owner,
		"owner_pubkey_ed25519": signer.PublicKeyHex(),
		"funding_tx_hash":      txHash,
		"timestamp_unix":       ts,
		"sig_ed25519":          sig,
		"order":                json.RawMessage(f.order),
	}
	if mutate != nil {
		mutate(body)
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/orders/pool-escrow", bytes.NewReader(raw))
	req.RemoteAddr = fmt.Sprintf("10.9.%d.%d:4000", poolEscrowTestIP.Add(1)/250, poolEscrowTestIP.Load()%250+1)
	rec := httptest.NewRecorder()
	f.a.handleOrdersPoolEscrow(rec, req)
	return rec
}

func wantAPIError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status || !strings.Contains(rec.Body.String(), code) {
		t.Fatalf("want %d %s, got %d %s", status, code, rec.Code, rec.Body.String())
	}
}

func poolEscrowHash(n int) string { return fmt.Sprintf("%064x", n) }

func TestOrdersPoolEscrowDisabledOffHub(t *testing.T) {
	f := newPoolEscrowFixture(t)
	t.Setenv("HACKME_ORDERS_POOL_ESCROW", "0")
	rec := f.post("campaign-pool-x", poolEscrowHash(1), f.owner, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404 when disabled, got %d", rec.Code)
	}
}

func TestOrdersPoolEscrowRejectsBadClaims(t *testing.T) {
	f := newPoolEscrowFixture(t)
	hub := f.a.signer.Address()
	me := f.owner.Address()
	camp := "campaign-pool-t1"
	memo := ordersPoolEscrowMemoPrefix + camp
	stranger, _ := nodecrypto.LoadOrCreate(t.TempDir())

	// Signature by a key that does not own owner_address.
	wantAPIError(t, f.post(camp, poolEscrowHash(1), stranger, nil), http.StatusUnauthorized, "bad_signature")
	// Signed body tampered after signing (order swapped for a bigger package).
	wantAPIError(t, f.post(camp, poolEscrowHash(1), f.owner, func(b map[string]any) {
		b["order"] = json.RawMessage(`{"guard_pack":"secrets","dig_package":"deep"}`)
	}), http.StatusUnauthorized, "bad_signature")
	// Stale timestamp.
	wantAPIError(t, f.post(camp, poolEscrowHash(1), f.owner, func(b map[string]any) {
		b["timestamp_unix"] = time.Now().Unix() - 3600
	}), http.StatusBadRequest, "stale_request")
	// Unknown funding tx.
	wantAPIError(t, f.post(camp, poolEscrowHash(1), f.owner, nil), http.StatusNotFound, "funding_not_found")

	f.putTransfer(poolEscrowHash(2), "pending", me, hub, 10, memo)
	wantAPIError(t, f.post(camp, poolEscrowHash(2), f.owner, nil), http.StatusConflict, "funding_not_included")

	f.putTransfer(poolEscrowHash(3), "included", me, chain.DevFeeAddress, 10, memo)
	wantAPIError(t, f.post(camp, poolEscrowHash(3), f.owner, nil), http.StatusBadRequest, "funding_wrong_recipient")

	f.putTransfer(poolEscrowHash(4), "included", stranger.Address(), hub, 10, memo)
	wantAPIError(t, f.post(camp, poolEscrowHash(4), f.owner, nil), http.StatusBadRequest, "funding_wrong_sender")

	// A transfer paid for another campaign cannot fund this one.
	f.putTransfer(poolEscrowHash(5), "included", me, hub, 10, ordersPoolEscrowMemoPrefix+"campaign-pool-other")
	wantAPIError(t, f.post(camp, poolEscrowHash(5), f.owner, nil), http.StatusBadRequest, "funding_wrong_memo")

	f.putTransfer(poolEscrowHash(6), "included", me, hub, 0.5, memo)
	wantAPIError(t, f.post(camp, poolEscrowHash(6), f.owner, nil), http.StatusPaymentRequired, "funding_too_small")

	// None of the rejected claims may leave a funding record behind.
	var n int
	_ = f.a.db.QueryRow(`SELECT COUNT(*) FROM orders_pool_escrow`).Scan(&n)
	if n != 0 {
		t.Fatalf("rejected claims recorded %d funding rows", n)
	}
}

func TestOrdersPoolEscrowIdempotentAndNoReuse(t *testing.T) {
	f := newPoolEscrowFixture(t)
	ctx := context.Background()
	if err := f.a.ensureOrdersPoolEscrowTable(ctx); err != nil {
		t.Fatal(err)
	}
	me := f.owner.Address()
	camp := "campaign-pool-t2"
	tx := poolEscrowHash(7)
	if _, err := f.a.db.Exec(`INSERT INTO orders_pool_escrow (campaign_id, funding_tx, owner, funded_units, created_at) VALUES (?,?,?,?,?)`,
		camp, tx, me, chain.HMCToUnits(5), time.Now().Unix()); err != nil {
		t.Fatal(err)
	}

	// Funding recorded but no campaign (create failed / unclaimed sweep): never "ok".
	wantAPIError(t, f.post(camp, tx, f.owner, nil), http.StatusConflict, "funding_refunded")
	if _, err := f.a.db.Exec(`INSERT INTO fuzz_campaigns (id, campaign_type, status, title, created_at) VALUES (?, 'property', 'running', 't', ?)`, camp, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	// Same claim again (customer retry after a timeout): same answer, same owner secret.
	rec := f.post(camp, tx, f.owner, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("idempotent retry: %d %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out["idempotent"] != true || out["orders_owner_secret"] != f.a.ordersPoolOwnerSecret(camp, me) || out["orders_owner_secret"] == "" {
		t.Fatalf("idempotent body: %v", out)
	}
	// The same funding tx cannot open a second campaign …
	wantAPIError(t, f.post("campaign-pool-t3", tx, f.owner, nil), http.StatusConflict, "funding_used")
	// … and the campaign id cannot be re-bound to another funding tx.
	wantAPIError(t, f.post(camp, poolEscrowHash(8), f.owner, nil), http.StatusConflict, "funding_used")
}

func TestOrdersPoolOwnerSecretBoundToCampaignAndOwner(t *testing.T) {
	f := newPoolEscrowFixture(t)
	s1 := f.a.ordersPoolOwnerSecret("campaign-a", "HMC-0000000000000001")
	if len(s1) != 64 || s1 != f.a.ordersPoolOwnerSecret("campaign-a", "HMC-0000000000000001") {
		t.Fatalf("secret not deterministic: %q", s1)
	}
	if s1 == f.a.ordersPoolOwnerSecret("campaign-b", "HMC-0000000000000001") ||
		s1 == f.a.ordersPoolOwnerSecret("campaign-a", "HMC-0000000000000002") {
		t.Fatal("secret must differ per campaign and per owner")
	}
}

func TestOrdersPoolRefundQueueAccumulates(t *testing.T) {
	f := newPoolEscrowFixture(t)
	ctx := context.Background()
	if err := f.a.ensureOrdersPoolEscrowTable(ctx); err != nil {
		t.Fatal(err)
	}
	camp := "campaign-pool-t4"
	_, _ = f.a.db.Exec(`INSERT INTO orders_pool_escrow (campaign_id, funding_tx, owner, funded_units, created_at) VALUES (?,?,?,?,?)`,
		camp, poolEscrowHash(9), f.owner.Address(), chain.HMCToUnits(6), time.Now().Unix())
	f.a.queueOrdersPoolRefund(ctx, camp, chain.HMCToUnits(1), "overpayment")
	f.a.queueOrdersPoolRefund(ctx, camp, chain.HMCToUnits(2), "campaign closed")
	f.a.queueOrdersPoolRefund(ctx, "campaign-not-funded", chain.HMCToUnits(50), "x")
	var units uint64
	var st string
	_ = f.a.db.QueryRow(`SELECT refund_units, refund_status FROM orders_pool_escrow WHERE campaign_id=?`, camp).Scan(&units, &st)
	if units != chain.HMCToUnits(3) || st != "pending" {
		t.Fatalf("refund=%d status=%s", units, st)
	}
	var n int
	_ = f.a.db.QueryRow(`SELECT COUNT(*) FROM orders_pool_escrow`).Scan(&n)
	if n != 1 {
		t.Fatal("refund for an unfunded campaign must not create a row")
	}
}

func poolRefundRow(t *testing.T, a *app, camp string) (units uint64, status, tx, memo string) {
	t.Helper()
	if err := a.db.QueryRow(`SELECT refund_units, refund_status, refund_tx, refund_memo FROM orders_pool_escrow WHERE campaign_id=?`, camp).
		Scan(&units, &status, &tx, &memo); err != nil {
		t.Fatal(err)
	}
	return
}

func markHubTxIncluded(t *testing.T, a *app, hash string) {
	t.Helper()
	res, _ := a.db.Exec(`UPDATE tx_pool SET status='included' WHERE tx_hash=?`, hash)
	n, _ := res.RowsAffected()
	if n == 0 {
		res, _ = a.db.Exec(`UPDATE tx_history SET status='included' WHERE tx_hash=?`, hash)
		n, _ = res.RowsAffected()
	}
	if n == 0 {
		t.Fatalf("refund tx %s not found in pool/history", hash)
	}
}

// A refund queued while an earlier refund transfer is still in flight must not be wiped when
// that transfer lands, and a crash between submit and record must not pay twice.
func TestOrdersPoolRefundInFlightAndCrashSafe(t *testing.T) {
	f := newPoolEscrowFixture(t)
	ctx := context.Background()
	if err := f.a.ensureOrdersPoolEscrowTable(ctx); err != nil {
		t.Fatal(err)
	}
	// Fund the hub wallet directly in the transfer ledger (supply meta kept consistent).
	units := chain.HMCToUnits(10)
	if _, err := f.a.db.Exec(`INSERT INTO accounts (address, balance_units, next_nonce, updated_at) VALUES (?, ?, 0, strftime('%s','now'))
		ON CONFLICT(address) DO UPDATE SET balance_units = balance_units + excluded.balance_units`, f.a.signer.Address(), units); err != nil {
		t.Fatal(err)
	}
	_, _ = f.a.db.Exec(`INSERT INTO meta(key, value) VALUES('econ_total_minted_units', ?) ON CONFLICT(key) DO UPDATE SET value = CAST(value AS INTEGER) + ?`, fmt.Sprint(units), units)
	camp := "campaign-pool-t5"
	_, _ = f.a.db.Exec(`INSERT INTO orders_pool_escrow (campaign_id, funding_tx, owner, funded_units, created_at) VALUES (?,?,?,?,?)`,
		camp, poolEscrowHash(10), f.owner.Address(), chain.HMCToUnits(6), time.Now().Unix())

	f.a.queueOrdersPoolRefund(ctx, camp, chain.HMCToUnits(1), "overpayment")
	f.a.ordersPoolRefundTick(ctx)
	units, st, tx1, memo1 := poolRefundRow(t, f.a, camp)
	if st != "sent" || tx1 == "" || units != chain.HMCToUnits(1) {
		t.Fatalf("after first tick: units=%d st=%s tx=%q", units, st, tx1)
	}
	// Campaign closes while the overpayment refund is still pending on chain.
	f.a.queueOrdersPoolRefund(ctx, camp, chain.HMCToUnits(2), "campaign closed")
	if _, st, _, _ := poolRefundRow(t, f.a, camp); st != "sent" {
		t.Fatalf("queue during flight changed state to %s", st)
	}
	markHubTxIncluded(t, f.a, tx1)
	f.a.ordersPoolRefundTick(ctx)
	units, st, _, _ = poolRefundRow(t, f.a, camp)
	if units != chain.HMCToUnits(2) || st != "pending" {
		t.Fatalf("close refund lost: units=%d st=%s (want 2 HMC pending)", units, st)
	}

	// Second refund goes out; simulate a crash right after submit (hash never recorded).
	f.a.ordersPoolRefundTick(ctx)
	_, st, tx2, memo2 := poolRefundRow(t, f.a, camp)
	if st != "sent" || tx2 == "" || memo2 == memo1 {
		t.Fatalf("second refund: st=%s tx=%q memo=%q (memo must be unique per refund)", st, tx2, memo2)
	}
	_, _ = f.a.db.Exec(`UPDATE orders_pool_escrow SET refund_status='sending', refund_tx='', refund_sent_at=0 WHERE campaign_id=?`, camp)
	f.a.ordersPoolRefundTick(ctx)
	_, st, tx3, _ := poolRefundRow(t, f.a, camp)
	if st != "sent" || tx3 != tx2 {
		t.Fatalf("crash recovery resent instead of adopting: st=%s tx=%q want %q", st, tx3, tx2)
	}
	var n int
	_ = f.a.db.QueryRow(`SELECT COUNT(*) FROM tx_pool WHERE from_address=? AND tx_json LIKE ?`, f.a.signer.Address(), "%"+camp+":1%").Scan(&n)
	if n != 1 {
		t.Fatalf("second refund submitted %d times", n)
	}
	markHubTxIncluded(t, f.a, tx2)
	f.a.ordersPoolRefundTick(ctx)
	if units, st, _, _ := poolRefundRow(t, f.a, camp); units != 0 || st != "done" {
		t.Fatalf("final: units=%d st=%s", units, st)
	}
}

// A funding transfer that nobody claimed is refunded once after the timeout; a late claim for
// it is refused.
func TestOrdersPoolUnclaimedFundingRefunded(t *testing.T) {
	f := newPoolEscrowFixture(t)
	ctx := context.Background()
	if err := f.a.ensureOrdersPoolEscrowTable(ctx); err != nil {
		t.Fatal(err)
	}
	hub := f.a.signer.Address()
	camp := "campaign-pool-t6"
	f.putTransfer(poolEscrowHash(11), "included", f.owner.Address(), hub, 5, ordersPoolEscrowMemoPrefix+camp)
	f.putTransfer(poolEscrowHash(12), "included", f.owner.Address(), hub, 5, "unrelated")
	f.a.sweepUnclaimedPoolFunding(ctx)
	var n int
	_ = f.a.db.QueryRow(`SELECT COUNT(*) FROM orders_pool_escrow`).Scan(&n)
	if n != 0 {
		t.Fatal("fresh funding must wait for its claim")
	}
	_, _ = f.a.db.Exec(`UPDATE tx_history SET applied_at=applied_at-? WHERE tx_hash IN (?,?)`, ordersPoolUnclaimedRefundSec+60, poolEscrowHash(11), poolEscrowHash(12))
	f.a.sweepUnclaimedPoolFunding(ctx)
	f.a.sweepUnclaimedPoolFunding(ctx)
	_ = f.a.db.QueryRow(`SELECT COUNT(*) FROM orders_pool_escrow`).Scan(&n)
	if n != 1 {
		t.Fatalf("want exactly one refund row for the order funding, got %d", n)
	}
	units, st, _, _ := poolRefundRow(t, f.a, camp)
	if units != chain.HMCToUnits(5) || st != "pending" {
		t.Fatalf("unclaimed refund units=%d st=%s", units, st)
	}
	wantAPIError(t, f.post(camp, poolEscrowHash(11), f.owner, nil), http.StatusConflict, "funding_refunded")
}
