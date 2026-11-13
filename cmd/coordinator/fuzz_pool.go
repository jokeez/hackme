package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"hackme/internal/chain"
	"hackme/internal/fuzzengine"
	"hackme/internal/fuzzingcli"
	"hackme/internal/hunt"
	"hackme/internal/pathsafe"
	"hackme/internal/poolfuzz"
	"hackme/internal/poolsync"
)

// verifySettleAckReceipts ensures each ACK id has a MAC'd Apply receipt bound to the
// outbox row (event_id + campaign + kind + hub-expected units_hash). Never trust bare bools
// or tenant-invented escrow unit vectors (PUB-07).
func verifySettleAckReceipts(ctx context.Context, pf *poolfuzz.Service, publishToken, ownerRef, ownerSecret, campaignID string, ids []int64, receipts []poolsync.SettleApplyReceipt) error {
	if pf == nil {
		return fmt.Errorf("no pool service")
	}
	if strings.TrimSpace(ownerSecret) == "" {
		return fmt.Errorf("owner secret required to verify settle receipts")
	}
	byID := make(map[int64]poolsync.SettleApplyReceipt, len(receipts))
	for _, rc := range receipts {
		if rc.OutboxID <= 0 {
			return fmt.Errorf("receipt missing outbox_id")
		}
		byID[rc.OutboxID] = rc
	}
	if len(ids) == 0 {
		return fmt.Errorf("ids required")
	}
	campaignID = strings.TrimSpace(campaignID)
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		rc, ok := byID[id]
		if !ok {
			return fmt.Errorf("missing settle receipt for outbox id %d", id)
		}
		it, err := pf.GetSettleOutboxItem(ctx, id)
		if err != nil {
			return fmt.Errorf("outbox id %d: %w", id, err)
		}
		if campaignID != "" && strings.TrimSpace(it.CampaignID) != campaignID {
			return fmt.Errorf("outbox id %d campaign mismatch", id)
		}
		wantEvent := chain.FuzzSettleEventID(it.CampaignID, it.ID)
		if strings.TrimSpace(rc.EventID) != wantEvent {
			return fmt.Errorf("outbox id %d event_id mismatch", id)
		}
		if strings.TrimSpace(rc.CampaignID) != strings.TrimSpace(it.CampaignID) {
			return fmt.Errorf("outbox id %d campaign_id mismatch", id)
		}
		if strings.ToLower(strings.TrimSpace(rc.Kind)) != strings.ToLower(strings.TrimSpace(it.Kind)) {
			return fmt.Errorf("outbox id %d kind mismatch", id)
		}
		outcome := strings.ToLower(strings.TrimSpace(rc.Outcome))
		switch outcome {
		case poolsync.SettleOutcomeApplied, poolsync.SettleOutcomeDepleted,
			poolsync.SettleOutcomeAlreadyPaid, poolsync.SettleOutcomeDrainClose:
		default:
			return fmt.Errorf("outbox id %d invalid outcome", id)
		}
		wantUnits := poolsync.UnitsHashFromOutbox(wantEvent, it.CampaignID, it.Kind, it.MinerAddress, it.Severity, it.WorkItemID, outcome)
		if !strings.EqualFold(strings.TrimSpace(rc.UnitsHash), wantUnits) {
			return fmt.Errorf("outbox id %d units_hash mismatch (hub outbox binding)", id)
		}
		if !poolsync.VerifySettleReceiptMAC(publishToken, ownerRef, ownerSecret, rc) {
			return fmt.Errorf("outbox id %d receipt mac invalid", id)
		}
	}
	return nil
}

// writeHarnessUploadAPIError returns structured JSON for Orders harness publish
// (harness_too_large / publish_rate_limited) instead of opaque HTML/plain 413/429.
func writeHarnessUploadAPIError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok": false, "code": code, "error": msg,
	})
}

// coordPoolReadOK allows the pool admin token or the worker token.
// Empty tokens are accepted only in explicit insecure mode (report #12).
func coordPoolReadOK(r *http.Request, adminToken, workerToken string, allowInsecure bool) bool {
	if strings.TrimSpace(adminToken) == "" && strings.TrimSpace(workerToken) == "" && allowInsecure {
		return true
	}
	// AUTH-02: admin secret only via Admin-Token / Bearer (not Worker-Token carrier).
	if adminToken != "" && coordPublishRouteOK(r, adminToken) {
		return true
	}
	if workerToken != "" && coordAdminOK(r, workerToken) {
		return true
	}
	return false
}

