package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hackme/internal/chain"
)

// Normal recovery after a process stops just after recording an included payment.
func TestReviewUpdate2RecordedFundingIsRecoveredAfterRestart(t *testing.T) {
	f := newPoolEscrowFixture(t)
	ctx := context.Background()
	if err := f.a.ensureOrdersPoolEscrowTable(ctx); err != nil {
		t.Fatal(err)
	}
	camp, hash := "campaign-review-recorded-payment", poolEscrowHash(9002)
	f.putTransfer(hash, "included", f.owner.Address(), f.a.signer.Address(), 5, ordersPoolEscrowMemoPrefix+camp)
	if _, err := f.a.db.Exec(`UPDATE tx_history SET applied_at=applied_at-? WHERE tx_hash=?`, ordersPoolUnclaimedRefundSec+60, hash); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.db.Exec(`INSERT INTO orders_pool_escrow (campaign_id,funding_tx,owner,funded_units,created_at) VALUES (?,?,?,?,?)`, camp, hash, f.owner.Address(), chain.HMCToUnits(5), time.Now().Unix()-ordersPoolUnclaimedRefundSec-60); err != nil {
		t.Fatal(err)
	}
	wantAPIError(t, f.post(camp, hash, f.owner, nil), http.StatusConflict, "funding_refunded")
	// These are the two recovery passes used by the refund worker. No money is sent.
	f.a.ordersPoolRefundOnClose(ctx, camp)
	f.a.sweepUnclaimedPoolFunding(ctx)
	units, status, _, _ := poolRefundRow(t, f.a, camp)
	if units != chain.HMCToUnits(5) || status != "pending" {
		t.Fatalf("recorded payment with no campaign must be queued for refund: units=%d status=%s", units, status)
	}
}

// A transient database write failure must not permanently mark a refund queued.
func TestReviewUpdate2ClosedRefundRecoversFromQueueWriteFailure(t *testing.T) {
	f := newPoolEscrowFixture(t)
	ctx := context.Background()
	if err := f.a.ensureOrdersPoolEscrowTable(ctx); err != nil {
		t.Fatal(err)
	}
	camp := "campaign-review-close-refund"
	if _, err := f.a.db.Exec(`INSERT INTO orders_pool_escrow (campaign_id,funding_tx,owner,funded_units,created_at) VALUES (?,?,?,?,?)`, camp, poolEscrowHash(9003), f.owner.Address(), chain.HMCToUnits(1), time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.db.Exec(`UPDATE wallet SET balance_units=?,balance_hmc=10 WHERE id=1`, chain.HMCToUnits(10)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.db.Exec(`INSERT INTO accounts(address,balance_units,next_nonce,updated_at) VALUES (?,?,0,0) ON CONFLICT(address) DO UPDATE SET balance_units=excluded.balance_units`, f.a.signer.Address(), chain.HMCToUnits(10)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.chain.OpenFuzzEscrow(ctx, camp, 1, 64); err != nil {
		t.Fatal(err)
	}
	closed, err := f.a.chain.CancelFuzzEscrow(ctx, camp)
	if err != nil {
		t.Fatal(err)
	}
	want := chain.HMCToUnits(closed.RunsPoolHMC + closed.BountyPoolHMC - closed.RunsPaidHMC - closed.BountyPaidHMC - closed.CrashBonusPaidHMC)
	if want == 0 {
		t.Fatal("test requires an unspent closed escrow")
	}
	if _, err := f.a.db.Exec(`CREATE TRIGGER review_refund_write_failure BEFORE UPDATE OF refund_units ON orders_pool_escrow BEGIN SELECT RAISE(FAIL,'review simulated storage error'); END`); err != nil {
		t.Fatal(err)
	}
	f.a.ordersPoolRefundOnClose(ctx, camp)
	if _, err := f.a.db.Exec(`DROP TRIGGER review_refund_write_failure`); err != nil {
		t.Fatal(err)
	}
	f.a.ordersPoolRefundOnClose(ctx, camp)
	units, status, _, _ := poolRefundRow(t, f.a, camp)
	if units != want || status != "pending" {
		t.Fatalf("refund must recover after storage is healthy again: units=%d status=%s want=%d pending", units, status, want)
	}
}

// Per-order clients discover the hub through the canonical URL, independently of
// the legacy prepaid HACKME_ORDERS_HUB_URL setting.
func TestReviewUpdate2PaidMirrorCancelRequiresHubConfirmation(t *testing.T) {
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer hub.Close()
	a := reviewRC20Customer(t, "")
	t.Setenv("HACKME_CANONICAL_CHAIN_URL", hub.URL)
	camp := "campaign-review-paid-mirror"
	a.insertPoolOrderMirror(context.Background(), camp, securityAuditRequest{Title: "Paid pool scan", BudgetRuns: 64}, poolEscrowHash(9004), nil, []byte(`{"dig_package":"scan","guard_pack":"secrets"}`))
	r := httptest.NewRequest(http.MethodPost, "/api/fuzz/campaigns/"+camp+"/status", bytes.NewBufferString(`{"status":"cancelled"}`))
	r.Header.Set("X-Hackme-Admin-Token", "review-local-admin-token-for-tests")
	w := httptest.NewRecorder()
	a.handleFuzzCampaignStatus(w, r, camp)
	c, err := a.getFuzzCampaign(context.Background(), camp)
	if err != nil {
		t.Fatal(err)
	}
	if w.Code < 400 || c.Status == "cancelled" {
		t.Fatalf("hub did not confirm the paid order cancellation: HTTP %d local status=%s", w.Code, c.Status)
	}
}
