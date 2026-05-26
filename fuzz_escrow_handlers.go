package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"hackme/internal/chain"
)

func (a *app) handleFuzzPoolSettle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireAdminAuth(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	defer r.Body.Close()
	var req struct {
		Kind         string `json:"kind"`
		CampaignID   string `json:"campaign_id"`
		MinerAddress string `json:"miner_address"`
		Severity     string `json:"severity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid json", nil)
		return
	}
	kind := strings.TrimSpace(strings.ToLower(req.Kind))
	campaignID := strings.TrimSpace(req.CampaignID)
	if campaignID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "campaign_id required", nil)
		return
	}
	ctx := r.Context()
	var row *chain.FuzzEscrowRow
	var err error
	switch kind {
	case "run":
		row, err = a.chain.PayFuzzRun(ctx, campaignID, req.MinerAddress)
	case "finding", "bounty":
		row, err = a.chain.PayFuzzBounty(ctx, campaignID, req.MinerAddress, req.Severity)
	case "finalize", "close":
		row, err = a.chain.FinalizeFuzzEscrow(ctx, campaignID)
	default:
		writeAPIError(w, http.StatusBadRequest, "invalid_kind", "kind must be run|finding|finalize", nil)
		return
	}
	if err != nil {
		status := http.StatusInternalServerError
		code := "settle_failed"
		switch {
		case errors.Is(err, chain.ErrFuzzEscrowNotFound):
			status, code = http.StatusNotFound, "escrow_not_found"
		case errors.Is(err, chain.ErrFuzzEscrowClosed), errors.Is(err, chain.ErrFuzzEscrowDepleted), errors.Is(err, chain.ErrFuzzEscrowAlreadyPaid):
			status, code = http.StatusConflict, "escrow_conflict"
		}
		writeAPIError(w, status, code, err.Error(), nil)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "escrow": row})
}

func (a *app) handleFuzzEscrowGet(w http.ResponseWriter, r *http.Request, campaignID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	row, err := a.chain.GetFuzzEscrow(r.Context(), campaignID)
	if err != nil {
		if errors.Is(err, chain.ErrFuzzEscrowNotFound) {
			writeAPIError(w, http.StatusNotFound, "escrow_not_found", "no escrow for campaign", nil)
			return
		}
		writeAPIError(w, http.StatusInternalServerError, "load_failed", err.Error(), nil)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "escrow": row})
}
