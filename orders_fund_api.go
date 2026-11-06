package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"hackme/internal/chain"
	"hackme/internal/netutil"
)

const ordersFundMemoMaxWait = 75 * time.Second

func (a *app) handleOrdersFundAPI(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/orders/fund")
	path = strings.Trim(path, "/")
	switch {
	case path == "" && r.Method == http.MethodPost:
		a.handleOrdersFund(w, r)
	case path == "confirm" && r.Method == http.MethodPost:
		a.handleOrdersFundConfirm(w, r)
	case path != "" && r.Method == http.MethodGet:
		a.handleOrdersFundStatus(w, r, path)
	default:
		writeAPIError(w, http.StatusNotFound, "not_found", "unknown orders fund route", nil)
	}
}

type ordersFundRequest struct {
	AmountHMC float64 `json:"amount_hmc"`
}

type ordersFundConfirmRequest struct {
	TxHash      string  `json:"tx_hash"`
	AmountHMC   float64 `json:"amount_hmc,omitempty"`
	AmountUnits uint64  `json:"amount_units,omitempty"`
}

func (a *app) requireOrdersFundAuth(w http.ResponseWriter, r *http.Request) bool {
	return requireAdminAuthOrDesktopLoopback(w, r)
}

func (a *app) ordersFundCanonicalBase() (string, error) {
	base := strings.TrimRight(strings.TrimSpace(a.canonicalChainBaseURL()), "/")
	if !walletCanonicalBaseUsable(base) {
		return "", fmt.Errorf("canonical chain URL required for Fund Orders")
	}
	if !a.shouldUseCanonicalChainAPI() && !a.networkModeActive() {
		// Chain host shares one ledger — Fund Orders is for desktop followers.
		return "", fmt.Errorf("Fund Orders is for desktop Orders followers with canonical chain")
	}
	return base, nil
}

// ordersFundOutcome is the result of a successful or pending fund attempt.
type ordersFundOutcome struct {
	Funded      *chain.OrdersFundResult
	TxHash      string
	OnChain     map[string]any
	Pending     bool
	AmountHMC   float64
	AmountUnits uint64
}

