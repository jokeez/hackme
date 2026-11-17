package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"hackme/internal/chain"
	"hackme/internal/netutil"
	"hackme/internal/poolsync"
)

// Hub pool Orders API (rc20): customer Fund confirm + Dig/Hunt start against tenant prepaid.
// Auth: fleet publish token + X-Hackme-Orders-Owner-Ref (= customer wallet). Escrow and
// hubv1 mint live only on this node — customer Orders never opens local escrow for the pool path.

func (a *app) handleOrdersPoolAPI(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/orders/pool")
	path = strings.Trim(path, "/")
	switch {
	case path == "dig" && r.Method == http.MethodPost:
		a.handleOrdersPoolDig(w, r)
	case path == "hunt" && r.Method == http.MethodPost:
		a.handleOrdersPoolHunt(w, r)
	case path == "fund/confirm" && r.Method == http.MethodPost:
		a.handleOrdersPoolFundConfirm(w, r)
	case path == "cancel" && r.Method == http.MethodPost:
		a.handleOrdersPoolCancel(w, r)
	case path == "finalize" && r.Method == http.MethodPost:
		a.handleOrdersPoolFinalize(w, r)
	case path == "prepaid" && r.Method == http.MethodGet:
		a.handleOrdersPoolPrepaidStatus(w, r)
	default:
		writeAPIError(w, http.StatusNotFound, "not_found", "unknown orders pool route", nil)
	}
}

func ordersPoolPublishTokenOK(got string) bool {
	want := poolsync.OrdersPublishToken()
	if want == "" || got == "" {
		return false
	}
	if len(want) != len(got) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

func extractOrdersPoolSecret(r *http.Request) string {
	if s := strings.TrimSpace(r.Header.Get("X-Hackme-Admin-Token")); s != "" {
		return s
	}
	const prefix = "Bearer "
	auth := r.Header.Get("Authorization")
	if len(auth) > len(prefix) && strings.EqualFold(auth[:len(prefix)], prefix) {
		return strings.TrimSpace(auth[len(prefix):])
	}
	return ""
}

func ordersPoolOwnerRef(r *http.Request) string {
	return chain.OrdersPrepaidNormalizeAddr(strings.TrimSpace(r.Header.Get("X-Hackme-Orders-Owner-Ref")))
}

// requireOrdersPoolPublishAuth gates hub pool routes: publish token + Owner-Ref wallet.
func (a *app) requireOrdersPoolPublishAuth(w http.ResponseWriter, r *http.Request) (owner string, ok bool) {
	if !ordersPoolPublishTokenOK(extractOrdersPoolSecret(r)) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-orders-pool"`)
		writeAPIError(w, http.StatusUnauthorized, "auth_required", "orders publish token required", nil)
		return "", false
	}
	owner = ordersPoolOwnerRef(r)
	if owner == "" || !chain.IsValidAddress(owner) {
		writeAPIError(w, http.StatusBadRequest, "owner_ref_required",
			"X-Hackme-Orders-Owner-Ref must be the customer HMC wallet", nil)
		return "", false
	}
	return owner, true
}

func (a *app) handleOrdersPoolPrepaidStatus(w http.ResponseWriter, r *http.Request) {
	if !a.allowRate("orders_pool_prepaid:"+clientIP(r), 30) {
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "rate limited", nil)
		return
	}
	owner, ok := a.requireOrdersPoolPublishAuth(w, r)
	if !ok {
		return
	}
	if !a.requireOrdersPoolOwnerProof(w, r, owner) {
		return
	}
	hmc, err := a.chain.GetOrdersPrepaidHMC(r.Context(), owner)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "prepaid_status_failed", err.Error(), nil)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "owner_ref": owner, "spendable_hmc": hmc})
}