func addFuzzPoolRoutes(mux *http.ServeMux, adminToken, workerToken, publishToken string, allowInsecure bool, wm *workManager, pf *poolfuzz.Service) {
	if pf == nil {
		return
	}
	var (
		listMu    sync.Mutex
		listCache []map[string]any
		listAt    time.Time
	)
	const listCacheTTL = 12 * time.Second
	const listPublicLimitMax = 100
	var (
		listRefreshMu sync.Mutex
		listRefreshAt = map[string]time.Time{}
	)
	mux.HandleFunc("/api/fuzz/pool/campaigns/list", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		limit := 50
		if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}
		if limit > listPublicLimitMax {
			limit = listPublicLimitMax
		}
		force := strings.TrimSpace(r.URL.Query().Get("refresh")) == "1"
		if force {
			ip := clientIP(r)
			listRefreshMu.Lock()
			last := listRefreshAt[ip]
			if !last.IsZero() && time.Since(last) < 30*time.Second {
				listRefreshMu.Unlock()
				force = false // ignore bust; fall through to cache
			} else {
				listRefreshAt[ip] = time.Now()
				listRefreshMu.Unlock()
			}
		}
		listMu.Lock()
		if !force && len(listCache) > 0 && time.Since(listAt) < listCacheTTL {
			cached := cloneCampaignMaps(listCache)
			listMu.Unlock()
			cap := wm.fuzzFleetCapacity(time.Now().Unix())
			poolfuzz.AnnotateCampaignFleetETA(cached, cap.EstShardsPerHour)
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Cache-Control", "public, max-age=15")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true, "campaigns": cached, "cached": true, "fleet_capacity": cap,
			})
			return
		}
		listMu.Unlock()
		items, err := pf.ListPublicCampaigns(r.Context(), limit)
		if err != nil {
			listMu.Lock()
			stale := cloneCampaignMaps(listCache)
			listMu.Unlock()
			if len(stale) > 0 {
				cap := wm.fuzzFleetCapacity(time.Now().Unix())
				poolfuzz.AnnotateCampaignFleetETA(stale, cap.EstShardsPerHour)
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.Header().Set("Cache-Control", "public, max-age=5")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"ok": true, "campaigns": stale, "cached": true, "stale": true, "fleet_capacity": cap,
				})
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if len(items) > 0 {
			listMu.Lock()
			listCache = items
			listAt = time.Now()
			listMu.Unlock()
		}
		cap := wm.fuzzFleetCapacity(time.Now().Unix())
		out := cloneCampaignMaps(items)
		poolfuzz.AnnotateCampaignFleetETA(out, cap.EstShardsPerHour)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=15")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "campaigns": out, "fleet_capacity": cap,
		})
	})

	mux.HandleFunc("/api/fuzz/pool/campaigns/progress", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		isAdmin, isPublish, pubOK := campaignRegisterAuthed(r, adminToken, publishToken, allowInsecure)
		if !coordPoolReadOK(r, adminToken, workerToken, allowInsecure) && !pubOK {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ownerAccess := false
		if isPublish && !isAdmin && !coordPoolReadOK(r, adminToken, workerToken, allowInsecure) {
			exists, err := pf.CampaignExists(r.Context(), id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if !exists {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			ok, err := pf.CampaignIsOrdersPublish(r.Context(), id)
			if err != nil || !ok {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			okOwner, err := loadPublishOwnerGate(r, pf, id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if !okOwner {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			ownerAccess = true
		}
		var prog map[string]any
		var err error
		if ownerAccess || isAdmin {
			prog, err = pf.CampaignProgressOwner(r.Context(), id)
		} else {
			prog, err = pf.CampaignProgress(r.Context(), id)
		}
		if err == sql.ErrNoRows {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		cap := wm.fuzzFleetCapacity(time.Now().Unix())
		prog["fleet_capacity"] = cap
		budget := intFromProgress(prog["budget_runs"])
		done := intFromProgress(prog["runs_done"])
		remaining := budget - done
		if remaining < 0 {
			remaining = 0
		}
		eta := poolfuzz.EstimateFleetETASeconds(remaining, cap.EstShardsPerHour)
		prog["remaining_runs"] = remaining
		prog["eta_sec_fleet"] = eta
		if eta < 0 {
			prog["eta_note"] = "fleet capacity warming — not enough online dig/hybrid workers"
		} else if eta == 0 && remaining == 0 {
			prog["eta_note"] = "complete"
		} else {
			prog["eta_note"] = "ETA from live hybrid GHS + dig workers (heuristic)"
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(prog)
	})

	// Admin or scoped Orders publish (orders_publish campaigns only) for findings mirror.
	mux.HandleFunc("/api/fuzz/pool/campaigns/findings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		isAdmin, isPublish, authed := campaignRegisterAuthed(r, adminToken, publishToken, allowInsecure)
		if !authed {
			writePublishAuthError(w)
			return
		}
		if isPublish && !isAdmin {
			ok, err := pf.CampaignIsOrdersPublish(r.Context(), id)
			if err != nil || !ok {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			okOwner, err := loadPublishOwnerGate(r, pf, id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if !okOwner {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}
		limit := 100
		if s := strings.TrimSpace(r.URL.Query().Get("limit")); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 200 {
				limit = n
			}
		}
		items, err := pf.ListCampaignFindingsMirror(r.Context(), id, limit)
		if err == sql.ErrNoRows {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "campaign_id": id, "findings": items, "count": len(items),
		})
	})

	mux.HandleFunc("/api/fuzz/pool/settle/outbox", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Admin or scoped Orders publish (desktop settle-pull). Worker token denied.
		isAdmin, isPublish, ok := campaignRegisterAuthed(r, adminToken, publishToken, allowInsecure)
		if !ok {
			writePublishAuthError(w)
			return
		}
		limit := 64
		if s := strings.TrimSpace(r.URL.Query().Get("limit")); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 500 {
				limit = n
			}
		}
		campaignID := strings.TrimSpace(r.URL.Query().Get("campaign_id"))
		if isPublish && !isAdmin {
			if campaignID == "" {
				http.Error(w, "campaign_id required for publish token", http.StatusBadRequest)
				return
			}
			pub, err := pf.CampaignIsOrdersPublish(r.Context(), campaignID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if !pub {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			okOwner, err := loadPublishOwnerGate(r, pf, campaignID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if !okOwner {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}
		countOnly := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("count")), "1") ||
			strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("count")), "true")
		if campaignID != "" && countOnly {
			n, err := pf.CountPendingSettleOutbox(r.Context(), campaignID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "campaign_id": campaignID, "pending": n})
			return
		}
		var (
			items []poolfuzz.SettleOutboxItem
			err   error
		)
		if campaignID != "" {
			items, err = pf.ListPendingSettleOutboxForCampaign(r.Context(), campaignID, limit)
		} else {
			items, err = pf.ListPendingSettleOutbox(r.Context(), limit)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "items": items})
	})

	mux.HandleFunc("/api/fuzz/pool/settle/outbox/ack", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		isAdmin, isPublish, ok := campaignRegisterAuthed(r, adminToken, publishToken, allowInsecure)
		if !ok {
			writePublishAuthError(w)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCoordinatorJSONBodyBytes)
		var req struct {
			IDs            []int64                       `json:"ids"`
			CampaignID     string                        `json:"campaign_id"`
			ConfirmApplied bool                          `json:"confirm_applied"` // ignored alone
			OriginApplied  bool                          `json:"origin_applied"`  // ignored alone
			HubApplied     bool                          `json:"hub_applied"`     // admin/hub-side only
			Receipts       []poolsync.SettleApplyReceipt `json:"receipts"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		const maxSettleAckIDs = 256
		if len(req.IDs) > maxSettleAckIDs || len(req.Receipts) > maxSettleAckIDs {
			http.Error(w, "too many settle ack ids (max 256)", http.StatusBadRequest)
			return
		}
		// Never trust bare confirm_applied/origin_applied.
		hubSide := isAdmin && req.HubApplied && len(req.Receipts) == 0
		if !hubSide {
			if len(req.Receipts) == 0 {
				http.Error(w, "settle receipts required (never trust confirm_applied alone)", http.StatusBadRequest)
				return
			}
			if len(req.IDs) == 0 {
				for _, rc := range req.Receipts {
					if rc.OutboxID > 0 {
						req.IDs = append(req.IDs, rc.OutboxID)
					}
				}
			}
		}
		var (
			n   int64
			err error
		)
		if isPublish && !isAdmin {
			if strings.TrimSpace(req.CampaignID) == "" {
				http.Error(w, "campaign_id required for publish token", http.StatusBadRequest)
				return
			}
			okOwner, errOwn := loadPublishOwnerGate(r, pf, req.CampaignID)
			if errOwn != nil {
				http.Error(w, errOwn.Error(), http.StatusInternalServerError)
				return
			}
			if !okOwner {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			// Separate from publish/harness hourly caps — settle must drain after CLEAN.
			if !settleRateAllow(r) {
				http.Error(w, "settle rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			storedOwner, _ := pf.CampaignOwnerRef(r.Context(), req.CampaignID)
			ownerSecret := publishOwnerSecretFromRequest(r)
			if err := verifySettleAckReceipts(r.Context(), pf, publishToken, storedOwner, ownerSecret, req.CampaignID, req.IDs, req.Receipts); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			n, err = pf.AckSettleOutboxForOrdersPublish(r.Context(), req.CampaignID, req.IDs)
		} else if hubSide {
			// SETTLE-01: bare hub_applied must not clear orders_publish outbox rows —
			// those require owner-signed receipts (false ACK without Apply).
			for _, id := range req.IDs {
				if id <= 0 {
					continue
				}
				it, gerr := pf.GetSettleOutboxItem(r.Context(), id)
				if gerr != nil {
					http.Error(w, gerr.Error(), http.StatusBadRequest)
					return
				}
				pub, perr := pf.CampaignIsOrdersPublish(r.Context(), it.CampaignID)
				if perr != nil {
					http.Error(w, perr.Error(), http.StatusInternalServerError)
					return
				}
				if pub {
					http.Error(w, "hub_applied refused for orders_publish outbox; owner-signed settle receipts required", http.StatusBadRequest)
					return
				}
			}
			n, err = pf.AckSettleOutbox(r.Context(), req.IDs)
		} else {
			camp := strings.TrimSpace(req.CampaignID)
			if camp == "" && len(req.Receipts) > 0 {
				camp = strings.TrimSpace(req.Receipts[0].CampaignID)
			}
			// SETTLE-01 admin fallback: still require the campaign owner secret
			// (hash gate). Caller-chosen MAC key must not bypass owner binding.
			if camp != "" {
				okOwner, errOwn := loadPublishOwnerGate(r, pf, camp)
				if errOwn != nil {
					http.Error(w, errOwn.Error(), http.StatusInternalServerError)
					return
				}
				if !okOwner {
					http.Error(w, "forbidden: owner-signed settle receipts required", http.StatusForbidden)
					return
				}
			}
			storedOwner, _ := pf.CampaignOwnerRef(r.Context(), camp)
			ownerSecret := publishOwnerSecretFromRequest(r)
			if ownerSecret == "" || storedOwner == "" {
				http.Error(w, "hub_applied=true or owner-signed settle receipts required", http.StatusBadRequest)
				return
			}
			if err := verifySettleAckReceipts(r.Context(), pf, publishToken, storedOwner, ownerSecret, camp, req.IDs, req.Receipts); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if camp != "" {
				n, err = pf.AckSettleOutboxForOrdersPublish(r.Context(), camp, req.IDs)
				if err != nil && strings.Contains(err.Error(), "not orders_publish") {
					n, err = pf.AckSettleOutbox(r.Context(), req.IDs)
				}
			} else {
				n, err = pf.AckSettleOutbox(r.Context(), req.IDs)
			}
		}
		if err != nil {
			msg := err.Error()
			if strings.Contains(msg, "not orders_publish") || strings.Contains(msg, "campaign_id required") {
				http.Error(w, msg, http.StatusForbidden)
				return
			}
			http.Error(w, msg, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "acked": n})
	})

	mux.HandleFunc("/api/fuzz/pool/settle/replay", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if adminToken == "" && allowInsecure {
			// loopback dev
		} else if adminToken == "" || !coordPublishRouteOK(r, adminToken) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
			http.Error(w, "admin authentication required", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCoordinatorJSONBodyBytes)
		var req struct {
			CampaignID string `json:"campaign_id"`
			ID         string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		cid := strings.TrimSpace(req.CampaignID)
		if cid == "" {
			cid = strings.TrimSpace(req.ID)
		}
		if cid == "" {
			http.Error(w, "campaign_id required", http.StatusBadRequest)
			return
		}
		runs, findings, fin, err := pf.ReplayCampaignSettles(r.Context(), cid)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "campaign_id": cid,
			"runs_enqueued": runs, "findings_enqueued": findings, "finalize_enqueued": fin,
		})
	})

	mux.HandleFunc("/api/fuzz/pool/stats", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		st, err := pf.PoolStats(r.Context())
		if err != nil {
			http.Error(w, "stats failed", http.StatusInternalServerError)
			return
		}
		// POOL-01: keep queue depth public for marketplace UX; fleet capacity /
		// hashrate / shard ETA only for admin|worker (not anonymous scrape intel).
		if coordPoolReadOK(r, adminToken, workerToken, allowInsecure) {
			cap := wm.fuzzFleetCapacity(time.Now().Unix())
			st["fleet_capacity"] = cap
			st["fleet_hashrate_gh_s"] = cap.FleetHashrateGHS
			st["est_shards_per_hour"] = cap.EstShardsPerHour
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(st)
	})

	mux.HandleFunc("/api/fuzz/pool/campaigns/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if adminToken == "" && allowInsecure {
			// loopback dev
		} else if adminToken == "" || !coordPublishRouteOK(r, adminToken) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
			http.Error(w, "admin authentication required", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCoordinatorJSONBodyBytes)
		var req struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.ID) == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		status := strings.TrimSpace(strings.ToLower(req.Status))
		if status == "" {
			status = "cancelled"
		}
		if err := pf.SetCampaignStatus(r.Context(), req.ID, status); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": req.ID, "status": status})
	})

	mux.HandleFunc("/api/fuzz/pool/campaigns/cleanup-gates", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if adminToken == "" && allowInsecure {
			// loopback dev
		} else if adminToken == "" || !coordPublishRouteOK(r, adminToken) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
			http.Error(w, "admin authentication required", http.StatusUnauthorized)
			return
		}
		n, err := pf.CancelInternalGateCampaigns(r.Context(), 200)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "cancelled": n})
	})

	mux.HandleFunc("/api/fuzz/pool/campaigns/cleanup-stale", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if adminToken == "" && allowInsecure {
			// loopback dev
		} else if adminToken == "" || !coordPublishRouteOK(r, adminToken) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
			http.Error(w, "admin authentication required", http.StatusUnauthorized)
			return
		}
		minAge := int64(3600)
		if s := strings.TrimSpace(r.URL.Query().Get("min_age_sec")); s != "" {
			if n, err := strconv.ParseInt(s, 10, 64); err == nil && n >= 0 {
				minAge = n
			}
		}
		n, err := pf.CancelZeroProgressPoolCampaigns(r.Context(), minAge, 300)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		stuck, err := pf.CancelStuckExpiredLeaseCampaigns(r.Context(), minAge, 100)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		missingHarness, err := pf.CancelHuntCampaignsMissingHarness(r.Context(), minAge, 100)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		repaired, err := pf.RepairZombiePoolCampaigns(r.Context(), 50)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		now := time.Now().Unix()
		if _, err := pf.ReclaimExpiredLeases(r.Context(), now); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		churn, err := pf.ForceCompleteLeaseChurnItems(r.Context(), now, 100)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		stalled, err := pf.ForceCompleteStalledNearDoneCampaigns(r.Context(), now, 50)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "cancelled": n, "stuck_expired_leases": stuck,
			"missing_harness": missingHarness, "repaired": repaired,
			"lease_churn_force_done": churn, "stalled_near_done_force_done": stalled,
		})
	})

	mux.HandleFunc("/api/fuzz/pool/campaigns/repair-zombies", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if adminToken == "" && allowInsecure {
			// loopback dev
		} else if adminToken == "" || !coordPublishRouteOK(r, adminToken) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
			http.Error(w, "admin authentication required", http.StatusUnauthorized)
			return
		}
		limit := 20
		if s := strings.TrimSpace(r.URL.Query().Get("limit")); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 {
				limit = n
			}
		}
		n, err := pf.RepairZombiePoolCampaigns(r.Context(), limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "repaired": n})
	})

	// Admin-minted hub prepaid tickets (ops / after confirmed funds). Publish token alone cannot mint.
	mux.HandleFunc("/api/fuzz/pool/prepaid-ticket", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// AUTH-01: admin mint accepts Admin-Token / Bearer only (not Worker-Token carrier).
		if adminToken == "" || !coordPublishRouteOK(r, adminToken) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
			http.Error(w, "admin authentication required", http.StatusUnauthorized)
			return
		}
		hubSec := poolsync.OrdersPrepaidHubSecret()
		if hubSec == "" {
			http.Error(w, "HACKME_ORDERS_PREPAID_HUB_SECRET not configured", http.StatusServiceUnavailable)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCoordinatorJSONBodyBytes)
		var req struct {
			CampaignID        string  `json:"campaign_id"`
			OwnerRef          string  `json:"owner_ref"`
			BudgetHMC         float64 `json:"budget_hmc"`
			BudgetRuns        int     `json:"budget_runs"`
			EscrowFingerprint string  `json:"escrow_fingerprint"`
			TTLSec            int64   `json:"ttl_sec"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		owner := strings.TrimSpace(req.OwnerRef)
		camp := strings.TrimSpace(req.CampaignID)
		if owner == "" || camp == "" || req.BudgetHMC <= 0 || req.BudgetRuns <= 0 {
			http.Error(w, "campaign_id, owner_ref, budget_hmc, budget_runs required", http.StatusBadRequest)
			return
		}
		// Always bind ticket FP to the claimed open escrow (ignore client-supplied opaque FP).
		fp := poolsync.EscrowPrepaidFingerprintParts(camp, req.BudgetHMC, req.BudgetRuns, "open")
		algebraic := false
		if poolsync.RequireHubPrepaidTicket() {
			if poolsync.PrepaidAlgebraicMintAllowed() {
				algebraic = true
			} else {
				hubBase := poolsync.HubEscrowProbeURL("")
				if wm != nil {
					hubBase = poolsync.HubEscrowProbeURL(wm.ordersProbeURL)
				}
				hubTok := adminToken
				if wm != nil {
					if t := wm.ordersAdminToken(); t != "" {
						hubTok = t
					}
				}
				if err := poolsync.VerifyHubPrepaidEscrowLive(r.Context(), hubBase, hubTok, camp, req.BudgetHMC, req.BudgetRuns, fp); err != nil {
					http.Error(w, "hub escrow required before prepaid mint: "+err.Error()+
						" (open escrow on hub ledger, or set HACKME_ORDERS_PREPAID_ALLOW_ALGEBRAIC_MINT=1 break-glass)", http.StatusConflict)
					return
				}
			}
		}
		ttl := req.TTLSec
		if ttl <= 0 {
			ttl = poolsync.DefaultHubPrepaidTicketTTL
		}
		if ttl > poolsync.DefaultHubPrepaidTicketTTL {
			ttl = poolsync.DefaultHubPrepaidTicketTTL
		}
		exp := time.Now().Unix() + ttl
		ticket := poolsync.SignHubPrepaidTicket(hubSec, owner, camp, req.BudgetHMC, req.BudgetRuns, fp, exp)
		if ticket == "" {
			http.Error(w, "ticket sign failed", http.StatusInternalServerError)
			return
		}
		note := "hub-signed; bind on POST /api/fuzz/pool/campaigns with matching budget/owner/escrow_fingerprint"
		if algebraic {
			note = "hub-signed (algebraic break-glass — no live GetFuzzEscrow); " + note
		} else if poolsync.RequireHubPrepaidTicket() {
			note = "hub-signed after live hub escrow verify; " + note
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":                  true,
			"prepaid_attestation": ticket,
			"escrow_fingerprint":  fp,
			"exp_unix":            exp,
			"algebraic_mint":      algebraic,
			"note":                note,
		})
	})

	mux.HandleFunc("/api/fuzz/pool/campaigns", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		isAdmin, isPublish, authed := campaignRegisterAuthed(r, adminToken, publishToken, allowInsecure)
		if !authed {
			writePublishAuthError(w)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCoordinatorJSONBodyBytes)
		var req struct {
			ID                 string         `json:"id"`
			CampaignType       string         `json:"campaign_type"`
			Title              string         `json:"title"`
			Description        string         `json:"description"`
			OwnerRef           string         `json:"owner_ref"`
			Status             string         `json:"status"`
			BudgetRuns         int            `json:"budget_runs"`
			BudgetSeconds      int            `json:"budget_seconds"`
			BudgetHMC          float64        `json:"budget_hmc"`
			PrepaidAttestation string         `json:"prepaid_attestation"`
			EscrowFingerprint  string         `json:"escrow_fingerprint"`
			Config             map[string]any `json:"config"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.ID) == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		if req.Config == nil {
			req.Config = map[string]any{}
		}
		req.Config["pool_distributed"] = true
		issuedOwnerSecret := ""
		prepaidOK := false
		verifiedBudgetHMC := 0.0
		if isPublish && !isAdmin {
			// Capture client budget / attest / escrow FP before strip (strip drops spoofable markers).
			verifiedBudgetHMC = publishBudgetHMCFromReq(req.BudgetHMC, req.Config)
			attest := strings.TrimSpace(req.PrepaidAttestation)
			if attest == "" {
				if v, ok := req.Config["prepaid_attestation"].(string); ok {
					attest = strings.TrimSpace(v)
				}
			}
			escrowFP := strings.TrimSpace(req.EscrowFingerprint)
			if escrowFP == "" {
				if v, ok := req.Config["escrow_fingerprint"].(string); ok {
					escrowFP = strings.TrimSpace(v)
				}
			}
			stripPublishEscalationConfig(req.Config)
			req.OwnerRef = strings.TrimSpace(req.OwnerRef)
			headerOwner := publishOwnerRefFromRequest(r)
			if headerOwner == "" {
				http.Error(w, "X-Hackme-Orders-Owner-Ref required for publish token", http.StatusBadRequest)
				return
			}
			if req.OwnerRef == "" {
				req.OwnerRef = headerOwner
			} else if req.OwnerRef != headerOwner {
				http.Error(w, "owner_ref must match X-Hackme-Orders-Owner-Ref", http.StatusBadRequest)
				return
			}
			if err := validatePublishCampaignCaps(publishCampaignReq{
				ID: req.ID, Status: req.Status, Title: req.Title, Description: req.Description,
				OwnerRef: req.OwnerRef, BudgetRuns: req.BudgetRuns, BudgetSeconds: req.BudgetSeconds, Config: req.Config,
			}); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			minHMC := publishMinBudgetHMC(req.Config)
			maxHMC := publishMaxBudgetHMC()
			if verifiedBudgetHMC+1e-12 < minHMC {
				http.Error(w, fmt.Sprintf("budget_hmc_required: publish requires budget_hmc >= %.2f (prepaid)", minHMC), http.StatusBadRequest)
				return
			}
			if verifiedBudgetHMC > maxHMC {
				http.Error(w, fmt.Sprintf("budget_hmc exceeds publish cap (%.2f)", maxHMC), http.StatusBadRequest)
				return
			}
			requireHub := poolsync.RequireHubPrepaidTicket()
			hubSec := poolsync.OrdersPrepaidHubSecret()
			if requireHub && hubSec == "" {
				http.Error(w, "prepaid_hub_secret_required: set HACKME_ORDERS_PREPAID_HUB_SECRET on minting hub", http.StatusServiceUnavailable)
				return
			}
			if requireHub || poolsync.IsHubPrepaidTicket(attest) {
				wantFP := poolsync.EscrowPrepaidFingerprintParts(req.ID, verifiedBudgetHMC, req.BudgetRuns, "open")
				if escrowFP == "" || !strings.EqualFold(escrowFP, wantFP) {
					http.Error(w, "escrow_fingerprint must match open escrow for claimed budget_hmc/budget_runs", http.StatusBadRequest)
					return
				}
				escrowFP = wantFP
				// Crit-grade on-hub PoF: MAC alone is not enough — require live hub escrow
				// (or documented algebraic break-glass) before accepting hubv1 tickets.
				hubBase := poolsync.HubEscrowProbeURL("")
				if wm != nil {
					hubBase = poolsync.HubEscrowProbeURL(wm.ordersProbeURL)
				}
				hubTok := adminToken
				if wm != nil {
					if t := wm.ordersAdminToken(); t != "" {
						hubTok = t
					}
				}
				if err := poolsync.VerifyHubPrepaidEscrowLive(r.Context(), hubBase, hubTok, req.ID, verifiedBudgetHMC, req.BudgetRuns, escrowFP); err != nil {
					http.Error(w, "hub escrow required for hub prepaid ticket: "+err.Error()+
						" (open escrow on hub ledger, or set HACKME_ORDERS_PREPAID_ALLOW_ALGEBRAIC_MINT=1 break-glass)", http.StatusConflict)
					return
				}
			}
			if !poolsync.VerifyPublishPrepaidAttestation(publishToken, hubSec, req.OwnerRef, req.ID, verifiedBudgetHMC, req.BudgetRuns, escrowFP, attest, time.Now().Unix(), requireHub) {
				if requireHub {
					http.Error(w, "prepaid_attestation invalid or missing (hub-signed ticket required; self-MAC refused)", http.StatusBadRequest)
				} else {
					http.Error(w, "prepaid_attestation invalid or missing (open escrow then attest before publish)", http.StatusBadRequest)
				}
				return
			}
			prepaidOK = true
			if escrowFP != "" {
				req.Config["escrow_fingerprint"] = escrowFP
			}
			// Force safe lifecycle — register upsert must not cancel/complete via publish.
			st := strings.ToLower(strings.TrimSpace(req.Status))
			if st == "" || st == "planned" {
				req.Status = st
				if st == "" {
					req.Status = "running"
				}
			} else {
				req.Status = "running"
			}
			if !publishRateAllow(r) {
				http.Error(w, "orders publish rate limit exceeded", http.StatusTooManyRequests)
				return
			}
		}
		// Admin registration on a minting hub: the pool pays miners from an escrow, and only
		// an escrow on the hub ledger pays them for real. An escrow on another node only
		// credits that node's local accounts (2026-11-12: bootstrap/lab campaigns closed
		// ~34k+ shards "paid" with no HMC reaching miners). The publish path already
		// requires a live hub escrow; the admin path used to skip it.
		if isAdmin && poolsync.RequireHubPrepaidTicket() && !poolsync.PrepaidAlgebraicMintAllowed() {
			exists, eerr := pf.CampaignExists(r.Context(), req.ID)
			if eerr != nil {
				http.Error(w, eerr.Error(), http.StatusInternalServerError)
				return
			}
			if !exists {
				hubBase := poolsync.HubEscrowProbeURL("")
				hubTok := adminToken
				if wm != nil {
					hubBase = poolsync.HubEscrowProbeURL(wm.ordersProbeURL)
					if t := wm.ordersAdminToken(); t != "" {
						hubTok = t
					}
				}
				row, ferr := poolsync.FetchHubFuzzEscrow(r.Context(), hubBase, hubTok, req.ID)
				if ferr != nil || row == nil || !strings.EqualFold(strings.TrimSpace(row.Status), "open") {
					msg := "no open escrow"
					if ferr != nil {
						msg = ferr.Error()
					}
					http.Error(w, "hub_escrow_required: pool campaigns must be backed by an open escrow on the hub ledger ("+msg+
						"); escrows on other nodes do not pay miners on chain", http.StatusConflict)
					return
				}
				// The escrow must also cover what is being registered: work items = budget_runs, and the
				// advertised budget must not exceed what is locked. A dust escrow under a large campaign
				// would have miners work shards that can never be paid (fimasw, 2026-11-13).
				claimHMC := publishBudgetHMCFromReq(req.BudgetHMC, req.Config)
				if row.BudgetHMC+1e-9 < claimHMC || req.BudgetRuns > row.BudgetRuns {
					http.Error(w, fmt.Sprintf("hub_escrow_required: hub escrow covers %.8f HMC / %d runs, registration claims %.8f HMC / %d runs",
						row.BudgetHMC, row.BudgetRuns, claimHMC, req.BudgetRuns), http.StatusConflict)
					return
				}
			}
		}
		// Dig (non-Hunt) campaigns get the same depth pack/mutator/seed finalize as local B2B.
		if !poolfuzz.IsHuntCampaign(req.Config) {
			repoRoot := strings.TrimSpace(os.Getenv("HACKME_REPO_ROOT"))
			if repoRoot == "" {
				if wd, err := os.Getwd(); err == nil {
					repoRoot = wd
				}
			}
			// Tenant-scope Dig corpus namespaces (owner:<ref>:pack:<id>).
			if strings.TrimSpace(req.OwnerRef) != "" {
				if req.Config == nil {
					req.Config = map[string]any{}
				}
				req.Config["owner_ref"] = strings.TrimSpace(req.OwnerRef)
			}
			pack := configString(req.Config, "guard_pack", "guard_name")
			pkg := configString(req.Config, "dig_package")
			req.Config = fuzzingcli.FinalizeDigCampaignConfig(req.Config, pkg, pack, repoRoot)
		}
		publishIdempotent := false
		if isPublish && !isAdmin {
			stripPublishEscalationConfig(req.Config)
			req.Config["orders_publish"] = true
			if prepaidOK {
				req.Config["budget_hmc"] = verifiedBudgetHMC
				req.Config["prepaid_attested"] = true
			}
			exists, err := pf.CampaignExists(r.Context(), req.ID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if exists {
				pub, err := pf.CampaignIsOrdersPublish(r.Context(), req.ID)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				if !pub {
					http.Error(w, "campaign id exists (not orders_publish) — publish cannot hijack", http.StatusConflict)
					return
				}
				okOwner, err := loadPublishOwnerGate(r, pf, req.ID)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				existingOwner, err := pf.CampaignOwnerRef(r.Context(), req.ID)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				reqOwner := strings.TrimSpace(req.OwnerRef)
				if existingOwner != "" {
					if reqOwner == "" || reqOwner != existingOwner || !okOwner {
						http.Error(w, "campaign id exists under a different owner_ref — publish cannot hijack", http.StatusConflict)
						return
					}
				} else if reqOwner == "" {
					http.Error(w, "owner_ref required to refresh existing orders_publish campaign", http.StatusConflict)
					return
				} else if err := pf.StampCampaignOwnerRefIfEmpty(r.Context(), req.ID, reqOwner); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				publishIdempotent = true
				// Legacy rows minted before owner-secret: issue once on authenticated refresh.
				existingHash, err := pf.CampaignOwnerSecretHash(r.Context(), req.ID)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				if strings.TrimSpace(existingHash) == "" {
					sec, hash, err := newOrdersOwnerSecret()
					if err != nil {
						http.Error(w, "owner secret mint failed", http.StatusInternalServerError)
						return
					}
					stamped, err := pf.StampOrdersOwnerSecretHash(r.Context(), req.ID, hash)
					if err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					if stamped {
						issuedOwnerSecret = sec
					}
				}
			} else {
				sec, hash, err := newOrdersOwnerSecret()
				if err != nil {
					http.Error(w, "owner secret mint failed", http.StatusInternalServerError)
					return
				}
				issuedOwnerSecret = sec
				req.Config["orders_owner_secret_hash"] = hash
			}
		}
		if !publishIdempotent {
			if err := pf.RegisterCampaign(r.Context(), poolfuzz.Campaign{
				ID:            req.ID,
				CampaignType:  req.CampaignType,
				Title:         req.Title,
				Description:   req.Description,
				OwnerRef:      req.OwnerRef,
				Status:        req.Status,
				BudgetRuns:    req.BudgetRuns,
				BudgetSeconds: req.BudgetSeconds,
				Config:        req.Config,
			}); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		campaignID := req.ID
		// Enqueue pool work only after prepaid attestation (publish) or for admin.
		if !isPublish || isAdmin || prepaidOK {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				_ = pf.EnsureWorkItems(ctx, campaignID, time.Now().Unix())
			}()
		}
		attach := map[string]any{"ok": false, "skipped": true, "reason": "no_work_manager"}
		// Publish token must never attach PoH escrow (admin-only escalation).
		if isPublish && !isAdmin {
			attach = map[string]any{"ok": false, "skipped": true, "reason": "orders_publish_no_poh_attach"}
			if publishIdempotent {
				attach["idempotent"] = true
			}
		} else if wm != nil {
			attach = wm.attachPoHOrderFromFuzzConfig(req.ID, req.Config)
		}
		wantAttach := false
		if req.Config != nil && !(isPublish && !isAdmin) {
			wantAttach = configTruthy(req.Config, "attach_poh_order", "create_poh_order")
		}
		if wantAttach {
			okAttach, _ := attach["ok"].(bool)
			skipped, _ := attach["skipped"].(bool)
			if !okAttach && !skipped {
				// Roll back campaign + work so a failed PoH escrow cannot leave claimable pool work.
				_ = pf.SetCampaignStatus(r.Context(), req.ID, "cancelled")
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusPaymentRequired)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"ok":               false,
					"campaign_id":      req.ID,
					"pool_distributed": true,
					"attach_poh_order": attach,
					"error":            "poh_attach_failed",
					"reason":           attach["reason"],
					"rolled_back":      true,
				})
				return
			}
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		resp := map[string]any{
			"ok":               true,
			"campaign_id":      req.ID,
			"pool_distributed": true,
			"work_queue":       "async",
			"attach_poh_order": attach,
		}
		if prepaidOK {
			resp["budget_hmc"] = verifiedBudgetHMC
			resp["prepaid_attested"] = true
		}
		if issuedOwnerSecret != "" {
			resp["orders_owner_secret"] = issuedOwnerSecret
			resp["orders_owner_secret_header"] = "X-Hackme-Orders-Owner-Secret"
		}
		_ = json.NewEncoder(w).Encode(resp)
		return
	})

	mux.HandleFunc("/api/fuzz/work/claim", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !coordinatorWorkPOSTAuthed(r, adminToken, workerToken, allowInsecure) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
			http.Error(w, "coordinator authentication required", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCoordinatorJSONBodyBytes)
		var req struct {
			WorkerID        string `json:"worker_id"`
			MinerPubKey     string `json:"miner_pubkey"`
			MinerPubKeyEd   string `json:"miner_pubkey_ed25519"`
			MinerAddress    string `json:"miner_address"`
			WorkerVersion   string `json:"worker_version"`
			HuntHarnessExec string `json:"hunt_harness_exec"`
			HuntEngines     string `json:"hunt_engines"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		workerID := strings.TrimSpace(req.WorkerID)
		if !validCoordinatorWorkerID(workerID) {
			http.Error(w, "invalid worker_id", http.StatusBadRequest)
			return
		}
		minVer := poolfuzz.MinWorkerVersion()
		if !poolfuzz.WorkerVersionAllowed(req.WorkerVersion, minVer) {
			wm.recordDrop("worker_outdated")
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":                 false,
				"reason":             "worker_outdated",
				"min_worker_version": minVer,
				"worker_version":     strings.TrimSpace(req.WorkerVersion),
			})
			return
		}
		pub := strings.TrimSpace(req.MinerPubKey)
		if pub == "" {
			pub = strings.TrimSpace(req.MinerPubKeyEd)
		}
		if okID, reasonID := wm.checkClaimMinerIdentity(workerID, pub, req.MinerAddress); !okID {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": reasonID})
			return
		}
		ipKey := clientIPKey(r)
		now := time.Now().Unix()
		// Peer/IP gate only until claim succeeds — then chargeClaimWorker.
		if ok, reason := wm.allowClaimPeer(workerID, ipKey, now); !ok {
			wm.recordDrop(reason)
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": reason})
			return
		}
		if ok, reason := wm.allowFuzzClaimByGHS(workerID, now); !ok {
			wm.recordDrop(reason)
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": reason})
			return
		}
		// Heartbeat only for existing workers or when under maxWorkers (PoH parity).
		if okSeen, reasonSeen := wm.touchWorkerSeenLimited(workerID); !okSeen {
			wm.recordDrop(reasonSeen)
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": reasonSeen})
			return
		}
		huntCapable := poolfuzz.HuntHarnessCapable(req.HuntHarnessExec)
		var work poolfuzz.ClaimedWork
		var ok bool
		var err error
		// Dig-only workers must not sticky-lease Hunt shards then 403 — skip/retry for Dig.
		claimOpts := poolfuzz.ClaimOptions{SkipHunt: !huntCapable}
		for try := 0; try < 8; try++ {
			work, ok, err = pf.ClaimWithOptions(r.Context(), workerID, now, claimOpts)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if !ok {
				break
			}
			isHunt := work.TaskClass == "hunt" || work.WorkKind == "hunt_shard"
			if isHunt && !huntCapable {
				_, _ = pf.ReleaseWorkLease(r.Context(), work.CampaignID, work.ItemID, workerID)
				if try == 0 {
					wm.recordDrop("worker_outdated_for_hunt")
				}
				ok = false
				continue
			}
			if isHunt {
				if soft, reason := applyHuntEngineClaimGate(&work, req.HuntEngines); soft {
					_, _ = pf.ReleaseWorkLease(r.Context(), work.CampaignID, work.ItemID, workerID)
					wm.recordDrop(reason)
					ok = false
					continue
				}
			}
			break
		}
		if !ok {
			wm.recordDrop("no_fuzz_work")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(noFuzzWorkBody(r.Context(), pf, huntCapable, nil))
			return
		}
		// Bind payout lock only after a fully successful claim (not on no_fuzz_work /
		// rate-limit / harness reject — those must not squat an unlocked id).
		wm.bindClaimPayoutFromPub(workerID, pub)
		if okCharge, reasonCharge := wm.chargeClaimWorker(workerID, now); !okCharge {
			// Lease already held — release so a rate-limited success does not stick.
			_, _ = pf.ReleaseWorkLease(r.Context(), work.CampaignID, work.ItemID, workerID)
			wm.recordDrop(reasonCharge)
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": reasonCharge})
			return
		}
		wm.noteWorkerClientIP(workerID, ipKey)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(fuzzClaimPayload(workerID, work))
	})

	mux.HandleFunc("/api/fuzz/work/claim_batch", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !coordinatorWorkPOSTAuthed(r, adminToken, workerToken, allowInsecure) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
			http.Error(w, "coordinator authentication required", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCoordinatorJSONBodyBytes)
		var req struct {
			WorkerID        string `json:"worker_id"`
			MinerPubKey     string `json:"miner_pubkey"`
			MinerPubKeyEd   string `json:"miner_pubkey_ed25519"`
			MinerAddress    string `json:"miner_address"`
			WorkerVersion   string `json:"worker_version"`
			HuntHarnessExec string `json:"hunt_harness_exec"`
			HuntEngines     string `json:"hunt_engines"`
			Limit           int    `json:"limit"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		workerID := strings.TrimSpace(req.WorkerID)
		if !validCoordinatorWorkerID(workerID) {
			http.Error(w, "invalid worker_id", http.StatusBadRequest)
			return
		}
		minVer := poolfuzz.MinWorkerVersion()
		if !poolfuzz.WorkerVersionAllowed(req.WorkerVersion, minVer) {
			wm.recordDrop("worker_outdated")
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": false, "reason": "worker_outdated", "min_worker_version": minVer,
			})
			return
		}
		pub := strings.TrimSpace(req.MinerPubKey)
		if pub == "" {
			pub = strings.TrimSpace(req.MinerPubKeyEd)
		}
		if okID, reasonID := wm.checkClaimMinerIdentity(workerID, pub, req.MinerAddress); !okID {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": reasonID})
			return
		}
		ipKey := clientIPKey(r)
		now := time.Now().Unix()
		if ok, reason := wm.allowFuzzClaimByGHS(workerID, now); !ok {
			wm.recordDrop(reason)
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": reason})
			return
		}
		if okSeen, reasonSeen := wm.touchWorkerSeenLimited(workerID); !okSeen {
			wm.recordDrop(reasonSeen)
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": reasonSeen})
			return
		}
		limit := req.Limit
		if limit < 1 {
			limit = 1
		}
		if limit > poolfuzz.MaxBatchClaimSubmit {
			limit = poolfuzz.MaxBatchClaimSubmit
		}
		// Capacity uses the compile-time cap so allocation size is not attacker-controlled
		// (CodeQL go/uncontrolled-allocation-size); loop still respects capped limit.
		items := make([]map[string]any, 0, poolfuzz.MaxBatchClaimSubmit)
		batchHuntCapable := poolfuzz.HuntHarnessCapable(req.HuntHarnessExec)
		batchOpts := poolfuzz.ClaimOptions{SkipHunt: !batchHuntCapable}
		softSkips := 0
		for i := 0; i < limit; i++ {
			// Charge IP claim bucket per lease (not once per batch) so multi-id
			// clients cannot pull up to 16 leases for a single IP slot.
			if ok, reason := wm.allowClaimPeer(workerID, ipKey, now); !ok {
				wm.recordDrop(reason)
				if len(items) == 0 {
					w.WriteHeader(http.StatusTooManyRequests)
					_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": reason})
					return
				}
				break
			}
			work, ok, err := pf.ClaimWithOptions(r.Context(), workerID, now, batchOpts)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if !ok {
				break
			}
			isHunt := work.TaskClass == "hunt" || work.WorkKind == "hunt_shard"
			if isHunt && !batchHuntCapable {
				// Defensive: SkipHunt already filters at selection.
				_, _ = pf.ReleaseWorkLease(r.Context(), work.CampaignID, work.ItemID, workerID)
				wm.recordDrop("worker_outdated_for_hunt")
				break
			}
			if isHunt {
				if soft, reason := applyHuntEngineClaimGate(&work, req.HuntEngines); soft {
					_, _ = pf.ReleaseWorkLease(r.Context(), work.CampaignID, work.ItemID, workerID)
					wm.recordDrop(reason)
					// The released shard comes straight back on the next Claim; stop after
					// a couple instead of churning the same lease up to `limit` times.
					if softSkips++; softSkips >= 2 {
						break
					}
					continue
				}
			}
			wm.bindClaimPayoutFromPub(workerID, pub)
			if okCharge, reasonCharge := wm.chargeClaimWorker(workerID, now); !okCharge {
				_, _ = pf.ReleaseWorkLease(r.Context(), work.CampaignID, work.ItemID, workerID)
				wm.recordDrop(reasonCharge)
				break
			}
			wm.noteWorkerClientIP(workerID, ipKey)
			items = append(items, fuzzClaimPayload(workerID, work))
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if len(items) == 0 {
			wm.recordDrop("no_fuzz_work")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(noFuzzWorkBody(r.Context(), pf, batchHuntCapable, []any{}))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "count": len(items), "items": items})
	})

	mux.HandleFunc("/api/fuzz/work/corpus_snapshot", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !coordinatorWorkPOSTAuthed(r, adminToken, workerToken, allowInsecure) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
			http.Error(w, "coordinator authentication required", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCoordinatorJSONBodyBytes)
		var req struct {
			WorkerID   string `json:"worker_id"`
			CampaignID string `json:"campaign_id"`
			ItemID     int64  `json:"item_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		workerID := strings.TrimSpace(req.WorkerID)
		if !validCoordinatorWorkerID(workerID) {
			http.Error(w, "invalid worker_id", http.StatusBadRequest)
			return
		}
		seeds, sha, err := pf.CorpusSnapshotForLease(r.Context(), workerID, req.CampaignID, req.ItemID)
		if err != nil {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":                     true,
			"corpus_snapshot_sha256": sha,
			"corpus_seeds":           fuzzengine.CorpusSeedsClaimMaps(seeds),
		})
	})

	mux.HandleFunc("/api/fuzz/work/submit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !coordinatorWorkPOSTAuthed(r, adminToken, workerToken, allowInsecure) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
			http.Error(w, "coordinator authentication required", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCoordinatorJSONBodyBytes)
		var req struct {
			WorkerID        string `json:"worker_id"`
			MinerAddress    string `json:"miner_address"`
			MinerPubKey     string `json:"miner_pubkey"`
			MinerPubKeyEd   string `json:"miner_pubkey_ed25519"` // PoH-lane alias (D8)
			MinerSig        string `json:"miner_sig"`
			MinerSigEd      string `json:"miner_sig_ed25519"` // PoH-lane alias
			MinerSigAlg     string `json:"miner_sig_alg"`
			SubmitNonce     uint64 `json:"submit_nonce"`
			WorkID          string `json:"work_id"`
			CampaignID      string `json:"campaign_id"`
			ItemID          int64  `json:"item_id"`
			InputN          uint64 `json:"input_n"`
			ActualInput     uint64 `json:"actual_input"`
			InputBytesHex   string `json:"input_bytes_hex"`
			CheckResult     int32  `json:"check_result"`
			DurationMS      int    `json:"duration_ms"`
			Trap            string `json:"trap"`
			SegmentExecDone int    `json:"segment_exec_done"`
			EdgesTouched    *int   `json:"edges_touched"`
			HuntEngine      string `json:"hunt_engine"`
			Engine          string `json:"engine"`
			EngineVersion   string `json:"engine_version"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.MinerPubKey) == "" {
			req.MinerPubKey = strings.TrimSpace(req.MinerPubKeyEd)
		}
		if strings.TrimSpace(req.MinerSig) == "" {
			req.MinerSig = strings.TrimSpace(req.MinerSigEd)
		}
		if strings.TrimSpace(req.WorkerID) == "" || strings.TrimSpace(req.CampaignID) == "" || req.ItemID <= 0 {
			http.Error(w, "invalid submit payload", http.StatusBadRequest)
			return
		}
		if !validCoordinatorWorkerID(strings.TrimSpace(req.WorkerID)) {
			http.Error(w, "invalid worker_id", http.StatusBadRequest)
			return
		}
		ipKey := clientIPKey(r)
		now := time.Now().Unix()
		// Report #28: charge IP/bans only here. Declared worker_id slot is charged
		// after signature + payout-lock (see below) so forged ids cannot freeze a miner.
		if okSub, reasonSub := wm.allowSubmitPeer(req.WorkerID, ipKey, now); !okSub {
			wm.recordDrop(reasonSub)
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": reasonSub})
			return
		}
		signPayload := poolfuzz.SubmitSignPayload{
			WorkerID: req.WorkerID, CampaignID: req.CampaignID, ItemID: req.ItemID,
			InputN: req.InputN, ActualInput: req.ActualInput, InputBytesHex: strings.TrimSpace(req.InputBytesHex),
			CheckResult: req.CheckResult, SubmitNonce: req.SubmitNonce, SegmentExecDone: req.SegmentExecDone,
			DurationMS: req.DurationMS,
		}
		if req.EdgesTouched != nil {
			signPayload.EdgesTouched = *req.EdgesTouched
			signPayload.EdgesTouchedOK = true
		}
		signBody := poolfuzz.CanonicalSubmitBytes(signPayload)
		okSig, reason, payoutAddr := wm.validateFuzzHybridSignature(fuzzSubmitAuth{
			WorkerID: req.WorkerID, MinerAddress: req.MinerAddress, MinerPubKey: req.MinerPubKey,
			MinerSig: req.MinerSig, MinerSigAlg: req.MinerSigAlg, SubmitNonce: req.SubmitNonce,
		}, signBody)
		if !okSig {
			// Rolling deploy: workers that predate duration/edges binding.
			okSig, reason, payoutAddr = wm.validateFuzzHybridSignature(fuzzSubmitAuth{
				WorkerID: req.WorkerID, MinerAddress: req.MinerAddress, MinerPubKey: req.MinerPubKey,
				MinerSig: req.MinerSig, MinerSigAlg: req.MinerSigAlg, SubmitNonce: req.SubmitNonce,
			}, poolfuzz.CanonicalSubmitBytesLegacy(signPayload))
		}
		if !okSig {
			wm.markSubmitOutcome(req.WorkerID, ipKey, reason, now)
			w.WriteHeader(submitRejectHTTPStatus(reason))
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": reason})
			return
		}
		// Check payout lock without committing address until Submit succeeds (M14).
		// Report #26 class fix: if worker_id is already payout-locked (claim-time PoP),
		// require a matching signed payout address — unsigned/forged-id submits cannot
		// ride an existing lease or rewrite the hunt claim.
		locked := wm.lockedPayoutAddress(strings.TrimSpace(req.WorkerID))
		if locked != "" {
			if payoutAddr == "" || !strings.EqualFold(locked, payoutAddr) {
				wm.markSubmitOutcome(req.WorkerID, ipKey, "payout_address_locked", now)
				// M14 reject: the signing key does not own this worker_id's claim-time
				// payout lock — either a forged worker_id (lease_owner is the victim) or
				// a rotated-away key. Do not release the lease on a foreign-key submit
				// (previously freed the shard, which let any pool token holder snipe a
				// victim lease by declaring its worker_id); TTL expiry reclaims it.
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": "payout_address_locked"})
				return
			}
		} else if payoutAddr != "" {
			// No durable lock yet — keep in-memory check for session-bound workers.
			wm.mu.Lock()
			memLocked := ""
			if wm.worker != nil {
				memLocked = strings.TrimSpace(wm.worker[req.WorkerID].PayoutAddress)
			}
			wm.mu.Unlock()
			if memLocked != "" && !strings.EqualFold(memLocked, payoutAddr) {
				wm.markSubmitOutcome(req.WorkerID, ipKey, "payout_address_locked", now)
				// Same M14 reject posture as the durable-lock branch above: strike
				// nobody, release nothing (the declared id is forgeable), and return
				// the short reason without echoing payout addresses.
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": "payout_address_locked"})
				return
			}
		}
		if okSub, reasonSub := wm.chargeSubmitWorker(req.WorkerID, now); !okSub {
			wm.recordDrop(reasonSub)
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": reasonSub})
			return
		}
		var inputBytes []byte
		if h := strings.TrimSpace(req.InputBytesHex); h != "" {
			inputBytes, _ = hex.DecodeString(h)
		}
		sub := poolfuzz.SubmitRequest{
			WorkerID:        req.WorkerID,
			MinerAddress:    payoutAddr,
			WorkID:          req.WorkID,
			CampaignID:      req.CampaignID,
			ItemID:          req.ItemID,
			InputN:          req.InputN,
			ActualInput:     req.ActualInput,
			InputBytes:      inputBytes,
			CheckResult:     req.CheckResult,
			DurationMS:      req.DurationMS,
			Trap:            strings.TrimSpace(req.Trap),
			SegmentExecDone: req.SegmentExecDone,
		}
		if req.EdgesTouched != nil {
			sub.EdgesTouched = *req.EdgesTouched
			sub.EdgesTouchedOK = true
		}
		eng := strings.TrimSpace(req.HuntEngine)
		if eng == "" {
			eng = strings.TrimSpace(req.Engine)
		}
		sub.HuntEngine = eng
		sub.EngineVersion = strings.TrimSpace(req.EngineVersion)
		out, err := pf.SubmitWithOutcome(r.Context(), sub)
		if err != nil {
			// Free lease only when identity is proven via payout lock match.
			// Unlocked forgeable ids must not snipe a victim shard on submit error
			// (TTL reclaim handles abandon); locked+matched means the real miner.
			locked := wm.lockedPayoutAddress(strings.TrimSpace(req.WorkerID))
			if locked != "" && payoutAddr != "" && strings.EqualFold(locked, payoutAddr) {
				_, _ = pf.ReleaseWorkLease(r.Context(), req.CampaignID, req.ItemID, req.WorkerID)
			}
			wm.markSubmitOutcome(req.WorkerID, ipKey, "fuzz_submit_failed", now)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Commit hybrid nonce + payout bind only after accepted work.
		// adoptFuzzPayoutLock matches notePayoutLock: prefetch unpaid, refuse
		// fleet-decoy plants, and pin the durable settle key on first adopt.
		if payoutAddr != "" {
			wm.adoptFuzzPayoutLock(req.WorkerID, payoutAddr)
			wm.commitFuzzHybridNonce(payoutAddr, req.SubmitNonce)
		} else {
			wm.touchWorkerSeen(req.WorkerID)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		resp := map[string]any{"ok": true, "accepted": true}
		if out.Async {
			w.WriteHeader(http.StatusAccepted)
			resp["async"] = true
			if out.QueueID > 0 {
				resp["queue_id"] = out.QueueID
			}
		}
		if out.ReplayStatus != "" {
			resp["replay_status"] = out.ReplayStatus
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/api/fuzz/work/submit_batch", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !coordinatorWorkPOSTAuthed(r, adminToken, workerToken, allowInsecure) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
			http.Error(w, "coordinator authentication required", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCoordinatorJSONBodyBytes)
		var req struct {
			WorkerID string `json:"worker_id"`
			Items    []struct {
				MinerAddress    string `json:"miner_address"`
				MinerPubKey     string `json:"miner_pubkey"`
				MinerPubKeyEd   string `json:"miner_pubkey_ed25519"`
				MinerSig        string `json:"miner_sig"`
				MinerSigEd      string `json:"miner_sig_ed25519"`
				MinerSigAlg     string `json:"miner_sig_alg"`
				SubmitNonce     uint64 `json:"submit_nonce"`
				WorkID          string `json:"work_id"`
				CampaignID      string `json:"campaign_id"`
				ItemID          int64  `json:"item_id"`
				InputN          uint64 `json:"input_n"`
				ActualInput     uint64 `json:"actual_input"`
				InputBytesHex   string `json:"input_bytes_hex"`
				CheckResult     int32  `json:"check_result"`
				DurationMS      int    `json:"duration_ms"`
				Trap            string `json:"trap"`
				SegmentExecDone int    `json:"segment_exec_done"`
				EdgesTouched    *int   `json:"edges_touched"`
			} `json:"items"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		workerID := strings.TrimSpace(req.WorkerID)
		if !validCoordinatorWorkerID(workerID) {
			http.Error(w, "invalid worker_id", http.StatusBadRequest)
			return
		}
		if len(req.Items) == 0 {
			http.Error(w, "items required", http.StatusBadRequest)
			return
		}
		if len(req.Items) > poolfuzz.MaxBatchClaimSubmit {
			req.Items = req.Items[:poolfuzz.MaxBatchClaimSubmit]
		}
		ipKey := clientIPKey(r)
		now := time.Now().Unix()
		results := make([]map[string]any, 0, len(req.Items))
		okN := 0
		for _, it := range req.Items {
			row := map[string]any{"item_id": it.ItemID, "campaign_id": it.CampaignID, "ok": false}
			if strings.TrimSpace(it.MinerPubKey) == "" {
				it.MinerPubKey = strings.TrimSpace(it.MinerPubKeyEd)
			}
			if strings.TrimSpace(it.MinerSig) == "" {
				it.MinerSig = strings.TrimSpace(it.MinerSigEd)
			}
			if okSub, reasonSub := wm.allowSubmitPeer(workerID, ipKey, now); !okSub {
				row["error"] = reasonSub
				results = append(results, row)
				continue
			}
			signPayload := poolfuzz.SubmitSignPayload{
				WorkerID: workerID, CampaignID: it.CampaignID, ItemID: it.ItemID,
				InputN: it.InputN, ActualInput: it.ActualInput, InputBytesHex: strings.TrimSpace(it.InputBytesHex),
				CheckResult: it.CheckResult, SubmitNonce: it.SubmitNonce, SegmentExecDone: it.SegmentExecDone,
				DurationMS: it.DurationMS,
			}
			if it.EdgesTouched != nil {
				signPayload.EdgesTouched = *it.EdgesTouched
				signPayload.EdgesTouchedOK = true
			}
			signBody := poolfuzz.CanonicalSubmitBytes(signPayload)
			okSig, reason, payoutAddr := wm.validateFuzzHybridSignature(fuzzSubmitAuth{
				WorkerID: workerID, MinerAddress: it.MinerAddress, MinerPubKey: it.MinerPubKey,
				MinerSig: it.MinerSig, MinerSigAlg: it.MinerSigAlg, SubmitNonce: it.SubmitNonce,
			}, signBody)
			if !okSig {
				okSig, reason, payoutAddr = wm.validateFuzzHybridSignature(fuzzSubmitAuth{
					WorkerID: workerID, MinerAddress: it.MinerAddress, MinerPubKey: it.MinerPubKey,
					MinerSig: it.MinerSig, MinerSigAlg: it.MinerSigAlg, SubmitNonce: it.SubmitNonce,
				}, poolfuzz.CanonicalSubmitBytesLegacy(signPayload))
			}
			if !okSig {
				wm.markSubmitOutcome(workerID, ipKey, reason, now)
				row["error"] = reason
				results = append(results, row)
				continue
			}
			locked := wm.lockedPayoutAddress(workerID)
			if locked != "" && (payoutAddr == "" || !strings.EqualFold(locked, payoutAddr)) {
				wm.markSubmitOutcome(workerID, ipKey, "payout_address_locked", now)
				row["error"] = "payout_address_locked"
				results = append(results, row)
				continue
			}
			if okSub, reasonSub := wm.chargeSubmitWorker(workerID, now); !okSub {
				row["error"] = reasonSub
				results = append(results, row)
				continue
			}
			var inputBytes []byte
			if h := strings.TrimSpace(it.InputBytesHex); h != "" {
				inputBytes, _ = hex.DecodeString(h)
			}
			sub := poolfuzz.SubmitRequest{
				WorkerID: workerID, MinerAddress: payoutAddr, WorkID: it.WorkID,
				CampaignID: it.CampaignID, ItemID: it.ItemID, InputN: it.InputN,
				ActualInput: it.ActualInput, InputBytes: inputBytes, CheckResult: it.CheckResult,
				DurationMS: it.DurationMS, Trap: strings.TrimSpace(it.Trap), SegmentExecDone: it.SegmentExecDone,
			}
			if it.EdgesTouched != nil {
				sub.EdgesTouched = *it.EdgesTouched
				sub.EdgesTouchedOK = true
			}
			out, err := pf.SubmitWithOutcome(r.Context(), sub)
			if err != nil {
				row["error"] = err.Error()
				results = append(results, row)
				continue
			}
			if payoutAddr != "" {
				wm.commitFuzzHybridNonce(payoutAddr, it.SubmitNonce)
				wm.touchWorkerSeen(workerID)
			} else {
				wm.touchWorkerSeen(workerID)
			}
			row["ok"] = true
			row["accepted"] = true
			if out.ReplayStatus != "" {
				row["replay_status"] = out.ReplayStatus
			}
			if out.Async {
				row["async"] = true
				if out.QueueID > 0 {
					row["queue_id"] = out.QueueID
				}
			}
			okN++
			results = append(results, row)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": okN > 0, "accepted": okN, "failed": len(results) - okN, "results": results,
		})
	})

	mux.HandleFunc("/api/fuzz/work/release", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !coordinatorWorkPOSTAuthed(r, adminToken, workerToken, allowInsecure) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
			http.Error(w, "coordinator authentication required", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCoordinatorJSONBodyBytes)
		var req struct {
			WorkerID      string `json:"worker_id"`
			CampaignID    string `json:"campaign_id"`
			ItemID        int64  `json:"item_id"`
			MinerPubKey   string `json:"miner_pubkey"`
			MinerPubKeyEd string `json:"miner_pubkey_ed25519"`
			MinerAddress  string `json:"miner_address"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		workerID := strings.TrimSpace(req.WorkerID)
		if !validCoordinatorWorkerID(workerID) {
			http.Error(w, "invalid worker_id", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.CampaignID) == "" || req.ItemID <= 0 {
			http.Error(w, "campaign_id and item_id required", http.StatusBadRequest)
			return
		}
		// Admin may release any lease; shared worker token must CHECK worker_id→payout
		// lock (anti-snipe) but must NOT create one — a failed/no-op release is not a
		// registration act (report #27 payout-lock poisoning).
		isAdmin := adminToken != "" && coordPublishRouteOK(r, adminToken)
		if !isAdmin {
			pub := strings.TrimSpace(req.MinerPubKey)
			if pub == "" {
				pub = strings.TrimSpace(req.MinerPubKeyEd)
			}
			if okID, reasonID := wm.checkReleaseMinerIdentity(workerID, pub, req.MinerAddress); !okID {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": reasonID})
				return
			}
			ipKey := clientIPKey(r)
			now := time.Now().Unix()
			// IP/ban gate only — never charge the declared worker claim bucket on
			// release (unlocked ids pass check-only identity with any pubkey).
			if ok, reason := wm.allowClaimPeer(workerID, ipKey, now); !ok {
				wm.recordDrop("release_" + reason)
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": reason})
				return
			}
		}
		released, err := pf.ReleaseWorkLease(r.Context(), req.CampaignID, req.ItemID, workerID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if !released {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": "lease_not_held", "released": false})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "released": true})
	})

	mux.HandleFunc("/api/fuzz/work/replay-status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !coordinatorWorkPOSTAuthed(r, adminToken, workerToken, allowInsecure) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
			http.Error(w, "coordinator authentication required", http.StatusUnauthorized)
			return
		}
		cid := strings.TrimSpace(r.URL.Query().Get("campaign_id"))
		itemStr := strings.TrimSpace(r.URL.Query().Get("item_id"))
		if cid == "" || itemStr == "" {
			http.Error(w, "campaign_id and item_id required", http.StatusBadRequest)
			return
		}
		itemID, err := strconv.ParseInt(itemStr, 10, 64)
		if err != nil || itemID <= 0 {
			http.Error(w, "invalid item_id", http.StatusBadRequest)
			return
		}
		st, err := pf.HuntReplayStatus(r.Context(), cid, itemID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(st)
	})

	mux.HandleFunc("/api/fuzz/pool/hunt/harness", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			// Admin or scoped Orders publish (customer Hunt custom-harness sync).
			isAdmin, isPublish, authed := campaignRegisterAuthed(r, adminToken, publishToken, allowInsecure)
			if !authed {
				writePublishAuthError(w)
				return
			}
			if isPublish && !isAdmin {
				if !publishRateAllow(r) {
					writeHarnessUploadAPIError(w, http.StatusTooManyRequests, "publish_rate_limited",
						"orders publish rate limit exceeded")
					return
				}
			}
			const harnessUploadMax = 36 << 20 // keep in sync with hunt.CoordinatorHarnessUploadMaxBytes
			r.Body = http.MaxBytesReader(w, r.Body, harnessUploadMax)
			ct := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
			var hash, sourceRel, wantContent string
			var data []byte
			var err error
			if ct == "application/octet-stream" || ct == "application/x-hunt-harness" {
				hash = strings.TrimSpace(r.Header.Get("X-Hackme-Harness-Hash"))
				sourceRel = strings.TrimSpace(r.Header.Get("X-Hackme-Source-Rel"))
				wantContent = strings.TrimSpace(r.Header.Get("X-Hackme-Harness-Content-SHA256"))
				data, err = io.ReadAll(r.Body)
				if err != nil {
					var maxErr *http.MaxBytesError
					if errors.As(err, &maxErr) {
						writeHarnessUploadAPIError(w, http.StatusRequestEntityTooLarge, "harness_too_large",
							fmt.Sprintf("harness exceeds coordinator max (%d bytes)", harnessUploadMax))
						return
					}
					http.Error(w, "read body failed", http.StatusBadRequest)
					return
				}
			} else {
				var req struct {
					HarnessHash          string `json:"harness_hash"`
					SourceRel            string `json:"source_rel"`
					BinaryB64            string `json:"binary_b64"`
					HarnessContentSHA256 string `json:"harness_content_sha256"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					var maxErr *http.MaxBytesError
					if errors.As(err, &maxErr) {
						writeHarnessUploadAPIError(w, http.StatusRequestEntityTooLarge, "harness_too_large",
							fmt.Sprintf("harness exceeds coordinator max (%d bytes)", harnessUploadMax))
						return
					}
					http.Error(w, "invalid json", http.StatusBadRequest)
					return
				}
				hash = strings.TrimSpace(req.HarnessHash)
				sourceRel = strings.TrimSpace(req.SourceRel)
				wantContent = strings.TrimSpace(req.HarnessContentSHA256)
				data, err = base64.StdEncoding.DecodeString(strings.TrimSpace(req.BinaryB64))
				if err != nil {
					http.Error(w, "invalid binary_b64", http.StatusBadRequest)
					return
				}
			}
			if int64(len(data)) > harnessUploadMax {
				writeHarnessUploadAPIError(w, http.StatusRequestEntityTooLarge, "harness_too_large",
					fmt.Sprintf("harness exceeds coordinator max (%d bytes)", harnessUploadMax))
				return
			}
			sum := sha256.Sum256(data)
			fp := hex.EncodeToString(sum[:])
			// HUNT-01: publish-token uploads must attest content_sha256 (admin may omit in lab).
			if isPublish && !isAdmin && wantContent == "" {
				http.Error(w, "harness_content_sha256 required for orders publish harness upload", http.StatusBadRequest)
				return
			}
			if wantContent != "" {
				wantContent = strings.ToLower(wantContent)
				if !hunt.ValidContentSHA256(wantContent) || wantContent != fp {
					http.Error(w, "harness_content_sha256 mismatch with body", http.StatusBadRequest)
					return
				}
			}
			if err := huntPutHarnessArtifact(r.Context(), pf.DB, hash, data, sourceRel); err != nil {
				lower := strings.ToLower(err.Error())
				if strings.Contains(lower, "exceeds") && strings.Contains(lower, "bytes") {
					writeHarnessUploadAPIError(w, http.StatusRequestEntityTooLarge, "harness_too_large", err.Error())
					return
				}
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true, "harness_hash": strings.TrimSpace(hash), "byte_size": len(data),
				"content_sha256": fp, "storage": huntHarnessStorageMode(),
			})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/fuzz/pool/hunt/harness/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Workers + admin: full fetch. Orders publish: HEAD for probe/verify; GET body
		// requires content attestation (HUNT-02 — shared publish token must not exfil
		// proprietary binaries by harness_hash alone).
		workOK := coordinatorWorkPOSTAuthed(r, adminToken, workerToken, allowInsecure)
		pubOK := !workOK && coordPublishOK(r, publishToken)
		if !workOK && !pubOK {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
			http.Error(w, "coordinator authentication required", http.StatusUnauthorized)
			return
		}
		hash := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/fuzz/pool/hunt/harness/"), "/")
		if hash == "" {
			http.Error(w, "harness hash required", http.StatusBadRequest)
			return
		}
		etag := `"` + strings.ToLower(strings.TrimSpace(hash)) + `"`
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "private, max-age=3600")
		if match := strings.TrimSpace(r.Header.Get("If-None-Match")); match != "" {
			for _, part := range strings.Split(match, ",") {
				if strings.TrimSpace(part) == etag || strings.TrimSpace(part) == "*" {
					w.WriteHeader(http.StatusNotModified)
					return
				}
			}
		}
		storedFP, fpErr := hunt.GetHarnessContentSHA256(r.Context(), pf.DB, hash)
		storedFP = strings.ToLower(strings.TrimSpace(storedFP))
		if pubOK && r.Method == http.MethodGet {
			if fpErr != nil || storedFP == "" {
				http.Error(w, "harness not found", http.StatusNotFound)
				return
			}
			want := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Hackme-Harness-Content-SHA256")))
			if want == "" || !hunt.ValidContentSHA256(want) || want != storedFP {
				http.Error(w, "harness_content_sha256 required for orders publish harness download", http.StatusForbidden)
				return
			}
		}
		if path, ok := hunt.GetHarnessArtifactPath(hash); ok {
			safe, allowOk := pathsafe.Allow(path)
			if allowOk && pathsafe.AbsRE.MatchString(safe) {
				disk := filepath.FromSlash(safe)
				// Never ServeFile: it follows symlinks. Open with O_NOFOLLOW so a planted
				// link in the harness store cannot exfiltrate outside the object root.
				f, openErr := os.OpenFile(disk, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
				if openErr == nil {
					st, stErr := f.Stat()
					if stErr == nil && st.Mode().IsRegular() {
						w.Header().Set("Content-Type", "application/octet-stream")
						if storedFP != "" {
							w.Header().Set("X-Hackme-Content-SHA256", storedFP)
						}
						http.ServeContent(w, r, st.Name(), st.ModTime(), f)
						_ = f.Close()
						return
					}
					_ = f.Close()
				}
			}
		}
		data, err := huntGetHarnessArtifact(r.Context(), pf.DB, hash)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		fp := storedFP
		if fp == "" {
			fp = hunt.ContentFingerprint(data)
		}
		w.Header().Set("X-Hackme-Content-SHA256", fp)
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			return
		}
		_, _ = w.Write(data)
	})
}

