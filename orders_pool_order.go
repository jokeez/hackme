package main

// Self-serve pool orders paid on chain (customer Orders node side). See orders_pool_escrow.go
// for the hub side and the whole flow.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"hackme/internal/chain"
	"hackme/internal/logsafe"
	"hackme/internal/netutil"
	"hackme/internal/poolsync"
)

type hubPoolEscrowInfo struct {
	Enabled    bool   `json:"enabled"`
	Address    string `json:"address"`
	MemoPrefix string `json:"memo_prefix"`
	Path       string `json:"path"`
}

// fetchHubPoolEscrowInfo reads the hub's advertised pool-order route from canonical /api/status.
func (a *app) fetchHubPoolEscrowInfo(ctx context.Context, base string) (hubPoolEscrowInfo, error) {
	var out hubPoolEscrowInfo
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, base+"/api/status?lite=1", nil)
	if err != nil {
		return out, err
	}
	resp, err := netutil.HTTPClientNoRedirect(10 * time.Second).Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	var st struct {
		OrdersPoolEscrow *hubPoolEscrowInfo `json:"orders_pool_escrow"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&st); err != nil {
		return out, err
	}
	if st.OrdersPoolEscrow == nil || !st.OrdersPoolEscrow.Enabled || !chain.IsValidAddress(st.OrdersPoolEscrow.Address) ||
		st.OrdersPoolEscrow.MemoPrefix != ordersPoolEscrowMemoPrefix || !strings.HasPrefix(st.OrdersPoolEscrow.Path, "/api/") {
		return out, fmt.Errorf("the network does not accept paid pool orders yet")
	}
	return *st.OrdersPoolEscrow, nil
}

func (a *app) handleOrdersPoolOrder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	if !a.allowRate("orders_pool_order:"+clientIP(r), 1) {
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "rate limited", nil)
		return
	}
	if !requireAdminAuthStrict(w, r) {
		return
	}
	logAdminAction(r, "orders_pool_order")
	if a.signer == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "signer_unavailable", "node wallet unavailable", nil)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, ordersPoolEscrowMaxBodySize+1))
	if err != nil || len(body) > ordersPoolEscrowMaxBodySize {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "body_too_large", "request too large", nil)
		return
	}
	var order ordersPoolOrder
	if err := json.Unmarshal(body, &order); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid json", nil)
		return
	}
	// Same builder the hub runs: the price we pay is the price the hub will check.
	audit, err := ciBuildAuditRequest(order.ciDigRequest, ciPolicy{AllowDeep: true, AllowPool: true})
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_order", err.Error(), nil)
		return
	}
	orderJSON, _ := json.Marshal(order)
	base := strings.TrimRight(strings.TrimSpace(a.canonicalChainBaseURL()), "/")
	if base == "" {
		writeAPIError(w, http.StatusConflict, "no_canonical_chain", "this node has no canonical chain to pay on", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	hub, err := a.fetchHubPoolEscrowInfo(ctx, base)
	if err != nil {
		writeAPIError(w, http.StatusConflict, "pool_orders_unavailable", err.Error()+" — run the scan locally instead", nil)
		return
	}
	from := strings.TrimSpace(a.signer.Address())
	price := chain.HMCToUnits(audit.BudgetHMC)
	fee := chain.DefaultTransferMinFee
	bal, nonce, ok := a.fetchCanonicalAddressState(ctx, from)
	if !ok {
		writeAPIError(w, http.StatusBadGateway, "canonical_balance_unavailable", "could not read the on-chain balance", nil)
		return
	}
	if bal < price+fee {
		writeAPIError(w, http.StatusPaymentRequired, "insufficient_on_chain",
			fmt.Sprintf("%s costs %.8f HMC (+%.8f network fee); this wallet has %.8f HMC", audit.DigPackage,
				chain.UnitsToHMC(price), chain.UnitsToHMC(fee), chain.UnitsToHMC(bal)),
			map[string]any{"price_hmc": chain.UnitsToHMC(price), "on_chain_hmc": chain.UnitsToHMC(bal), "address": from})
		return
	}
	campaignID := "campaign-pool-" + time.Now().UTC().Format("20060102t150405") + fmt.Sprintf("%06d", time.Now().Nanosecond()/1000)

	// 1. Pay the hub escrow address on chain.
	tx := chain.TransferTx{
		TxType:        "transfer_v1",
		SigAlg:        chain.TransferSigAlgEd25519,
		From:          from,
		To:            hub.Address,
		AmountUnits:   price,
		FeeUnits:      fee,
		Nonce:         nonce,
		TimestampUnix: time.Now().Unix(),
		Memo:          hub.MemoPrefix + campaignID,
		PubKeyEd25519: strings.TrimSpace(a.signer.PublicKeyHex()),
	}
	tx.ChainID = a.txChainIDForSigning(ctx)
	canon, err := tx.CanonicalBytes()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "invalid_tx_encoding", err.Error(), nil)
		return
	}
	tx.SigEd25519 = strings.TrimSpace(a.signer.SignHex(canon))
	raw, _ := json.Marshal(tx)
	txHash, submitStatus, err := a.relayOrdersFundTransfer(ctx, base, raw)
	if err != nil || txHash == "" {
		writeAPIError(w, http.StatusBadGateway, "payment_submit_failed", fmt.Sprint(err), map[string]any{"status": submitStatus})
		return
	}
	log.Printf("orders pool order: %s paid %.8f HMC to hub escrow (tx %s)", logsafe.ID(campaignID), chain.UnitsToHMC(price), logsafe.ID(txHash))
	// Record the paid order before waiting/claiming: if this request dies (timeout, restart,
	// hub briefly down) the auto runner resumes the claim instead of leaving the money unclaimed.
	// The request itself claims now; keep the auto runner's resume off this order meanwhile.
	a.holdPoolMirrorSync(campaignID, 10*time.Minute)
	defer a.holdPoolMirrorSync(campaignID, 0)
	a.insertPoolOrderMirror(ctx, campaignID, audit, txHash, nil, orderJSON)

	// 2. Wait until it is in a block (the hub only accepts included payments).
	included := false
	for deadline := time.Now().Add(4 * time.Minute); time.Now().Before(deadline) && !included; {
		if m, ferr := a.fetchCanonicalTransferMap(ctx, base, txHash); ferr == nil && m != nil {
			switch strings.ToLower(strings.TrimSpace(fmt.Sprint(m["status"]))) {
			case "included", "applied", "confirmed":
				included = true
				continue
			case "rejected", "failed":
				writeAPIError(w, http.StatusBadGateway, "payment_rejected", fmt.Sprint("payment rejected: ", m["reject_code"]), map[string]any{"tx_hash": txHash})
				return
			}
		}
		select {
		case <-ctx.Done():
			deadline = time.Now()
		case <-time.After(3 * time.Second):
		}
	}
	if !included {
		writeAPIError(w, http.StatusAccepted, "payment_pending",
			"payment sent but not yet in a block; this node claims the order automatically once it is", map[string]any{"campaign_id": campaignID, "tx_hash": txHash})
		return
	}

	// 3. Signed claim → the hub opens the escrow and puts the campaign on the pool.
	res, status, err := a.claimHubPoolEscrow(ctx, base, hub.Path, campaignID, txHash, orderJSON)
	if err != nil {
		if poolClaimTerminal(err) {
			a.markPoolOrderClaimFailed(ctx, campaignID, err)
		}
		writeAPIError(w, http.StatusBadGateway, "hub_claim_failed", err.Error(),
			map[string]any{"campaign_id": campaignID, "tx_hash": txHash, "hub_status": status,
				"note": "the payment is on chain; this node retries the claim, and the hub refunds it if the order cannot be created"})
		return
	}
	a.finishPoolOrderClaim(ctx, campaignID, res)
	delete(res, "orders_owner_secret")
	res["campaign_id"] = campaignID
	res["funding_tx_hash"] = txHash
	res["execution"] = "pool"
	res["report_url"] = base + "/api/fuzz/campaigns/" + url.PathEscape(campaignID) + "/report.html"
	res["gate_url"] = base + "/api/fuzz/campaigns/" + url.PathEscape(campaignID) + "/gate?max_critical=0&max_high=0"
	res["pulse_url"] = base + "/api/fuzz/campaigns/" + url.PathEscape(campaignID) + "/pulse"
	writeJSON(w, res)
}

// claimHubPoolEscrow sends the signed claim, retrying while the hub has not seen the block yet.
func (a *app) claimHubPoolEscrow(ctx context.Context, base, path, campaignID, txHash string, orderJSON []byte) (map[string]any, int, error) {
	from := strings.TrimSpace(a.signer.Address())
	var lastErr error
	lastStatus := 0
	for attempt := 0; attempt < 12; attempt++ {
		ts := time.Now().Unix()
		sig := a.signer.SignHex(ordersPoolEscrowSigningBytes(campaignID, txHash, from, ts, orderJSON))
		body, _ := json.Marshal(map[string]any{
			"campaign_id":          campaignID,
			"owner_address":        from,
			"owner_pubkey_ed25519": strings.TrimSpace(a.signer.PublicKeyHex()),
			"funding_tx_hash":      txHash,
			"timestamp_unix":       ts,
			"sig_ed25519":          sig,
			"order":                json.RawMessage(orderJSON),
		})
		cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
		req, _ := http.NewRequestWithContext(cctx, http.MethodPost, base+path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := netutil.HTTPClientNoRedirect(90 * time.Second).Do(req)
		if err != nil {
			cancel()
			lastErr = err
		} else {
			raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
			resp.Body.Close()
			cancel()
			lastStatus = resp.StatusCode
			var m map[string]any
			_ = json.Unmarshal(raw, &m)
			if resp.StatusCode == http.StatusOK && m != nil {
				return m, resp.StatusCode, nil
			}
			code, _ := m["code"].(string)
			msg, _ := m["error"].(string)
			if msg == "" {
				msg = strings.TrimSpace(string(raw))
			}
			lastErr = fmt.Errorf("hub HTTP %d %s: %s", resp.StatusCode, code, msg)
			if code != "funding_not_found" && code != "funding_not_included" && code != "rate_limited" && resp.StatusCode < 500 {
				return nil, resp.StatusCode, lastErr
			}
		}
		select {
		case <-ctx.Done():
			return nil, lastStatus, lastErr
		case <-time.After(5 * time.Second):
		}
	}
	return nil, lastStatus, lastErr
}

// insertPoolOrderMirror keeps a local row so the dashboard lists the order; progress and
// findings come from the coordinator (owner secret), money lives on the hub.
func (a *app) insertPoolOrderMirror(ctx context.Context, campaignID string, audit securityAuditRequest, txHash string, hubRes map[string]any, orderJSON []byte) {
	if a.db == nil {
		return
	}
	cfg := map[string]any{
		"pool_claim_pending": true,
		"pool_claim_order":   string(orderJSON),
		"pool_distributed":   true,
		"orders_pool_mirror": true, // NOT orders_pool_escrow: that marks the hub-side campaign
		"auto_runner":        "1",
		"dig_package":        audit.DigPackage,
		"guard_pack":         audit.GuardPack,
		"budget_hmc":         audit.BudgetHMC,
		"funding_tx_hash":    txHash,
	}
	sum := map[string]any{
		"execution":            "pool",
		"pool_register_status": "claim_pending",
		"payment":              "paid on chain to the hub escrow",
	}
	_ = hubRes
	now := time.Now().Unix()
	_, err := a.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO fuzz_campaigns
		 (id, campaign_type, status, title, description, owner_ref, task_id, target_ref, budget_runs, budget_seconds, config_json, summary_json, report_token_hash, report_token_issued_at, created_at, started_at, completed_at)
		 VALUES (?, 'property', 'planned', ?, '', ?, '', '', ?, ?, ?, ?, '', 0, ?, ?, 0)`,
		campaignID, audit.Title, strings.TrimSpace(a.signer.Address()), audit.BudgetRuns, audit.BudgetSeconds,
		marshalMapJSON(cfg), marshalMapJSON(sum), now, now)
	if err != nil {
		log.Printf("orders pool order: local mirror %s: %v", logsafe.ID(campaignID), err)
	}
}