func (a *app) handleOrdersPoolFundConfirm(w http.ResponseWriter, r *http.Request) {
	if !a.allowRate("orders_pool_fund_confirm:"+clientIP(r), 20) {
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "rate limited", nil)
		return
	}
	owner, ok := a.requireOrdersPoolPublishAuth(w, r)
	if !ok {
		return
	}
	if !a.requireOrdersPoolOwnerProof(w, r, owner) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	defer r.Body.Close()
	var req ordersFundConfirmRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid json", nil)
		return
	}
	txHash := strings.TrimSpace(strings.ToLower(req.TxHash))
	if txHash == "" {
		writeAPIError(w, http.StatusBadRequest, "tx_hash_required", "tx_hash required", nil)
		return
	}
	amountUnits := req.AmountUnits
	if amountUnits == 0 && req.AmountHMC > 0 {
		amountUnits = chain.HMCToUnits(req.AmountHMC)
	}
	row, found, err := a.chain.TransferTxByHash(r.Context(), txHash)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "fund_verify_failed", err.Error(), nil)
		return
	}
	if !found {
		writeAPIError(w, http.StatusNotFound, "fund_tx_not_found", "transfer not found on hub chain", map[string]any{"tx_hash": txHash})
		return
	}
	if amountUnits == 0 {
		amountUnits = row.AmountUnits
	}
	if err := a.verifyHubOrdersFundTransfer(row, owner, amountUnits); err != nil {
		writeAPIError(w, http.StatusBadRequest, "fund_tx_mismatch", err.Error(), map[string]any{"tx_hash": txHash})
		return
	}
	status := strings.ToLower(strings.TrimSpace(row.Status))
	if status != "included" && status != "applied" && status != "confirmed" {
		writeAPIError(w, http.StatusConflict, "fund_not_included", "transfer not yet included", map[string]any{
			"tx_hash": txHash,
			"status":  status,
		})
		return
	}
	res, err := a.chain.CreditOrdersFundForOwner(r.Context(), owner, txHash, amountUnits)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "fund_credit_failed", err.Error(), map[string]any{"tx_hash": txHash})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "funded": res, "tx_hash": txHash, "hub_prepaid": true})
}

func (a *app) verifyHubOrdersFundTransfer(row chain.TransferStatusRow, owner string, amountUnits uint64) error {
	owner = chain.OrdersPrepaidNormalizeAddr(owner)
	gotFrom := chain.OrdersPrepaidNormalizeAddr(row.From)
	if !strings.EqualFold(gotFrom, owner) {
		return fmt.Errorf("from mismatch: %s want %s", row.From, owner)
	}
	if !strings.EqualFold(strings.TrimSpace(row.To), chain.DevFeeAddress) {
		return fmt.Errorf("to must be platform treasury %s", chain.DevFeeAddress)
	}
	if amountUnits > 0 && row.AmountUnits != amountUnits {
		return fmt.Errorf("amount_units mismatch: %d want %d", row.AmountUnits, amountUnits)
	}
	if !chain.MemoIsOrdersFund(row.Memo) {
		return fmt.Errorf("memo must start with %s (got %q)", chain.OrdersFundMemoPrefix, row.Memo)
	}
	if memoOwner := chain.OrdersFundOwnerFromMemo(row.Memo); memoOwner != "" && memoOwner != owner {
		return fmt.Errorf("memo owner mismatch: %s want %s", memoOwner, owner)
	}
	return nil
}

func (a *app) handleOrdersPoolDig(w http.ResponseWriter, r *http.Request) {
	if !a.allowRate("orders_pool_dig:"+clientIP(r), 6) {
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "rate limited", nil)
		return
	}
	owner, ok := a.requireOrdersPoolPublishAuth(w, r)
	if !ok {
		return
	}
	if !a.requireOrdersPoolOwnerProof(w, r, owner) {
		return
	}
	if !a.ordersPoolHubCapable() {
		writeAPIError(w, http.StatusServiceUnavailable, "pool_unavailable",
			"hub pool Dig requires minting hub + prepaid secret", nil)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	defer r.Body.Close()
	var req securityAuditRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid json", nil)
		return
	}
	trueVal := true
	req.PoolDistributed = &trueVal
	req.PayerRef = owner
	req.PayerWallet = owner
	a.serveSecurityAuditPoolHub(w, r, req, owner)
}

func (a *app) handleOrdersPoolHunt(w http.ResponseWriter, r *http.Request) {
	if !a.allowRate("orders_pool_hunt:"+clientIP(r), 6) {
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "rate limited", nil)
		return
	}
	owner, ok := a.requireOrdersPoolPublishAuth(w, r)
	if !ok {
		return
	}
	if !a.requireOrdersPoolOwnerProof(w, r, owner) {
		return
	}
	if !a.ordersPoolHubCapable() {
		writeAPIError(w, http.StatusServiceUnavailable, "pool_unavailable",
			"hub pool Hunt requires minting hub + prepaid secret", nil)
		return
	}
	a.serveHuntCampaignCreatePoolHub(w, r, owner)
}

func (a *app) ordersPoolHubCapable() bool {
	if a == nil || a.chain == nil {
		return false
	}
	// Explicit minting-hub role only — a leaked prepaid secret on a customer node
	// must not enable hub Dig/Hunt handlers.
	if !envBool("HACKME_CHAIN_MINTING_HUB", false) {
		return false
	}
	return poolsync.OrdersPrepaidHubSecret() != ""
}

