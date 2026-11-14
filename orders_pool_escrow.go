package main

// Self-serve pool orders paid on chain (hub side).
//
// Until rc20 a customer's Orders node could not put work on the public pool: pool campaigns on
// the minting hub need an escrow on the hub ledger, and only an operator could open one. The
// customer's "Fund Orders" transfer went to the platform address and the scan ran on the
// customer's own machine, so pool miners never saw that money.
//
// Flow:
//  1. The customer node sends an on-chain transfer of the order price to the hub wallet with
//     memo "orders_escrow:v1:<campaign_id>".
//  2. Once included, it POSTs /api/orders/pool-escrow here, signed with the same wallet key.
//  3. The hub verifies the transfer on its own chain (recipient, sender = signer, memo, amount,
//     not used before) and creates the campaign with the same code an operator uses
//     (serveSecurityAudit): escrow on the hub ledger + admin registration on the coordinator,
//     owner_ref = the customer address, plus a per-campaign owner secret for reading results.
//  4. Pool miners are paid from that escrow by the hub's normal settle path (on chain).
//  5. When the campaign closes, the unspent part goes back to the customer as a transfer.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"time"

	"hackme/internal/chain"
	"hackme/internal/logsafe"
	"hackme/internal/netutil"
	"hackme/internal/poolsync"
)

const (
	ordersPoolEscrowMemoPrefix  = "orders_escrow:v1:"
	ordersPoolRefundMemoPrefix  = "orders_escrow_refund:v1:"
	ordersPoolEscrowSigDomain   = "hackme-orders-pool-escrow|v1|"
	ordersPoolEscrowMaxSkewSec  = 600
	ordersPoolEscrowMaxBodySize = 4 << 20 // custom guard wasm hex
)

// ordersPoolEscrowEnabled: only the minting hub (whose ledger pays miners) serves this route.
func ordersPoolEscrowEnabled() bool {
	if v := strings.TrimSpace(os.Getenv("HACKME_ORDERS_POOL_ESCROW")); v != "" {
		return envBool("HACKME_ORDERS_POOL_ESCROW", false)
	}
	return envBool("HACKME_CHAIN_MINTING_HUB", false)
}

type ordersPoolEscrowRequest struct {
	CampaignID     string          `json:"campaign_id"`
	Owner          string          `json:"owner_address"`
	OwnerPubKey    string          `json:"owner_pubkey_ed25519"`
	FundingTx      string          `json:"funding_tx_hash"`
	Timestamp      int64           `json:"timestamp_unix"`
	Signature      string          `json:"sig_ed25519"`
	Order          json.RawMessage `json:"order"` // ciDigRequest + seed_byte_corpus
	SeedByteCorpus []string        `json:"-"`
}

type ordersPoolOrder struct {
	ciDigRequest
	SeedByteCorpus []string `json:"seed_byte_corpus"`
}

// ordersPoolEscrowSigningBytes is what the customer signs: binds campaign, funding tx, owner,
// time and the exact order body.
func ordersPoolEscrowSigningBytes(campaignID, fundingTx, owner string, ts int64, order []byte) []byte {
	h := sha256.Sum256(order)
	return []byte(ordersPoolEscrowSigDomain + campaignID + "|" + fundingTx + "|" + owner + "|" +
		strconv.FormatInt(ts, 10) + "|" + hex.EncodeToString(h[:]))
}