// applyHuntEngineClaimGate soft-skips or falls back to byte Hunt when the worker
// cannot run the campaign's coverage engine. soft=true means release the lease.
func applyHuntEngineClaimGate(work *poolfuzz.ClaimedWork, huntEnginesRaw string) (soft bool, reason string) {
	if work == nil || !poolfuzz.HuntEngineEnabled() {
		return false, ""
	}
	if !hunt.IsCoverageEngine(work.HuntEngine) {
		return false, ""
	}
	advertised := poolfuzz.WorkerHuntEngines(huntEnginesRaw)
	if poolfuzz.WorkerSupportsHuntEngine(advertised, work.HuntEngine) {
		return false, ""
	}
	cfgHint := map[string]any{
		"iterations_per_shard": work.IterationsPerShard,
		"hunt_corpus_guided":   work.CoverageKind == "hunt_corpus_guided",
		"hunt_engine_profile":  work.EngineProfile,
		"hunt_engine":          work.HuntEngine,
	}
	// Legacy workers omit hunt_engines — byte fallback only when explicitly enabled (default OFF).
	if len(advertised) == 0 && poolfuzz.HuntEngineAllowByteFallbackForConfig(cfgHint) {
		poolfuzz.ApplyHuntEngineByteFallback(work, cfgHint)
		return false, ""
	}
	if poolfuzz.HuntEngineAllowByteFallbackForConfig(cfgHint) && poolfuzz.WorkerSupportsHuntEngine(advertised, hunt.EngineByte) {
		poolfuzz.ApplyHuntEngineByteFallback(work, cfgHint)
		return false, ""
	}
	return true, "hunt_engine_soft_skip"
}