// registerPoolCampaignOnCoordinator mints hubv1 for a live hub escrow and registers once.
func (a *app) registerPoolCampaignOnCoordinator(ctx context.Context, campaignID, ownerRef, ctype string, budgetRuns, budgetSeconds int, budgetHMC float64, cfg map[string]any) (poolsync.RegisterResult, error) {
	hubSec := poolsync.OrdersPrepaidHubSecret()
	if hubSec == "" {
		return poolsync.RegisterResult{}, errors.New("hub prepaid secret not configured")
	}
	escrowFP := poolsync.EscrowPrepaidFingerprintParts(campaignID, budgetHMC, budgetRuns, "open")
	exp := time.Now().Unix() + poolsync.DefaultHubPrepaidTicketTTL
	ticket := poolsync.SignHubPrepaidTicket(hubSec, ownerRef, campaignID, budgetHMC, budgetRuns, escrowFP, exp)
	if ticket == "" {
		return poolsync.RegisterResult{}, errors.New("hub prepaid ticket sign failed")
	}
	if cfg == nil {
		cfg = map[string]any{}
	}
	cfg["pool_distributed"] = true
	cfg["hub_pool"] = true
	cfg["escrow_fingerprint"] = escrowFP
	// Settlements apply on this hub node (coordinator ORDERS_URL → loopback :18080).
	if base, pull := poolsync.ResolveOrdersSettleBase(); base != "" {
		cfg["orders_settle_base"] = base
		if pull {
			cfg["orders_settle_pull"] = true
		}
	}
	req := poolsync.RegisterRequest{
		ID:                 campaignID,
		CampaignType:       ctype,
		Title:              toString(cfg["title"]),
		OwnerRef:           ownerRef,
		Status:             "running",
		BudgetRuns:         budgetRuns,
		BudgetSeconds:      budgetSeconds,
		BudgetHMC:          budgetHMC,
		PrepaidAttestation: ticket,
		EscrowFingerprint:  escrowFP,
		Config:             cfg,
	}
	if req.Title == "" {
		req.Title = campaignID
	}
	coordURL := poolsync.ResolveCoordinatorURL()
	token := poolsync.OrdersCustomerToken()
	if token == "" {
		token = poolsync.CoordinatorAdminToken()
	}
	_, res, err := poolsync.RegisterOnce(ctx, coordURL, token, req)
	return res, err
}

// ensureOrdersPrepaidForBudget checks hub tenant prepaid covers needHMC (no local wallet debit).
func (a *app) ensureOrdersPrepaidForBudget(ctx context.Context, owner string, needHMC float64) error {
	owner = chain.OrdersPrepaidNormalizeAddr(owner)
	bal, err := a.chain.GetOrdersPrepaidHMC(ctx, owner)
	if err != nil {
		return &ordersAutoFundError{
			HTTPStatus: http.StatusInternalServerError,
			Code:       "prepaid_status_failed",
			Msg:        err.Error(),
		}
	}
	if bal+1e-9 >= needHMC {
		return nil
	}
	return &ordersAutoFundError{
		HTTPStatus: http.StatusPaymentRequired,
		Code:       "insufficient_prepaid",
		Msg: fmt.Sprintf("hub prepaid %.4f HMC < need %.4f — Fund Orders then confirm on hub",
			bal, needHMC),
		Extra: map[string]any{
			"spendable_hmc": bal,
			"need_hmc":      needHMC,
			"owner_ref":     owner,
			"action":        "fund_orders_then_hub_confirm",
		},
	}
}

func openFuzzEscrowForOwnerRetry(ctx context.Context, ch *chain.Service, campaignID, owner string, budgetHMC float64, budgetRuns int, escrowSplit string) (*chain.FuzzEscrowRow, error) {
	var escrow *chain.FuzzEscrowRow
	var err error
	for attempt := 0; attempt < 6; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*50) * time.Millisecond)
		}
		escrow, err = ch.OpenFuzzEscrowForOwner(ctx, campaignID, owner, budgetHMC, budgetRuns, escrowSplit, chain.FeeAssetHMC)
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "database is locked") {
			return escrow, err
		}
	}
	return escrow, err
}

