package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"hackme/internal/chain"
)

func (a *app) supSettlementNoteForAPI(ctx context.Context) string {
	if a == nil {
		return "SUP pool accrual is coordinator-side; enable genesis for on-chain balances."
	}
	ec, err := a.chain.SUPEconomics(ctx)
	if err == nil && ec.OnChainSettleLive {
		return "SUP on-chain ledger is live (mint + transfer_sup_v1). Exchange listing still follows HMC listing policy."
	}
	return "SUP accrual in coordinator; run scripts/ops/sup_genesis_init.sh on chain host for on-chain SUP."
}

func supOnChainSettleEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_SUP_ON_CHAIN_SETTLE")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func (a *app) handleSUPEconomics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ec, err := a.chain.SUPEconomics(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "economics": ec})
}

func (a *app) handleSUPGenesisInit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireAdminAuthStrict(w, r) {
		return
	}
	if err := a.chain.InitSUPGenesis(r.Context()); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	ec, _ := a.chain.SUPEconomics(r.Context())
	writeJSON(w, map[string]any{"ok": true, "economics": ec})
}

func (a *app) handleSUPMint(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !a.allowLoopbackAdminTxSend(r) && !requireAdminAuthStrict(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	var body struct {
		To          string  `json:"to"`
		AmountSUP   float64 `json:"amount_sup"`
		AmountUnits uint64  `json:"amount_units"`
		Memo        string  `json:"memo"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	units := body.AmountUnits
	if units == 0 && body.AmountSUP > 0 {
		units = chain.SUPToUnits(body.AmountSUP)
	}
	code, err := a.chain.MintSUP(r.Context(), strings.TrimSpace(body.To), units, body.Memo)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "code": code, "error": err.Error()})
		return
	}
	st, _ := a.chain.SupAddressState(r.Context(), body.To)
	writeJSON(w, map[string]any{"ok": true, "address_state": st})
}

func (a *app) handleSUPBurn(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !a.allowLoopbackAdminTxSend(r) && !requireAdminAuthStrict(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	var body struct {
		From        string  `json:"from"`
		AmountSUP   float64 `json:"amount_sup"`
		AmountUnits uint64  `json:"amount_units"`
		Memo        string  `json:"memo"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	from := strings.TrimSpace(body.From)
	if from == "" {
		writeJSON(w, map[string]any{"ok": false, "code": "from_required", "error": "from address required"})
		return
	}
	units := body.AmountUnits
	if units == 0 && body.AmountSUP > 0 {
		units = chain.SUPToUnits(body.AmountSUP)
	}
	if units == 0 {
		st, _ := a.chain.SupAddressState(r.Context(), from)
		units = st.BalanceSUPUnits
	}
	if units == 0 {
		writeJSON(w, map[string]any{"ok": false, "code": "zero_balance", "error": "nothing to burn"})
		return
	}
	code, err := a.chain.BurnSUPForService(r.Context(), from, units, body.Memo)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "code": code, "error": err.Error()})
		return
	}
	st, _ := a.chain.SupAddressState(r.Context(), from)
	writeJSON(w, map[string]any{"ok": true, "burned_sup_units": units, "address_state": st})
}

func (a *app) handleSUPTransferSend(w http.ResponseWriter, r *http.Request) {
	if !a.allowRate("sup_tx_send:"+clientIP(r), 20) {
		writeJSON(w, map[string]any{"ok": false, "code": "rate_limited"})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	var tx chain.SupTransferTx
	if err := json.Unmarshal(raw, &tx); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	simpleSign := strings.TrimSpace(tx.PubKeyEd25519) == "" && strings.TrimSpace(tx.SigEd25519) == ""
	if simpleSign && !a.allowLoopbackAdminTxSend(r) && !a.allowLoopbackDesktopDashboardAuth(r) && !requireAdminAuthStrict(w, r) {
		return
	}
	if simpleSign {
		if strings.TrimSpace(tx.From) == "" && a.signer != nil {
			tx.From = strings.TrimSpace(a.signer.Address())
		}
		if tx.TimestampUnix <= 0 {
			tx.TimestampUnix = time.Now().Unix()
		}
		if code, msg := chain.ValidateSupTransferShape(tx); code != "" {
			writeJSON(w, map[string]any{"ok": false, "code": code, "error": msg})
			return
		}
	}
	txHash, status, err := a.chain.SubmitSupTransferTx(r.Context(), tx)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "code": status, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "tx_hash": txHash, "status": status})
}