func (a *app) ensureOrdersPoolEscrowTable(ctx context.Context) error {
	_, err := a.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS orders_pool_escrow (
		campaign_id TEXT PRIMARY KEY,
		funding_tx TEXT NOT NULL UNIQUE,
		owner TEXT NOT NULL,
		funded_units INTEGER NOT NULL,
		escrow_units INTEGER NOT NULL DEFAULT 0,
		refund_units INTEGER NOT NULL DEFAULT 0,
		refund_tx TEXT NOT NULL DEFAULT '',
		refund_status TEXT NOT NULL DEFAULT 'none',
		refund_error TEXT NOT NULL DEFAULT '',
		closed_queued INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL
	)`)
	if err != nil {
		return err
	}
	// In-flight refund bookkeeping (added before release; ALTER is a no-op once present).
	for _, col := range []string{
		"refund_seq INTEGER NOT NULL DEFAULT 0",
		"refund_memo TEXT NOT NULL DEFAULT ''",
		"refund_sent_units INTEGER NOT NULL DEFAULT 0",
		"refund_sent_at INTEGER NOT NULL DEFAULT 0",
	} {
		if _, aerr := a.db.ExecContext(ctx, `ALTER TABLE orders_pool_escrow ADD COLUMN `+col); aerr != nil &&
			!strings.Contains(strings.ToLower(aerr.Error()), "duplicate column") {
			return aerr
		}
	}
	return nil
}

// ordersPoolOwnerSecret is deterministic for (campaign, owner) and only the hub can compute it
// (ed25519 signatures are deterministic), so a customer that lost its copy gets the same secret
// again by re-sending its signed claim — and nobody else can derive it from public data.
func (a *app) ordersPoolOwnerSecret(campaignID, owner string) string {
	if a.signer == nil {
		return ""
	}
	sig := a.signer.SignHex([]byte("hackme-orders-owner-secret|v1|" + campaignID + "|" + owner))
	h := sha256.Sum256([]byte(sig))
	return hex.EncodeToString(h[:])
}

func (a *app) handleOrdersPoolEscrow(w http.ResponseWriter, r *http.Request) {
	if !ordersPoolEscrowEnabled() {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	if !a.allowRate("orders_pool_escrow:"+clientIP(r), 1) {
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "rate limited", nil)
		return
	}
	if a.signer == nil || a.chain == nil || a.db == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "hub_unavailable", "hub signer/chain unavailable", nil)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, ordersPoolEscrowMaxBodySize+1))
	if err != nil || len(body) > ordersPoolEscrowMaxBodySize {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "body_too_large", "request too large", nil)
		return
	}
	var req ordersPoolEscrowRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid json", nil)
		return
	}
	req.CampaignID = strings.TrimSpace(req.CampaignID)
	req.Owner = strings.TrimSpace(req.Owner)
	req.FundingTx = strings.ToLower(strings.TrimSpace(req.FundingTx))
	if req.CampaignID == "" || cleanFuzzID(req.CampaignID, "campaign") != req.CampaignID || len(req.CampaignID) > 96 {
		writeAPIError(w, http.StatusBadRequest, "invalid_campaign_id", "campaign_id must be lowercase [a-z0-9-]", nil)
		return
	}
	if !chain.IsValidAddress(req.Owner) || len(req.FundingTx) != 64 {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "owner_address and funding_tx_hash required", nil)
		return
	}
	now := time.Now().Unix()
	if req.Timestamp < now-ordersPoolEscrowMaxSkewSec || req.Timestamp > now+ordersPoolEscrowMaxSkewSec {
		writeAPIError(w, http.StatusBadRequest, "stale_request", "timestamp_unix too far from hub time", nil)
		return
	}
	pub, perr := hex.DecodeString(strings.TrimSpace(req.OwnerPubKey))
	sig, serr := hex.DecodeString(strings.TrimSpace(req.Signature))
	if perr != nil || serr != nil || len(pub) != ed25519.PublicKeySize || len(sig) != ed25519.SignatureSize ||
		!chain.PubKeyOwnsAddress(strings.TrimSpace(req.OwnerPubKey), req.Owner) ||
		!ed25519.Verify(ed25519.PublicKey(pub), ordersPoolEscrowSigningBytes(req.CampaignID, req.FundingTx, req.Owner, req.Timestamp, req.Order), sig) {
		writeAPIError(w, http.StatusUnauthorized, "bad_signature", "claim must be signed by the paying wallet", nil)
		return
	}
	ctx := r.Context()
	if err := a.ensureOrdersPoolEscrowTable(ctx); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "db_error", err.Error(), nil)
		return
	}
	a.ordersPoolEscrowMu.Lock()
	defer a.ordersPoolEscrowMu.Unlock()

	// Idempotent retry: same campaign, same funding tx, same owner → same answer (and secret).
	var exCampaign, exTx, exOwner string
	err = a.db.QueryRowContext(ctx, `SELECT campaign_id, funding_tx, owner FROM orders_pool_escrow WHERE campaign_id=? OR funding_tx=?`,
		req.CampaignID, req.FundingTx).Scan(&exCampaign, &exTx, &exOwner)
	if err == nil {
		if exCampaign != req.CampaignID || exTx != req.FundingTx || exOwner != req.Owner {
			writeAPIError(w, http.StatusConflict, "funding_used", "this funding tx or campaign id is already bound to another order", nil)
			return
		}
		var one int
		if cerr := a.db.QueryRowContext(ctx, `SELECT 1 FROM fuzz_campaigns WHERE id=?`, req.CampaignID).Scan(&one); cerr != nil {
			// Funding recorded but no campaign: the create failed or the funding was never
			// claimed in time — it is being refunded, never reported as a live order.
			writeAPIError(w, http.StatusConflict, "funding_refunded", "this order was not created; the funding is refunded to "+req.Owner, nil)
			return
		}
		writeJSON(w, map[string]any{
			"ok": true, "campaign_id": req.CampaignID, "idempotent": true,
			"owner_ref": req.Owner, "orders_owner_secret": a.ordersPoolOwnerSecret(req.CampaignID, req.Owner),
		})
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		writeAPIError(w, http.StatusInternalServerError, "db_error", err.Error(), nil)
		return
	}

	// The funding transfer, on this hub's chain.
	tx, found, err := a.chain.TransferTxByHash(ctx, req.FundingTx)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "chain_error", err.Error(), nil)
		return
	}
	if !found {
		writeAPIError(w, http.StatusNotFound, "funding_not_found", "funding tx not found on the hub — wait for inclusion and retry", nil)
		return
	}
	switch strings.ToLower(tx.Status) {
	case "included", "applied", "confirmed":
	default:
		writeAPIError(w, http.StatusConflict, "funding_not_included", "funding tx is "+tx.Status+" — retry after it is in a block", nil)
		return
	}
	hubAddr := strings.TrimSpace(a.signer.Address())
	if tx.To != hubAddr && tx.To != strings.TrimSpace(a.signer.LongAddress()) {
		writeAPIError(w, http.StatusBadRequest, "funding_wrong_recipient", "funding must be sent to the hub escrow address "+hubAddr, nil)
		return
	}
	if tx.From != req.Owner {
		writeAPIError(w, http.StatusBadRequest, "funding_wrong_sender", "funding tx was not sent by owner_address", nil)
		return
	}
	if tx.Memo != ordersPoolEscrowMemoPrefix+req.CampaignID {
		writeAPIError(w, http.StatusBadRequest, "funding_wrong_memo", "funding memo must be "+ordersPoolEscrowMemoPrefix+req.CampaignID, nil)
		return
	}

	// The order itself: the same narrow builder as the CI route (shipped pack, preset or the
	// customer's own guard wasm; scan|audit|deep), forced onto the pool with no PoH order.
	var order ordersPoolOrder
	if err := json.Unmarshal(req.Order, &order); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_order", "order: "+err.Error(), nil)
		return
	}
	audit, err := ciBuildAuditRequest(order.ciDigRequest, ciPolicy{AllowDeep: true, AllowPool: true})
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_order", err.Error(), nil)
		return
	}
	priceUnits := chain.HMCToUnits(audit.BudgetHMC)
	if tx.AmountUnits < priceUnits {
		writeAPIError(w, http.StatusPaymentRequired, "funding_too_small",
			fmt.Sprintf("funding %.8f HMC is below the %s price %.8f HMC", chain.UnitsToHMC(tx.AmountUnits), audit.DigPackage, audit.BudgetHMC), nil)
		return
	}
	yes, no := true, false
	audit.CampaignID = req.CampaignID
	audit.OrderID = "order-" + strings.TrimPrefix(req.CampaignID, "campaign-")
	audit.PayerRef = req.Owner
	audit.PoolDistributed = &yes
	audit.CreatePoHOrder = &no
	audit.PublicProof = &no
	if len(order.SeedByteCorpus) > 0 {
		if len(order.SeedByteCorpus) > 256 {
			order.SeedByteCorpus = order.SeedByteCorpus[:256]
		}
		audit.SeedByteCorpus = order.SeedByteCorpus
	}
	secret := a.ordersPoolOwnerSecret(req.CampaignID, req.Owner)
	audit.ownerSecretHash = poolsync.OwnerSecretHash(secret)

	// Record the funding first (UNIQUE funding_tx) so a crash between escrow open and the
	// record cannot let the same transfer fund two campaigns.
	if _, err := a.db.ExecContext(ctx,
		`INSERT INTO orders_pool_escrow (campaign_id, funding_tx, owner, funded_units, created_at) VALUES (?,?,?,?,?)`,
		req.CampaignID, req.FundingTx, req.Owner, tx.AmountUnits, now); err != nil {
		writeAPIError(w, http.StatusConflict, "funding_used", "funding tx already used", nil)
		return
	}

	// Same code path as an operator order on the hub: escrow from the hub wallet (which just
	// received the funding) + pool registration as admin.
	rec := httptest.NewRecorder()
	inner := httptest.NewRequest(http.MethodPost, "/api/security-audit", bytes.NewReader(nil)).WithContext(ctx)
	inner.RemoteAddr = r.RemoteAddr
	a.serveSecurityAudit(rec, inner, audit)
	res := rec.Result()
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		if row, gerr := a.chain.GetFuzzEscrow(ctx, req.CampaignID); gerr == nil && row != nil {
			// Escrow opened before the failure: refunding the full funding now would pay twice
			// (once here, once more when that escrow closes). Close it instead; the close path
			// (ordersPoolRefundOnClose) returns its unspent part, the overpayment goes now.
			if _, cerr := a.db.ExecContext(ctx, `UPDATE fuzz_campaigns SET status='cancelled', completed_at=? WHERE id=? AND status IN ('planned','running')`, now, req.CampaignID); cerr == nil {
				a.tryCloseFuzzEscrowForStatus(ctx, req.CampaignID, "cancelled")
			}
			_ = a.pushPoolCampaignStatus(ctx, req.CampaignID, "cancelled")
			if tx.AmountUnits > priceUnits {
				a.queueOrdersPoolRefund(ctx, req.CampaignID, tx.AmountUnits-priceUnits, "overpayment")
			}
			log.Printf("orders pool escrow: %s create failed after escrow open — cancelled, refund on close", logsafe.ID(req.CampaignID))
		} else {
			// Nothing was escrowed: the whole funding goes back.
			a.queueOrdersPoolRefund(ctx, req.CampaignID, tx.AmountUnits, "create failed")
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(res.StatusCode)
		_, _ = w.Write(out)
		return
	}
	escrowUnits := uint64(0)
	if row, err := a.chain.GetFuzzEscrow(ctx, req.CampaignID); err == nil && row != nil {
		escrowUnits = chain.HMCToUnits(row.BudgetHMC)
	}
	// Anything paid above the price is returned right away.
	if tx.AmountUnits > priceUnits {
		a.queueOrdersPoolRefund(ctx, req.CampaignID, tx.AmountUnits-priceUnits, "overpayment")
	}
	_, _ = a.db.ExecContext(ctx, `UPDATE orders_pool_escrow SET escrow_units=? WHERE campaign_id=?`, escrowUnits, req.CampaignID)
	log.Printf("orders pool escrow: %s funded by %s (%s HMC, tx %s)", logsafe.ID(req.CampaignID), logsafe.ID(req.Owner),
		strconv.FormatFloat(chain.UnitsToHMC(tx.AmountUnits), 'f', 8, 64), logsafe.ID(req.FundingTx[:16]))

	var created map[string]any
	_ = json.Unmarshal(out, &created)
	if created == nil {
		created = map[string]any{}
	}
	created["ok"] = true
	created["owner_ref"] = req.Owner
	created["orders_owner_secret"] = secret
	created["funded_hmc"] = chain.UnitsToHMC(tx.AmountUnits)
	created["price_hmc"] = audit.BudgetHMC
	created["payment"] = "Paid on chain to the hub escrow; pool miners are paid from it, the unspent part is sent back to " + req.Owner + " when the scan closes."
	writeJSON(w, created)
}

// queueOrdersPoolRefund adds units to the pending refund for a funded campaign.
func (a *app) queueOrdersPoolRefund(ctx context.Context, campaignID string, units uint64, why string) {
	if units == 0 || a.db == nil {
		return
	}
	res, err := a.db.ExecContext(ctx,
		// A refund already in flight keeps its state: the tick subtracts only what that transfer
		// carried and sends the rest afterwards.
		`UPDATE orders_pool_escrow SET refund_units = refund_units + ?, refund_error='',
		        refund_status = CASE WHEN refund_status IN ('sent','sending') THEN refund_status ELSE 'pending' END
		  WHERE campaign_id=?`,
		units, campaignID)
	if err != nil {
		log.Printf("orders pool escrow: queue refund %s: %v", logsafe.ID(campaignID), err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("orders pool escrow: refund %s HMC queued for %s (%s)",
			strconv.FormatFloat(chain.UnitsToHMC(units), 'f', 8, 64), logsafe.ID(campaignID), why)
	}
}

// ordersPoolRefundOnClose queues the unspent escrow of a customer-funded campaign after the
// hub closed it (the close itself returned those units to the hub wallet). Runs once per
// campaign (closed_queued flips 0→1 atomically).
func (a *app) ordersPoolRefundOnClose(ctx context.Context, campaignID string) {
	if a.db == nil || a.chain == nil {
		return
	}
	if err := a.ensureOrdersPoolEscrowTable(ctx); err != nil {
		return
	}
	row, err := a.chain.GetFuzzEscrow(ctx, campaignID)
	if err != nil || row == nil || !strings.EqualFold(strings.TrimSpace(row.Status), "closed") {
		return
	}
	res, err := a.db.ExecContext(ctx, `UPDATE orders_pool_escrow SET closed_queued=1 WHERE campaign_id=? AND closed_queued=0`, campaignID)
	if err != nil {
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return // not customer-funded, or already queued
	}
	left := row.RunsPoolHMC + row.BountyPoolHMC - row.RunsPaidHMC - row.BountyPaidHMC - row.CrashBonusPaidHMC
	if units := chain.HMCToUnits(left); units > 0 {
		a.queueOrdersPoolRefund(ctx, campaignID, units, "campaign closed")
	}
}

// startOrdersPoolRefundWorker sends pending refunds as hub-signed transfers, one at a time,
// retrying on nonce conflicts with the settler's payouts.
func (a *app) startOrdersPoolRefundWorker(ctx context.Context) {
	if !ordersPoolEscrowEnabled() || a.db == nil {
		return
	}
	go func() {
		t := time.NewTicker(45 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				a.ordersPoolRefundTick(ctx)
			}
		}
	}()
}

func (a *app) ordersPoolRefundTick(ctx context.Context) {
	if a.signer == nil || a.chain == nil {
		return
	}
	if err := a.ensureOrdersPoolEscrowTable(ctx); err != nil {
		return
	}
	// Safety net: any customer-funded escrow that is closed but not yet queued (whatever path
	// closed it — finalize, cancel, stale sweep, a crash between close and hook) is queued here.
	if rows, err := a.db.QueryContext(ctx, `SELECT campaign_id FROM orders_pool_escrow WHERE closed_queued=0`); err == nil {
		var ids []string
		for rows.Next() {
			var id string
			if rows.Scan(&id) == nil {
				ids = append(ids, id)
			}
		}
		rows.Close()
		for _, id := range ids {
			a.ordersPoolRefundOnClose(ctx, id)
		}
	}
	a.sweepUnclaimedPoolFunding(ctx)
	type pend struct {
		campaign, owner, status, tx, memo string
		units, sentUnits                  uint64
		seq, sentAt                       int64
	}
	var list []pend
	rows, err := a.db.QueryContext(ctx, `SELECT campaign_id, owner, refund_status, refund_tx, refund_memo, refund_units, refund_sent_units, refund_seq, refund_sent_at
		FROM orders_pool_escrow WHERE refund_status IN ('pending','sending','sent')`)
	if err != nil {
		return
	}
	for rows.Next() {
		var p pend
		if rows.Scan(&p.campaign, &p.owner, &p.status, &p.tx, &p.memo, &p.units, &p.sentUnits, &p.seq, &p.sentAt) == nil {
			list = append(list, p)
		}
	}
	rows.Close()
	now := time.Now().Unix()
	for _, p := range list {
		switch p.status {
		case "sent":
			// A refund transfer was submitted: settle its fate before sending anything else.
			st, found, err := a.chain.TransferTxByHash(ctx, p.tx)
			if err != nil {
				continue
			}
			if !found {
				if now-p.sentAt > 1800 {
					// Dropped from the pool: look it up by memo again, then resend the same memo.
					_, _ = a.db.ExecContext(ctx, `UPDATE orders_pool_escrow SET refund_status='sending', refund_tx='', refund_sent_at=0 WHERE campaign_id=? AND refund_tx=?`, p.campaign, p.tx)
				}
				continue
			}
			switch strings.ToLower(st.Status) {
			case "included", "applied", "confirmed":
				// Subtract only what this transfer carried; anything queued meanwhile stays.
				_, _ = a.db.ExecContext(ctx, `UPDATE orders_pool_escrow
					SET refund_units = CASE WHEN refund_units > ? THEN refund_units - ? ELSE 0 END,
					    refund_status = CASE WHEN refund_units > ? THEN 'pending' ELSE 'done' END,
					    refund_seq = refund_seq + 1, refund_memo='', refund_sent_units=0, refund_tx=''
					WHERE campaign_id=? AND refund_tx=?`, p.sentUnits, p.sentUnits, p.sentUnits, p.campaign, p.tx)
				log.Printf("orders pool escrow: refund for %s included (%s)", logsafe.ID(p.campaign), logsafe.ID(p.tx[:16]))
			case "rejected", "failed":
				// Not applied: the same memo/amount goes out again on a later tick.
				_, _ = a.db.ExecContext(ctx, `UPDATE orders_pool_escrow SET refund_status='sending', refund_tx='', refund_sent_at=0, refund_error=? WHERE campaign_id=? AND refund_tx=?`,
					"rejected: "+st.RejectCode, p.campaign, p.tx)
			}
			continue
		case "sending":
			// Intent recorded but the hash was not: the transfer may still have gone out (crash
			// between submit and record). Adopt it if the memo is on chain/in the pool.
			if hash, ok := a.findHubRefundByMemo(ctx, p.memo); ok {
				_, _ = a.db.ExecContext(ctx, `UPDATE orders_pool_escrow SET refund_status='sent', refund_tx=?, refund_sent_at=? WHERE campaign_id=?`, hash, now, p.campaign)
				continue
			}
			if p.sentAt != 0 && now-p.sentAt < 180 {
				continue
			}
		case "pending":
			if p.units == 0 {
				_, _ = a.db.ExecContext(ctx, `UPDATE orders_pool_escrow SET refund_status='done' WHERE campaign_id=? AND refund_units=0 AND refund_status='pending'`, p.campaign)
				continue
			}
			p.memo = ordersPoolRefundMemoPrefix + p.campaign + ":" + strconv.FormatInt(p.seq, 10)
			p.sentUnits = p.units
		}
		// Record the intent BEFORE submitting so a crash cannot turn into a second payment.
		if _, err := a.db.ExecContext(ctx, `UPDATE orders_pool_escrow SET refund_status='sending', refund_memo=?, refund_sent_units=?, refund_sent_at=? WHERE campaign_id=?`,
			p.memo, p.sentUnits, now, p.campaign); err != nil {
			continue
		}
		hash, err := a.sendHubTransfer(ctx, p.owner, p.sentUnits, p.memo)
		if err != nil {
			_, _ = a.db.ExecContext(ctx, `UPDATE orders_pool_escrow SET refund_error=?, refund_sent_at=0 WHERE campaign_id=?`, err.Error(), p.campaign)
			continue
		}
		_, _ = a.db.ExecContext(ctx, `UPDATE orders_pool_escrow SET refund_status='sent', refund_tx=? WHERE campaign_id=?`, hash, p.campaign)
		log.Printf("orders pool escrow: refund %s HMC to %s for %s (tx %s)",
			strconv.FormatFloat(chain.UnitsToHMC(p.sentUnits), 'f', 8, 64), logsafe.ID(p.owner), logsafe.ID(p.campaign), logsafe.ID(hash[:16]))
		return // one transfer per tick: the next nonce is only known after this one lands
	}
}

// ordersPoolUnclaimedRefundSec: a funding transfer that was never claimed (customer node died
// between paying and claiming) is sent back after this long.
const ordersPoolUnclaimedRefundSec = 6 * 3600

// sweepUnclaimedPoolFunding refunds included order-funding transfers to the hub that no claim
// recorded within ordersPoolUnclaimedRefundSec. The row it inserts also blocks a late claim
// (the handler answers funding_refunded since no campaign exists).
func (a *app) sweepUnclaimedPoolFunding(ctx context.Context) {
	hub := strings.TrimSpace(a.signer.Address())
	cutoff := time.Now().Unix() - ordersPoolUnclaimedRefundSec
	rows, err := a.db.QueryContext(ctx, `SELECT h.tx_hash, h.from_address, h.amount_units, h.tx_json FROM tx_history h
		WHERE (h.to_address=? OR h.to_address=?) AND h.status IN ('included','applied','confirmed') AND h.applied_at < ?
		  AND h.tx_json LIKE ? AND NOT EXISTS (SELECT 1 FROM orders_pool_escrow e WHERE e.funding_tx=h.tx_hash)
		LIMIT 32`, hub, strings.TrimSpace(a.signer.LongAddress()), cutoff, "%"+ordersPoolEscrowMemoPrefix+"%")
	if err != nil {
		return
	}
	type fund struct {
		hash, from, campaign string
		units                uint64
	}
	var list []fund
	for rows.Next() {
		var f fund
		var js string
		if rows.Scan(&f.hash, &f.from, &f.units, &js) != nil {
			continue
		}
		var m struct {
			Memo string `json:"memo"`
		}
		if json.Unmarshal([]byte(js), &m) != nil || !strings.HasPrefix(m.Memo, ordersPoolEscrowMemoPrefix) {
			continue
		}
		f.campaign = strings.TrimPrefix(m.Memo, ordersPoolEscrowMemoPrefix)
		list = append(list, f)
	}
	rows.Close()
	a.ordersPoolEscrowMu.Lock() // never race a claim that is recording this funding right now
	defer a.ordersPoolEscrowMu.Unlock()
	for _, f := range list {
		if f.units == 0 || !chain.IsValidAddress(f.from) {
			continue
		}
		// Key the row by the funding tx when the campaign id is taken or malformed, so one bad
		// memo cannot block another customer's refund.
		key := f.campaign
		var one int
		if key == "" || cleanFuzzID(key, "campaign") != key || len(key) > 96 ||
			a.db.QueryRowContext(ctx, `SELECT 1 FROM orders_pool_escrow WHERE campaign_id=?`, key).Scan(&one) == nil ||
			a.db.QueryRowContext(ctx, `SELECT 1 FROM fuzz_campaigns WHERE id=?`, key).Scan(&one) == nil {
			key = "unclaimed-" + f.hash[:24]
		}
		if _, err := a.db.ExecContext(ctx, `INSERT INTO orders_pool_escrow (campaign_id, funding_tx, owner, funded_units, refund_units, refund_status, closed_queued, created_at)
			VALUES (?,?,?,?,?,'pending',1,?)`, key, f.hash, f.from, f.units, f.units, time.Now().Unix()); err == nil {
			log.Printf("orders pool escrow: unclaimed funding %s from %s (%s HMC) — refunding", logsafe.ID(f.hash[:16]), logsafe.ID(f.from),
				strconv.FormatFloat(chain.UnitsToHMC(f.units), 'f', 8, 64))
		}
	}
}

// findHubRefundByMemo finds a not-rejected transfer from the hub wallet carrying memo (pool or
// history). Memos are unique per refund (campaign + sequence).
func (a *app) findHubRefundByMemo(ctx context.Context, memo string) (string, bool) {
	if memo == "" || a.signer == nil {
		return "", false
	}
	from := strings.TrimSpace(a.signer.Address())
	pat := "%" + strings.TrimPrefix(memo, ordersPoolRefundMemoPrefix) + "%"
	for _, q := range []string{
		`SELECT tx_hash, tx_json, status FROM tx_history WHERE from_address=? AND tx_json LIKE ?`,
		`SELECT tx_hash, tx_json, status FROM tx_pool WHERE from_address=? AND tx_json LIKE ?`,
	} {
		rows, err := a.db.QueryContext(ctx, q, from, pat)
		if err != nil {
			continue
		}
		for rows.Next() {
			var hash, js, st string
			if rows.Scan(&hash, &js, &st) != nil {
				continue
			}
			if st = strings.ToLower(st); st == "rejected" || st == "failed" {
				continue
			}
			var m struct {
				Memo string `json:"memo"`
			}
			if json.Unmarshal([]byte(js), &m) == nil && m.Memo == memo {
				rows.Close()
				return hash, true
			}
		}
		rows.Close()
	}
	return "", false
}

// sendHubTransfer signs and submits a transfer from the hub wallet on its own chain.
func (a *app) sendHubTransfer(ctx context.Context, to string, units uint64, memo string) (string, error) {
	from := strings.TrimSpace(a.signer.Address())
	st, err := a.chain.TransferAddressState(ctx, from)
	if err != nil {
		return "", err
	}
	tx := chain.TransferTx{
		TxType:        "transfer_v1",
		SigAlg:        chain.TransferSigAlgEd25519,
		From:          from,
		To:            to,
		AmountUnits:   units,
		FeeUnits:      chain.DefaultTransferMinFee,
		Nonce:         st.NextNonce,
		TimestampUnix: time.Now().Unix(),
		Memo:          memo,
		PubKeyEd25519: strings.TrimSpace(a.signer.PublicKeyHex()),
	}
	tx.ChainID = a.txChainIDForSigning(ctx)
	canon, err := tx.CanonicalBytes()
	if err != nil {
		return "", err
	}
	tx.SigEd25519 = strings.TrimSpace(a.signer.SignHex(canon))
	hash, status, err := a.chain.SubmitTransferTx(ctx, tx)
	if err != nil {
		return "", fmt.Errorf("%s: %w", status, err)
	}
	return hash, nil
}

// ordersPoolEscrowStatus advertises the customer-funded pool route in /api/status (hub only),
// so Orders nodes know where to pay and what to put in the memo.
func (a *app) ordersPoolEscrowStatus() any {
	if !ordersPoolEscrowEnabled() || a == nil || a.signer == nil {
		return nil
	}
	return map[string]any{
		"enabled":     true,
		"address":     strings.TrimSpace(a.signer.Address()),
		"memo_prefix": ordersPoolEscrowMemoPrefix,
		"path":        "/api/orders/pool-escrow",
		"packages":    []string{"scan", "audit", "deep"},
		"note":        "Pay the order price to address with memo memo_prefix+campaign_id, then POST the signed claim to path.",
	}
}

// pushPoolCampaignStatus stops (or completes) a pool campaign on the coordinator. Called by the
// hub before it closes the escrow: otherwise the coordinator keeps leasing work that no escrow
// will pay for. Admin-only route; no-op where this node has no coordinator admin token.
func (a *app) pushPoolCampaignStatus(ctx context.Context, campaignID, status string) error {
	base := strings.TrimRight(strings.TrimSpace(a.coordinatorBaseURL()), "/")
	tok := strings.TrimSpace(poolsync.CoordinatorAdminToken())
	if base == "" || tok == "" {
		return nil
	}
	body, _ := json.Marshal(map[string]string{"id": campaignID, "status": status})
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, base+"/api/fuzz/pool/campaigns/status", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", tok)
	resp, err := netutil.HTTPClientNoRedirect(15 * time.Second).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("coordinator status HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}
