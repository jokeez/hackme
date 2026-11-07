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

func (a *app) handleHMSEconomics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ec, err := a.chain.HMSEconomics(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "economics": ec})
}

func (a *app) handleHMSGenesisInit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireAdminAuthStrict(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	raw, _ := io.ReadAll(r.Body)
	var body struct {
		TreasuryAddress string `json:"treasury_address"`
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &body)
	}
	treasury := strings.TrimSpace(body.TreasuryAddress)
	if treasury == "" {
		treasury = strings.TrimSpace(os.Getenv("HMS_TREASURY_ADDRESS"))
	}
	if err := a.chain.InitHMSGenesis(r.Context(), treasury); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	ec, _ := a.chain.HMSEconomics(r.Context())
	writeJSON(w, map[string]any{"ok": true, "economics": ec})
}

func (a *app) handleHMSMint(w http.ResponseWriter, r *http.Request) {
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
		AmountHMS   float64 `json:"amount_hms"`
		AmountUnits uint64  `json:"amount_units"`
		Memo        string  `json:"memo"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	units := body.AmountUnits
	if units == 0 && body.AmountHMS > 0 {
		units = chain.HMSToUnits(body.AmountHMS)
	}
	code, err := a.chain.MintHMS(r.Context(), strings.TrimSpace(body.To), units, body.Memo)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "code": code, "error": err.Error()})
		return
	}
	st, _ := a.chain.HmsAddressState(r.Context(), body.To)
	writeJSON(w, map[string]any{"ok": true, "address_state": st})
}

func (a *app) handleHMSTransferSend(w http.ResponseWriter, r *http.Request) {
	if !a.allowRate("hms_tx_send:"+clientIP(r), 20) {
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
	var tx chain.HmsTransferTx
	if err := json.Unmarshal(raw, &tx); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	simpleSign := strings.TrimSpace(tx.PubKeyEd25519) == "" && strings.TrimSpace(tx.SigEd25519) == ""
	if simpleSign && !a.allowLoopbackAdminTxSend(r) && !a.allowLoopbackDesktopDashboardAuth(r) && !requireAdminAuthStrict(w, r) {
		return
	}
	// Followers must not write HMS to a local fork (no canonical HMS relay yet).
	if a.shouldUseCanonicalChainAPI() {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":    false,
			"code":  "hms_canonical_required",
			"error": "HMS transfers must run on the chain host (canonical HMS relay not enabled on followers)",
		})
		return
	}
	if simpleSign {
		if strings.TrimSpace(tx.From) == "" && a.signer != nil {
			tx.From = strings.TrimSpace(a.signer.Address())
		}
		if tx.TimestampUnix <= 0 {
			tx.TimestampUnix = time.Now().Unix()
		}
		if code, msg := chain.ValidateHmsTransferShape(tx); code != "" {
			writeJSON(w, map[string]any{"ok": false, "code": code, "error": msg})
			return
		}
		if a.signer == nil {
			writeJSON(w, map[string]any{"ok": false, "code": "signer_unavailable", "error": "node signer unavailable"})
			return
		}
		from := strings.TrimSpace(tx.From)
		signerAddr := strings.TrimSpace(a.signer.Address())
		if from == "" {
			tx.From = signerAddr
		} else if !strings.EqualFold(from, signerAddr) {
			writeJSON(w, map[string]any{"ok": false, "code": "address_pubkey_mismatch", "error": "simple signing allowed only for node wallet address"})
			return
		}
		if tx.Nonce == 0 {
			if st, err := a.chain.HmsAddressState(r.Context(), tx.From); err == nil {
				tx.Nonce = st.HMSNextNonce
			}
		}
		tx.PubKeyEd25519 = strings.TrimSpace(a.signer.PublicKeyHex())
		tx.ChainID = a.txChainIDForSigning(r.Context()) // hub's network id ("" before Economics v2)
		canon, err := tx.CanonicalBytes()
		if err != nil {
			writeJSON(w, map[string]any{"ok": false, "code": "invalid_tx_encoding", "error": "invalid tx canonical payload"})
			return
		}
		tx.SigEd25519 = strings.TrimSpace(a.signer.SignHex(canon))
	}
	txHash, status, err := a.chain.SubmitHmsTransferTx(r.Context(), tx)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "code": status, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "tx_hash": txHash, "status": status})
}

// hmsStatusPayload is the honest HMS lane snapshot for /api/status (prelaunch by default).
func (a *app) hmsStatusPayload(ctx context.Context) map[string]any {
	out := map[string]any{
		"prelaunch":           true,
		"lane_enabled":        chain.HMSLaneEnabled(),
		"coordinator_url_set": strings.TrimSpace(os.Getenv("HACKME_HMS_COORDINATOR_URL")) != "",
	}
	if chain.HMSLaneEnabled() {
		out["prelaunch"] = false
	}
	if a == nil || a.chain == nil {
		return out
	}
	econCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	if ec, err := a.chain.HMSEconomics(econCtx); err == nil {
		out["mint_enabled"] = ec.MintEnabled
		out["on_chain_settle_live"] = ec.OnChainSettleLive
		out["prelaunch"] = ec.Prelaunch
		out["lane_enabled"] = ec.LaneEnabled
		out["circulating_hms"] = ec.CirculatingHMS
		out["max_supply_hms"] = ec.MaxSupplyHMS
		if ec.TreasuryAddress != "" {
			out["treasury_configured"] = true
		}
	}
	return out
}