// poolEscrowMirrorCampaign is a customer-side row for a hub-funded pool order: never register
// it on the pool from here, never drain settles (the hub owns the escrow).
func poolEscrowMirrorCampaign(cfg map[string]any) bool {
	return truthy(cfg["orders_pool_mirror"])
}

// syncPoolOrderMirror pulls coordinator progress for a paid pool order at most every 30 s.
func (a *app) syncPoolOrderMirror(ctx context.Context, campaignID string, now int64) {
	a.poolSyncMu.Lock()
	if a.poolMirrorSyncAt == nil {
		a.poolMirrorSyncAt = map[string]int64{}
	}
	if now-a.poolMirrorSyncAt[campaignID] < 30 {
		a.poolSyncMu.Unlock()
		return
	}
	a.poolMirrorSyncAt[campaignID] = now
	a.poolSyncMu.Unlock()
	var cfgRaw string
	_ = a.db.QueryRowContext(ctx, `SELECT COALESCE(config_json,'{}') FROM fuzz_campaigns WHERE id=?`, campaignID).Scan(&cfgRaw)
	if cfg := parseMapJSON(cfgRaw); truthy(cfg["pool_claim_pending"]) {
		a.poolSyncMu.Lock()
		a.poolMirrorSyncAt[campaignID] = now + 600 // one resume at a time (claim retries take minutes)
		a.poolSyncMu.Unlock()
		go a.resumePoolOrderClaim(campaignID, cfg)
		return
	}
	go func() {
		cctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := a.syncPoolCampaignProgressFromCoordinator(cctx, campaignID); err != nil {
			log.Printf("orders pool order: progress %s: %v", logsafe.ID(campaignID), err)
		}
	}()
}