// ordersAllowLocalFallback is the lab-only escape hatch (default off for product pool).
func ordersAllowLocalFallback() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_ORDERS_ALLOW_LOCAL_FALLBACK")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func (a *app) handleOrdersPoolCancel(w http.ResponseWriter, r *http.Request) {
	if !a.allowRate("orders_pool_cancel:"+clientIP(r), 10) {
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "rate limited", nil)
		return
	}
	owner, ok := a.requireOrdersPoolPublishAuth(w, r)
	if !ok {
		return
	}
	if !a.requireOrdersPoolOwnerProof(w, r, owner) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	defer r.Body.Close()
	var req struct {
		CampaignID string `json:"campaign_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid json", nil)
		return
	}
	campaignID := cleanFuzzID(strings.TrimSpace(req.CampaignID), "campaign")
	if campaignID == "" {
		writeAPIError(w, http.StatusBadRequest, "campaign_id_required", "campaign_id required", nil)
		return
	}
	c, err := a.getFuzzCampaign(r.Context(), campaignID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "campaign_not_found", "campaign not found", nil)
		return
	}
	campOwner := chain.OrdersPrepaidNormalizeAddr(strings.TrimSpace(c.OwnerRef))
	if campOwner == "" || campOwner != owner {
		writeAPIError(w, http.StatusForbidden, "owner_mismatch", "campaign owner_ref does not match Owner-Ref", nil)
		return
	}
	// Stop miners first — do not mark cancelled until coordinator accepts.
	if cerr := a.pushCoordinatorCampaignStatus(r.Context(), campaignID, "cancelled"); cerr != nil {
		writeAPIError(w, http.StatusBadGateway, "pool_cancel_failed",
			"coordinator cancel failed: "+cerr.Error(), nil)
		return
	}
	// Pull settle then refund unused escrow to tenant prepaid (authoritative money path).
	a.tryCloseFuzzEscrowForStatus(r.Context(), campaignID, "cancelled")

	now := time.Now().Unix()
	_, _ = a.db.ExecContext(r.Context(),
		`UPDATE fuzz_campaigns SET status='cancelled',
		 completed_at=CASE WHEN completed_at=0 THEN ? ELSE completed_at END WHERE id=?`,
		now, campaignID)

	prepaid, _ := a.chain.GetOrdersPrepaidHMC(r.Context(), owner)
	escrow, _ := a.chain.GetFuzzEscrow(r.Context(), campaignID)
	writeJSON(w, map[string]any{
		"ok":            true,
		"campaign_id":   campaignID,
		"status":        "cancelled",
		"hub_pool":      true,
		"spendable_hmc": prepaid,
		"escrow":        escrow,
	})
}

func (a *app) handleOrdersPoolFinalize(w http.ResponseWriter, r *http.Request) {
	if !a.allowRate("orders_pool_finalize:"+clientIP(r), 10) {
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "rate limited", nil)
		return
	}
	owner, ok := a.requireOrdersPoolPublishAuth(w, r)
	if !ok {
		return
	}
	if !a.requireOrdersPoolOwnerProof(w, r, owner) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	defer r.Body.Close()
	var req struct {
		CampaignID string `json:"campaign_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid json", nil)
		return
	}
	campaignID := cleanFuzzID(strings.TrimSpace(req.CampaignID), "campaign")
	if campaignID == "" {
		writeAPIError(w, http.StatusBadRequest, "campaign_id_required", "campaign_id required", nil)
		return
	}
	c, err := a.getFuzzCampaign(r.Context(), campaignID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "campaign_not_found", "campaign not found", nil)
		return
	}
	campOwner := chain.OrdersPrepaidNormalizeAddr(strings.TrimSpace(c.OwnerRef))
	if campOwner == "" || campOwner != owner {
		writeAPIError(w, http.StatusForbidden, "owner_mismatch", "campaign owner_ref does not match Owner-Ref", nil)
		return
	}
	if cerr := a.pushCoordinatorCampaignStatus(r.Context(), campaignID, "completed"); cerr != nil {
		writeAPIError(w, http.StatusBadGateway, "pool_finalize_failed",
			"coordinator complete failed: "+cerr.Error(), nil)
		return
	}
	a.tryCloseFuzzEscrowForStatus(r.Context(), campaignID, "completed")
	now := time.Now().Unix()
	_, _ = a.db.ExecContext(r.Context(),
		`UPDATE fuzz_campaigns SET status='completed',
		 completed_at=CASE WHEN completed_at=0 THEN ? ELSE completed_at END WHERE id=?`,
		now, campaignID)
	prepaid, _ := a.chain.GetOrdersPrepaidHMC(r.Context(), owner)
	escrow, _ := a.chain.GetFuzzEscrow(r.Context(), campaignID)
	writeJSON(w, map[string]any{
		"ok":            true,
		"campaign_id":   campaignID,
		"status":        "completed",
		"hub_pool":      true,
		"spendable_hmc": prepaid,
		"escrow":        escrow,
	})
}

func (a *app) pushCoordinatorCampaignStatus(ctx context.Context, campaignID, status string) error {
	coordURL := poolsync.ResolveCoordinatorURL()
	token := poolsync.CoordinatorAdminToken()
	if coordURL == "" || token == "" {
		return fmt.Errorf("coordinator admin URL/token not configured")
	}
	body, _ := json.Marshal(map[string]any{"id": campaignID, "status": status})
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, coordURL+"/api/fuzz/pool/campaigns/status", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Hackme-Admin-Token", token)
	resp, err := netutil.HTTPClientPoolOriginDial(30 * time.Second).Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}
