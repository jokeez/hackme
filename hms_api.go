package main

import (
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
	if !requireAdminAuth(w, r) {
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
	if !a.allowLoopbackAdminTxSend(r) && !requireAdminAuth(w, r) {
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
	if simpleSign && !a.allowLoopbackAdminTxSend(r) && !a.allowLoopbackDesktopDashboardAuth(r) && !requireAdminAuth(w, r) {
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
	}
	txHash, status, err := a.chain.SubmitHmsTransferTx(r.Context(), tx)
	if err != nil {
		writeJSON(w, map[string]any{"ok": false, "code": status, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "tx_hash": txHash, "status": status})
}