// resumePoolOrderClaim finishes a paid pool order whose claim did not complete in the request
// (payment not yet in a block, hub unreachable, node restarted).
func (a *app) resumePoolOrderClaim(campaignID string, cfg map[string]any) {
	defer func() {
		a.poolSyncMu.Lock()
		a.poolMirrorSyncAt[campaignID] = time.Now().Unix() // next try after the normal 30 s
		a.poolSyncMu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	base := strings.TrimRight(strings.TrimSpace(a.canonicalChainBaseURL()), "/")
	txHash := strings.TrimSpace(toString(cfg["funding_tx_hash"]))
	orderJSON := []byte(toString(cfg["pool_claim_order"]))
	if base == "" || txHash == "" || len(orderJSON) == 0 {
		return
	}
	hub, err := a.fetchHubPoolEscrowInfo(ctx, base)
	if err != nil {
		return
	}
	res, _, err := a.claimHubPoolEscrow(ctx, base, hub.Path, campaignID, txHash, orderJSON)
	if err != nil {
		if poolClaimTerminal(err) {
			a.markPoolOrderClaimFailed(ctx, campaignID, err)
		} else {
			log.Printf("orders pool order: claim %s still pending: %s", logsafe.ID(campaignID), logsafe.Err(err))
		}
		return
	}
	a.finishPoolOrderClaim(ctx, campaignID, res)
	log.Printf("orders pool order: %s claimed on the hub (resumed)", logsafe.ID(campaignID))
}

// poolClaimTerminal: the hub refused the order for good (its sweep / failure path refunds the
// funding). Transient answers (not yet included, rate limit, 5xx, network) are retried.
func poolClaimTerminal(err error) bool {
	if err == nil {
		return false
	}
	m := err.Error()
	for _, c := range []string{"funding_refunded", "funding_used", "funding_wrong_", "funding_too_small", "invalid_order",
		"invalid_campaign_id", "bad_signature", "invalid_request"} {
		if strings.Contains(m, c) {
			return true
		}
	}
	return false
}

func (a *app) finishPoolOrderClaim(ctx context.Context, campaignID string, res map[string]any) {
	if secret, _ := res["orders_owner_secret"].(string); strings.TrimSpace(secret) != "" {
		_ = poolsync.PersistOrdersOwnerSecret(campaignID, secret)
	}
	var cfgRaw, sumRaw string
	if err := a.db.QueryRowContext(ctx, `SELECT COALESCE(config_json,'{}'), COALESCE(summary_json,'{}') FROM fuzz_campaigns WHERE id=?`, campaignID).Scan(&cfgRaw, &sumRaw); err != nil {
		return
	}
	cfg, sum := parseMapJSON(cfgRaw), parseMapJSON(sumRaw)
	delete(cfg, "pool_claim_pending")
	delete(cfg, "pool_claim_order")
	sum["pool_register_status"] = "ok"
	if tok, ok := res["customer_report_token"].(string); ok && len(tok) > 6 {
		sum["hub_report_token_hint"] = tok[:6] + "…" // never the full token at rest
	}
	_, _ = a.db.ExecContext(ctx, `UPDATE fuzz_campaigns SET status='running', config_json=?, summary_json=? WHERE id=? AND status IN ('planned','running')`,
		marshalMapJSON(cfg), marshalMapJSON(sum), campaignID)
}

func (a *app) markPoolOrderClaimFailed(ctx context.Context, campaignID string, cause error) {
	var cfgRaw, sumRaw string
	if err := a.db.QueryRowContext(ctx, `SELECT COALESCE(config_json,'{}'), COALESCE(summary_json,'{}') FROM fuzz_campaigns WHERE id=?`, campaignID).Scan(&cfgRaw, &sumRaw); err != nil {
		return
	}
	cfg, sum := parseMapJSON(cfgRaw), parseMapJSON(sumRaw)
	delete(cfg, "pool_claim_pending")
	delete(cfg, "pool_claim_order")
	sum["pool_register_status"] = "refused"
	sum["pool_register_error"] = logsafe.Err(cause)
	sum["payment"] = "the hub did not create this order; it refunds the payment on chain"
	_, _ = a.db.ExecContext(ctx, `UPDATE fuzz_campaigns SET status='cancelled', completed_at=?, config_json=?, summary_json=? WHERE id=?`,
		time.Now().Unix(), marshalMapJSON(cfg), marshalMapJSON(sum), campaignID)
	log.Printf("orders pool order: %s refused by the hub: %s", logsafe.ID(campaignID), logsafe.Err(cause))
}

// holdPoolMirrorSync defers the next mirror sync/claim resume of campaignID by d (0 = normal
// 30 s cadence from now).
func (a *app) holdPoolMirrorSync(campaignID string, d time.Duration) {
	a.poolSyncMu.Lock()
	defer a.poolSyncMu.Unlock()
	if a.poolMirrorSyncAt == nil {
		a.poolMirrorSyncAt = map[string]int64{}
	}
	a.poolMirrorSyncAt[campaignID] = time.Now().Add(d).Unix()
}