// fundOrdersSpendable submits Orders→DevFee on canonical and credits local spendable when included.
// Callers must enforce auth. Returns outcome.Pending when inclusion timed out (confirm later).
func (a *app) fundOrdersSpendable(ctx context.Context, amountHMC float64) (*ordersFundOutcome, error) {
	if err := chain.ValidateOrdersFundAmountHMC(amountHMC); err != nil {
		return nil, &ordersAutoFundError{
			HTTPStatus: http.StatusBadRequest,
			Code:       "invalid_amount",
			Msg:        err.Error(),
		}
	}
	if a.signer == nil {
		return nil, &ordersAutoFundError{
			HTTPStatus: http.StatusBadRequest,
			Code:       "signer_unavailable",
			Msg:        "node signer unavailable",
		}
	}
	base, err := a.ordersFundCanonicalBase()
	if err != nil {
		return nil, &ordersAutoFundError{
			HTTPStatus: http.StatusBadRequest,
			Code:       "canonical_required",
			Msg:        err.Error(),
		}
	}
	from := strings.TrimSpace(a.signer.Address())
	amountUnits := chain.HMCToUnits(amountHMC)
	fee := chain.DefaultTransferMinFee
	need := amountUnits + fee

	addrCtx, addrCancel := context.WithTimeout(ctx, 10*time.Second)
	balUnits, nonce, ok := a.fetchCanonicalAddressState(addrCtx, from)
	addrCancel()
	if !ok {
		return nil, &ordersAutoFundError{
			HTTPStatus: http.StatusBadGateway,
			Code:       "canonical_balance_unavailable",
			Msg:        "could not read on-chain balance",
		}
	}
	if balUnits < need {
		return nil, &ordersAutoFundError{
			HTTPStatus: http.StatusPaymentRequired,
			Code:       "insufficient_on_chain",
			Msg: fmt.Sprintf("on-chain balance %.8f HMC < need %.8f HMC (amount+fee) — top up Wallet (#wallet) first",
				chain.UnitsToHMC(balUnits), chain.UnitsToHMC(need)),
			Extra: map[string]any{
				"on_chain_hmc":   chain.UnitsToHMC(balUnits),
				"amount_hmc":     amountHMC,
				"fee_units":      fee,
				"orders_address": from,
			},
		}
	}

	tx := chain.TransferTx{
		TxType:        "transfer_v1",
		SigAlg:        chain.TransferSigAlgEd25519,
		From:          from,
		To:            chain.DevFeeAddress,
		AmountUnits:   amountUnits,
		FeeUnits:      fee,
		Nonce:         nonce,
		TimestampUnix: time.Now().Unix(),
		Memo:          chain.OrdersFundMemo(from),
		PubKeyEd25519: strings.TrimSpace(a.signer.PublicKeyHex()),
	}
	tx.ChainID = a.txChainIDForSigning(ctx) // hub's network id ("" before Economics v2)
	canon, err := tx.CanonicalBytes()
	if err != nil {
		return nil, &ordersAutoFundError{
			HTTPStatus: http.StatusBadRequest,
			Code:       "invalid_tx_encoding",
			Msg:        "invalid tx canonical payload",
		}
	}
	tx.SigEd25519 = strings.TrimSpace(a.signer.SignHex(canon))
	submitRaw, err := json.Marshal(tx)
	if err != nil {
		return nil, &ordersAutoFundError{
			HTTPStatus: http.StatusBadRequest,
			Code:       "invalid_tx_encoding",
			Msg:        "invalid tx json",
		}
	}

	txHash, submitStatus, submitErr := a.relayOrdersFundTransfer(ctx, base, submitRaw)
	if submitErr != nil {
		return nil, &ordersAutoFundError{
			HTTPStatus: http.StatusBadGateway,
			Code:       "fund_submit_failed",
			Msg:        submitErr.Error(),
			Extra:      map[string]any{"status": submitStatus},
		}
	}
	if txHash == "" {
		return nil, &ordersAutoFundError{
			HTTPStatus: http.StatusBadGateway,
			Code:       "fund_submit_failed",
			Msg:        "canonical submit returned empty tx_hash",
		}
	}

	included, row, waitErr := a.waitCanonicalOrdersFundTx(ctx, base, txHash, amountUnits, from)
	if waitErr != nil {
		return nil, &ordersAutoFundError{
			HTTPStatus: http.StatusBadGateway,
			Code:       "fund_verify_failed",
			Msg:        waitErr.Error(),
			Extra:      map[string]any{"tx_hash": txHash},
		}
	}
	out := &ordersFundOutcome{
		TxHash:      txHash,
		OnChain:     row,
		AmountHMC:   amountHMC,
		AmountUnits: amountUnits,
	}
	if !included {
		out.Pending = true
		return out, nil
	}
	res, err := a.chain.CreditOrdersFund(ctx, from, txHash, amountUnits)
	if err != nil {
		return nil, &ordersAutoFundError{
			HTTPStatus: http.StatusInternalServerError,
			Code:       "fund_credit_failed",
			Msg:        err.Error(),
			Extra:      map[string]any{"tx_hash": txHash},
		}
	}
	out.Funded = res
	return out, nil
}

func (a *app) handleOrdersFund(w http.ResponseWriter, r *http.Request) {
	if !a.allowRate("orders_fund:"+clientIP(r), 10) {
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "rate limited", nil)
		return
	}
	if !a.requireOrdersFundAuth(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	defer r.Body.Close()
	var req ordersFundRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid json", nil)
		return
	}
	out, err := a.fundOrdersSpendable(r.Context(), req.AmountHMC)
	if err != nil {
		writeOrdersAutoFundFailed(w, err)
		return
	}
	if out.Pending {
		writeJSON(w, map[string]any{
			"ok":           false,
			"code":         "fund_pending_confirm",
			"error":        "transfer submitted; wait for inclusion then POST /api/orders/fund/confirm",
			"tx_hash":      out.TxHash,
			"status":       out.OnChain["status"],
			"amount_hmc":   out.AmountHMC,
			"amount_units": out.AmountUnits,
		})
		return
	}
	writeJSON(w, map[string]any{
		"ok":       true,
		"funded":   out.Funded,
		"tx_hash":  out.TxHash,
		"on_chain": out.OnChain,
	})
}

