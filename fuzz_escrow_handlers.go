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
	if !requireAdminAuthStrict(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	defer r.Body.Close()
	var req struct {
		Kind         string `json:"kind"`
		CampaignID   string `json:"campaign_id"`
		MinerAddress string `json:"miner_address"`
		Severity     string `json:"severity"`
		EventID      string `json:"event_id"`
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
	switch kind {
	case "run", "finding", "bounty", "finalize", "close", "crash_bonus", "unique_crash":
	default:
		writeAPIError(w, http.StatusBadRequest, "invalid_kind", "kind must be run|finding|crash_bonus|finalize", nil)
		return
	}
	ctx := r.Context()
	var row *chain.FuzzEscrowRow
	var err error
	eventID := strings.TrimSpace(req.EventID)
	if eventID == "" {
		writeAPIError(w, http.StatusBadRequest, "event_id_required", "event_id required for idempotent settle", nil)
		return
	}
	row, _, err = a.chain.ApplyFuzzSettleOnce(ctx, eventID, kind, campaignID, req.MinerAddress, req.Severity)
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
	writeJSON(w, map[string]any{"ok": true, "escrow": row, "event_id": eventID})
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
	// Same boundary as report/gate/pulse: admin or customer report token (not public).
	if !a.requireFuzzReportAccess(w, r, campaignID, "escrow") {
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
