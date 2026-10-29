package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
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
	case "run", "run_floor", "finding", "bounty", "finalize", "close", "crash_bonus", "unique_crash":
	default:
		writeAPIError(w, http.StatusBadRequest, "invalid_kind", "kind must be run|run_floor|finding|crash_bonus|finalize", nil)
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
	// H6: numeric event_id is reserved for coordinator outbox pull (bound miner_address).
	// Manual admin settles must use a non-numeric id (e.g. admin:...).
	if _, perr := strconv.ParseInt(eventID, 10, 64); perr == nil {
		writeAPIError(w, http.StatusBadRequest, "outbox_event_id", "numeric event_id reserved for settle-pull; use admin:<id> for manual settle", nil)
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
	if !requireAdminAuthStrict(w, r) {
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
			st := strings.TrimSpace(strings.ToLower(item.cStatus))
			switch st {
			case "cancelled", "completed":
				a.tryCloseFuzzEscrowForStatus(ctx, item.id, st)
				row, err = a.chain.GetFuzzEscrow(ctx, item.id)
				if err == nil && (row == nil || !strings.EqualFold(strings.TrimSpace(row.Status), "closed")) {
					err = errors.New("escrow still open after drain/close attempt")
				}
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

// handleFuzzEscrowReopenSettleDrain re-locks unpaid run pool after premature
// finalize left coordinator outbox rows unpaid (ops recovery).
func (a *app) handleFuzzEscrowReopenSettleDrain(w http.ResponseWriter, r *http.Request) {
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
		CampaignID string `json:"campaign_id"`
		Pull       *bool  `json:"pull"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid json", nil)
		return
	}
	campaignID := strings.TrimSpace(req.CampaignID)
	if campaignID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "campaign_id required", nil)
		return
	}
	ctx := r.Context()
	row, err := a.chain.ReopenFuzzEscrowForSettleDrain(ctx, campaignID)
	reopened := err == nil
	if err != nil {
		// Idempotent ops: already-open escrow still allows a forced pull.
		cur, gerr := a.chain.GetFuzzEscrow(ctx, campaignID)
		if gerr != nil {
			status := http.StatusInternalServerError
			code := "reopen_failed"
			if errors.Is(err, chain.ErrFuzzEscrowNotFound) || errors.Is(gerr, chain.ErrFuzzEscrowNotFound) {
				status, code = http.StatusNotFound, "escrow_not_found"
			} else if errors.Is(err, chain.ErrFuzzInsufficientBalance) {
				status, code = http.StatusConflict, "insufficient_balance"
			}
			writeAPIError(w, status, code, err.Error(), nil)
			return
		}
		st := strings.ToLower(strings.TrimSpace(cur.Status))
		if st != "open" && st != "bounty_paid" {
			status := http.StatusInternalServerError
			code := "reopen_failed"
			if errors.Is(err, chain.ErrFuzzInsufficientBalance) {
				status, code = http.StatusConflict, "insufficient_balance"
			}
			writeAPIError(w, status, code, err.Error(), nil)
			return
		}
		row = cur
	}
	pull := true
	if req.Pull != nil {
		pull = *req.Pull
	}
	var pullErr string
	if pull {
		if perr := a.pullFuzzSettleOutbox(ctx); perr != nil {
			pullErr = perr.Error()
		}
		if refreshed, gerr := a.chain.GetFuzzEscrow(ctx, campaignID); gerr == nil {
			row = refreshed
		}
	}
	out := map[string]any{"ok": true, "escrow": row, "reopened": reopened}
	if pullErr != "" {
		out["pull_error"] = pullErr
	}
	writeJSON(w, out)
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
	out := map[string]any{"ok": true, "escrow": row}
	if a.db != nil {
		var summaryJSON string
		if err := a.db.QueryRowContext(r.Context(),
			`SELECT summary_json FROM fuzz_campaigns WHERE id=?`, campaignID).Scan(&summaryJSON); err == nil {
			poolRuns := intFromAny(parseMapJSON(summaryJSON)["runs_done"])
			if poolRuns > 0 {
				out["pool_runs_done"] = poolRuns
			}
		}
	}
	writeJSON(w, out)
}