func (a *app) handleOrdersFundConfirm(w http.ResponseWriter, r *http.Request) {
	if !a.allowRate("orders_fund_confirm:"+clientIP(r), 20) {
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "rate limited", nil)
		return
	}
	if !a.requireOrdersFundAuth(w, r) {
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
	if a.signer == nil {
		writeAPIError(w, http.StatusBadRequest, "signer_unavailable", "node signer unavailable", nil)
		return
	}
	from := strings.TrimSpace(a.signer.Address())
	base, err := a.ordersFundCanonicalBase()
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "canonical_required", err.Error(), nil)
		return
	}
	amountUnits := req.AmountUnits
	if amountUnits == 0 && req.AmountHMC > 0 {
		amountUnits = chain.HMCToUnits(req.AmountHMC)
	}
	row, err := a.fetchCanonicalTransferMap(r.Context(), base, txHash)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "fund_verify_failed", err.Error(), nil)
		return
	}
	if amountUnits == 0 {
		amountUnits = parseUint64Any(row["amount_units"])
	}
	if err := a.verifyCanonicalOrdersFundTx(row, from, amountUnits); err != nil {
		writeAPIError(w, http.StatusBadRequest, "fund_tx_mismatch", err.Error(), map[string]any{"tx_hash": txHash})
		return
	}
	status := strings.ToLower(strings.TrimSpace(fmt.Sprint(row["status"])))
	if status != "included" && status != "applied" && status != "confirmed" {
		writeAPIError(w, http.StatusConflict, "fund_not_included", "transfer not yet included on canonical chain", map[string]any{
			"tx_hash": txHash,
			"status":  status,
		})
		return
	}
	res, err := a.chain.CreditOrdersFund(r.Context(), from, txHash, amountUnits)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "fund_credit_failed", err.Error(), map[string]any{"tx_hash": txHash})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "funded": res, "tx_hash": txHash, "on_chain": row})
}

func (a *app) handleOrdersFundStatus(w http.ResponseWriter, r *http.Request, txHash string) {
	if !a.requireOrdersFundAuth(w, r) {
		return
	}
	txHash = strings.TrimSpace(strings.ToLower(txHash))
	prev, ok, err := a.chain.OrdersFundAlreadyApplied(r.Context(), txHash)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "status_failed", err.Error(), nil)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "credited": ok, "funded": prev, "tx_hash": txHash})
}