// noFuzzWorkBody is the empty-claim reply. A worker that cannot run Hunt is told when Hunt
// work is what is waiting (hint), so "no_fuzz_work" stops hiding a capability problem such
// as a missing clang/bwrap/afl-fuzz on the worker host.
func noFuzzWorkBody(ctx context.Context, pf *poolfuzz.Service, huntCapable bool, items []any) map[string]any {
	body := map[string]any{"ok": false, "reason": "no_fuzz_work"}
	if items != nil {
		body["items"] = items
	}
	if !huntCapable && pf != nil {
		if n, err := pf.PendingHuntItems(ctx); err == nil && n > 0 {
			body["hint"] = "hunt_capability_required"
			body["hunt_campaigns_pending"] = n
		}
	}
	return body
}

func fuzzClaimPayload(workerID string, work poolfuzz.ClaimedWork) map[string]any {
	payload := map[string]any{
		"ok":              true,
		"worker_id":       workerID,
		"work_id":         work.WorkID,
		"campaign_id":     work.CampaignID,
		"item_id":         work.ItemID,
		"input_n":         work.InputN,
		"actual_input":    work.ActualInput,
		"input_mode":      work.InputMode,
		"input_bytes_hex": hex.EncodeToString(work.InputBytes),
		"depth_tier":      work.DepthTier,
		"per_run_hmc":     work.PerRunHMC,
		"exec_per_unit":   work.ExecPerUnit,
		"max_input_bytes": work.MaxInputBytes,
		"coverage_kind":   work.CoverageKind,
		"wasm_check_hex":  work.WasmCheckHex,
		"check_semantics": work.CheckSemantics,
		"task_class":      "fuzz",
		"scheduler_mode":  "fuzz",
	}
	sha := strings.TrimSpace(work.CorpusSnapshotSHA256)
	if sha != "" {
		payload["corpus_snapshot_sha256"] = sha
	}
	if seeds := fuzzengine.CorpusSeedsClaimMaps(work.CorpusSeeds); len(seeds) > 0 {
		if !poolfuzz.ShouldOmitFatCorpusSeeds(sha, work.CorpusSeeds) {
			payload["corpus_seeds"] = seeds
		} else {
			payload["corpus_light"] = true
		}
	}
	if work.PowerMutCap > 0 {
		payload["power_mut_cap"] = work.PowerMutCap
	}
	if work.HavocDeepV28 {
		payload["havoc_deep_v28"] = true
	}
	if work.HavocDeepV210 {
		payload["havoc_deep_v210"] = true
	}
	if work.DigGPUMutators {
		payload["dig_gpu_mutators"] = true
	}
	if work.CorpusExploreV2 {
		payload["corpus_explore_v2"] = true
	}
	if len(work.SeedByteCorpus) > 0 {
		payload["seed_byte_corpus"] = work.SeedByteCorpus
	}
	if len(work.MutatorDict) > 0 {
		payload["mutator_dict_hex"] = hex.EncodeToString(work.MutatorDict)
	}
	if len(work.LibFuzzerDict) > 0 {
		payload["libfuzzer_dict_hex"] = hex.EncodeToString(work.LibFuzzerDict)
	}
	if work.TaskClass == "hunt" || work.WorkKind == "hunt_shard" {
		payload["task_class"] = "hunt"
		payload["work_kind"] = "hunt_shard"
		payload["harness_hash"] = work.HarnessHash
		payload["upstream_target_id"] = work.UpstreamTargetID
		payload["per_shard_hmc"] = work.PerRunHMC
		if src := strings.TrimSpace(work.HuntSource); src != "" {
			payload["hunt_source"] = src
		}
		if p := strings.TrimSpace(work.HuntPinPath); p != "" {
			payload["hunt_pin_path"] = p
		}
		if rel := strings.TrimSpace(work.HuntSourceRel); rel != "" {
			payload["hunt_source_rel"] = rel
		}
		if u := strings.TrimSpace(work.HarnessFetchURL); u != "" {
			payload["harness_fetch_url"] = u
		}
		if sha := strings.TrimSpace(work.HarnessContentSHA256); sha != "" {
			payload["harness_content_sha256"] = sha
		}
		payload["hunt_detect_leaks"] = work.HuntDetectLeaks
		payload["shard_spec"] = map[string]any{
			"iterations_per_shard": work.IterationsPerShard,
			"check_semantics":      work.CheckSemantics,
		}
		if eng := strings.TrimSpace(work.HuntEngine); eng != "" {
			payload["hunt_engine"] = eng
			payload["engine"] = eng
		}
		if work.EngineLeaseSec > 0 {
			payload["engine_lease_sec"] = work.EngineLeaseSec
		}
		if p := strings.TrimSpace(work.EngineProfile); p != "" {
			payload["engine_profile"] = p
		}
	}
	return payload
}

func startPoolFuzzTicker(ctx context.Context, pf *poolfuzz.Service) {
	if pf == nil {
		return
	}
	poolfuzz.StartHuntReplayWorkers(ctx, pf)
	tickEvery := 5 * time.Second
	if v := strings.TrimSpace(os.Getenv("HACKME_POOL_TICK_SEC")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 2 && n <= 60 {
			tickEvery = time.Duration(n) * time.Second
		}
	}
	go func() {
		t := time.NewTicker(tickEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := pf.Tick(ctx); err != nil {
					// best-effort
				}
				// Drain settle HTTP off the submit/finalize hot path when enqueue-only.
				if rs, ok := pf.Settler.(*poolfuzz.RelaySettler); ok && rs != nil && rs.SkipInlineHTTP {
					_, _, _ = rs.DrainPendingSettleHTTP(ctx, 64)
				}
			}
		}
	}()
}
