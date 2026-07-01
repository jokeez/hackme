package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

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

func (a *app) handleFuzzEscrowCleanupStale(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireAdminAuth(w, r) {
		return
	}
	ctx := r.Context()
	rows, err := a.db.QueryContext(ctx,
		`SELECT c.id, c.status, e.status
		 FROM fuzz_campaigns c
		 JOIN fuzz_campaign_escrow e ON e.campaign_id = c.id
		 WHERE e.status IN ('open', 'bounty_paid')
		   AND c.status IN ('cancelled', 'completed')`)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "query_failed", err.Error(), nil)
		return
	}
	defer rows.Close()
	type staleRow struct {
		id, cStatus string
	}
	var pending []staleRow
	for rows.Next() {
		var cid, cStatus, eStatus string
		if err := rows.Scan(&cid, &cStatus, &eStatus); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "scan_failed", err.Error(), nil)
			return
		}
		pending = append(pending, staleRow{id: cid, cStatus: cStatus})
	}
	var cancelled, completed, failed int
	var refundedHMC float64
	var errs []string
	for _, item := range pending {
		var row *chain.FuzzEscrowRow
		var err error
		for attempt := 0; attempt < 5; attempt++ {
			switch strings.TrimSpace(strings.ToLower(item.cStatus)) {
			case "cancelled":
				row, err = a.chain.CancelFuzzEscrow(ctx, item.id)
			case "completed":
				row, err = a.chain.FinalizeFuzzEscrow(ctx, item.id)
			default:
				err = nil
			}
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), "database is locked") {
				break
			}
			time.Sleep(time.Duration(attempt+1) * 50 * time.Millisecond)
		}
		if err != nil {
			failed++
			errs = append(errs, item.id+": "+err.Error())
			continue
		}
		switch strings.TrimSpace(strings.ToLower(item.cStatus)) {
		case "cancelled":
			cancelled++
		case "completed":
			completed++
		}
		if row != nil {
			refundedHMC += row.RefundedBountyHMC
		}
	}
	writeJSON(w, map[string]any{
		"ok":                  true,
		"cancelled_closed":    cancelled,
		"completed_closed":    completed,
		"failed":              failed,
		"refunded_bounty_hmc": refundedHMC,
		"errors":              errs,
	})
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