func (a *app) relayOrdersFundTransfer(ctx context.Context, base string, submitRaw []byte) (txHash, status string, err error) {
	txTimeout := 12 * time.Second
	forwardCtx, cancel := context.WithTimeout(ctx, txTimeout+4*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(forwardCtx, http.MethodPost, base+"/api/tx/send", bytes.NewReader(submitRaw))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	client := netutil.HTTPClientNoRedirect(txTimeout)
	resp, err := client.Do(req)
	var body []byte
	var code int
	if err == nil && resp != nil {
		defer resp.Body.Close()
		body, _ = io.ReadAll(resp.Body)
		code = resp.StatusCode
	}
	if code == 0 {
		curlCtx, curlCancel := context.WithTimeout(ctx, 14*time.Second)
		st, bod, cerr := postJSONViaCurl(curlCtx, base+"/api/tx/send", submitRaw, map[string]string{"Content-Type": "application/json"})
		curlCancel()
		if cerr != nil {
			return "", "", fmt.Errorf("canonical submit unreachable: %v", err)
		}
		code, body = st, bod
	}
	var parsed map[string]any
	_ = json.Unmarshal(body, &parsed)
	if code < 200 || code >= 300 {
		msg := strings.TrimSpace(string(body))
		if parsed != nil {
			if e, ok := parsed["error"].(string); ok && e != "" {
				msg = e
			}
			if c, ok := parsed["code"].(string); ok && c != "" {
				status = c
			}
		}
		if msg == "" {
			msg = fmt.Sprintf("canonical HTTP %d", code)
		}
		return "", status, fmt.Errorf("%s", msg)
	}
	txHash = strings.TrimSpace(strings.ToLower(fmt.Sprint(parsed["tx_hash"])))
	status = strings.TrimSpace(fmt.Sprint(parsed["status"]))
	return txHash, status, nil
}

func (a *app) waitCanonicalOrdersFundTx(ctx context.Context, base, txHash string, amountUnits uint64, from string) (included bool, row map[string]any, err error) {
	deadline := time.Now().Add(ordersFundMemoMaxWait)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	var last map[string]any
	for time.Now().Before(deadline) {
		m, ferr := a.fetchCanonicalTransferMap(ctx, base, txHash)
		if ferr == nil && m != nil {
			last = m
			if err := a.verifyCanonicalOrdersFundTx(m, from, amountUnits); err != nil {
				return false, m, err
			}
			st := strings.ToLower(strings.TrimSpace(fmt.Sprint(m["status"])))
			if st == "included" || st == "applied" || st == "confirmed" {
				return true, m, nil
			}
		}
		select {
		case <-ctx.Done():
			return false, last, nil
		case <-time.After(2 * time.Second):
		}
	}
	return false, last, nil
}

func (a *app) fetchCanonicalTransferMap(ctx context.Context, base, txHash string) (map[string]any, error) {
	u := base + "/api/tx/" + url.PathEscape(txHash)
	reqCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := netutil.HTTPClientNoRedirect(8 * time.Second).Do(req)
	if err == nil && resp != nil {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusOK {
			var m map[string]any
			if json.Unmarshal(body, &m) == nil {
				return m, nil
			}
		}
		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("tx not found")
		}
	}
	curlCtx, curlCancel := context.WithTimeout(ctx, 8*time.Second)
	parsed, curlErr := fetchJSONViaCurl(curlCtx, u, nil)
	curlCancel()
	if curlErr != nil {
		return nil, fmt.Errorf("canonical tx lookup failed")
	}
	return parsed, nil
}

func (a *app) verifyCanonicalOrdersFundTx(row map[string]any, from string, amountUnits uint64) error {
	if row == nil {
		return fmt.Errorf("empty transfer")
	}
	gotFrom := strings.TrimSpace(fmt.Sprint(row["from"]))
	if gotFrom == "" {
		gotFrom = strings.TrimSpace(fmt.Sprint(row["from_address"]))
	}
	gotTo := strings.TrimSpace(fmt.Sprint(row["to"]))
	if gotTo == "" {
		gotTo = strings.TrimSpace(fmt.Sprint(row["to_address"]))
	}
	gotAmount := parseUint64Any(row["amount_units"])
	memo := canonicalTransferMemo(row)
	if !strings.EqualFold(gotFrom, from) {
		return fmt.Errorf("from mismatch: %s want %s", gotFrom, from)
	}
	if !strings.EqualFold(gotTo, chain.DevFeeAddress) {
		return fmt.Errorf("to must be platform treasury %s", chain.DevFeeAddress)
	}
	if amountUnits > 0 && gotAmount != amountUnits {
		return fmt.Errorf("amount_units mismatch: %d want %d", gotAmount, amountUnits)
	}
	// Require Orders fund memo always. Empty/missing memo used to pass confirm and
	// could credit any DevFee transfer of matching amount (audit tip 3415c68 H3).
	if !chain.MemoIsOrdersFund(memo) {
		return fmt.Errorf("memo must start with %s (got %q)", chain.OrdersFundMemoPrefix, memo)
	}
	return nil
}

// canonicalTransferMemo reads memo from GET /api/tx/{hash} JSON (missing key must not become "<nil>").
func canonicalTransferMemo(row map[string]any) string {
	if row == nil {
		return ""
	}
	v, ok := row["memo"]
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func parseUint64Any(v any) uint64 {
	switch t := v.(type) {
	case float64:
		if t < 0 {
			return 0
		}
		return uint64(t)
	case int64:
		if t < 0 {
			return 0
		}
		return uint64(t)
	case json.Number:
		n, _ := t.Int64()
		if n < 0 {
			return 0
		}
		return uint64(n)
	case string:
		var n uint64
		_, _ = fmt.Sscanf(strings.TrimSpace(t), "%d", &n)
		return n
	default:
		return 0
	}
}
