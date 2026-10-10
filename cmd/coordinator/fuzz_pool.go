package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/fuzzingcli"
	"hackme/internal/hunt"
	"hackme/internal/poolfuzz"
)

// coordPoolReadOK allows the pool admin token or the worker token.
// Empty tokens are accepted only in explicit insecure mode (report #12).
func coordPoolReadOK(r *http.Request, adminToken, workerToken string, allowInsecure bool) bool {
	if strings.TrimSpace(adminToken) == "" && strings.TrimSpace(workerToken) == "" && allowInsecure {
		return true
	}
	if adminToken != "" && coordAdminOK(r, adminToken) {
		return true
	}
	if workerToken != "" && coordAdminOK(r, workerToken) {
		return true
	}
	return false
}

func addFuzzPoolRoutes(mux *http.ServeMux, adminToken, workerToken string, allowInsecure bool, wm *workManager, pf *poolfuzz.Service) {
	if pf == nil {
		return
	}
	var (
		listMu    sync.Mutex
		listCache []map[string]any
		listAt    time.Time
	)
	const listCacheTTL = 12 * time.Second
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
		force := strings.TrimSpace(r.URL.Query().Get("refresh")) == "1"
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
		if !coordPoolReadOK(r, adminToken, workerToken, allowInsecure) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		prog, err := pf.CampaignProgress(r.Context(), id)
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

	mux.HandleFunc("/api/fuzz/pool/settle/outbox", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if adminToken == "" && allowInsecure {
			// loopback dev
		} else if adminToken == "" || !coordAdminOK(r, adminToken) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
			http.Error(w, "admin authentication required", http.StatusUnauthorized)
			return
		}
		limit := 64
		if s := strings.TrimSpace(r.URL.Query().Get("limit")); s != "" {
			if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 500 {
				limit = n
			}
		}
		items, err := pf.ListPendingSettleOutbox(r.Context(), limit)
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
		if adminToken == "" && allowInsecure {
			// loopback dev
		} else if adminToken == "" || !coordAdminOK(r, adminToken) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
			http.Error(w, "admin authentication required", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCoordinatorJSONBodyBytes)
		var req struct {
			IDs []int64 `json:"ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		n, err := pf.AckSettleOutbox(r.Context(), req.IDs)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
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
		} else if adminToken == "" || !coordAdminOK(r, adminToken) {
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
		cap := wm.fuzzFleetCapacity(time.Now().Unix())
		st["fleet_capacity"] = cap
		st["fleet_hashrate_gh_s"] = cap.FleetHashrateGHS
		st["est_shards_per_hour"] = cap.EstShardsPerHour
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
		} else if adminToken == "" || !coordAdminOK(r, adminToken) {
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
		} else if adminToken == "" || !coordAdminOK(r, adminToken) {
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
		} else if adminToken == "" || !coordAdminOK(r, adminToken) {
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
		if _, err := pf.ReclaimExpiredLeases(r.Context(), time.Now().Unix()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "cancelled": n, "stuck_expired_leases": stuck,
			"missing_harness": missingHarness, "repaired": repaired,
		})
	})

	mux.HandleFunc("/api/fuzz/pool/campaigns/repair-zombies", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if adminToken == "" && allowInsecure {
			// loopback dev
		} else if adminToken == "" || !coordAdminOK(r, adminToken) {
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

	mux.HandleFunc("/api/fuzz/pool/campaigns", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if adminToken == "" && allowInsecure {
			// loopback dev
		} else if adminToken == "" || !coordAdminOK(r, adminToken) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
			http.Error(w, "admin authentication required", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCoordinatorJSONBodyBytes)
		var req struct {
			ID            string         `json:"id"`
			CampaignType  string         `json:"campaign_type"`
			Title         string         `json:"title"`
			Description   string         `json:"description"`
			OwnerRef      string         `json:"owner_ref"`
			Status        string         `json:"status"`
			BudgetRuns    int            `json:"budget_runs"`
			BudgetSeconds int            `json:"budget_seconds"`
			Config        map[string]any `json:"config"`
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
		// Dig (non-Hunt) campaigns get the same depth pack/mutator/seed finalize as local B2B.
		if !poolfuzz.IsHuntCampaign(req.Config) {
			repoRoot := strings.TrimSpace(os.Getenv("HACKME_REPO_ROOT"))
			if repoRoot == "" {
				if wd, err := os.Getwd(); err == nil {
					repoRoot = wd
				}
			}
			pack := configString(req.Config, "guard_pack", "guard_name")
			pkg := configString(req.Config, "dig_package")
			req.Config = fuzzingcli.FinalizeDigCampaignConfig(req.Config, pkg, pack, repoRoot)
		}
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
		campaignID := req.ID
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			_ = pf.EnsureWorkItems(ctx, campaignID, time.Now().Unix())
		}()
		attach := map[string]any{"ok": false, "skipped": true, "reason": "no_work_manager"}
		if wm != nil {
			attach = wm.attachPoHOrderFromFuzzConfig(req.ID, req.Config)
		}
		wantAttach := false
		if req.Config != nil {
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
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":               true,
			"campaign_id":      req.ID,
			"pool_distributed": true,
			"work_queue":       "async",
			"attach_poh_order": attach,
		})
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
		work, ok, err := pf.Claim(r.Context(), workerID, now)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !ok {
			wm.recordDrop("no_fuzz_work")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": "no_fuzz_work"})
			return
		}
		isHunt := work.TaskClass == "hunt" || work.WorkKind == "hunt_shard"
		if isHunt && !poolfuzz.HuntHarnessCapable(req.HuntHarnessExec) {
			_, _ = pf.ReleaseWorkLease(r.Context(), work.CampaignID, work.ItemID, workerID)
			wm.recordDrop("worker_outdated_for_hunt")
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":                false,
				"reason":            "worker_outdated_for_hunt",
				"need_hunt_harness": poolfuzz.HuntHarnessLibFuzzerOneshot,
				"got_hunt_harness":  strings.TrimSpace(req.HuntHarnessExec),
				"hint":              "rebuild/redeploy workerfuzz with libFuzzer one-shot RunInputDetailed",
			})
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
		items := make([]map[string]any, 0, limit)
		for i := 0; i < limit; i++ {
			work, ok, err := pf.Claim(r.Context(), workerID, now)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if !ok {
				break
			}
			isHunt := work.TaskClass == "hunt" || work.WorkKind == "hunt_shard"
			if isHunt && !poolfuzz.HuntHarnessCapable(req.HuntHarnessExec) {
				_, _ = pf.ReleaseWorkLease(r.Context(), work.CampaignID, work.ItemID, workerID)
				wm.recordDrop("worker_outdated_for_hunt")
				continue
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
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": "no_fuzz_work", "items": []any{}})
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
		signBody := poolfuzz.CanonicalSubmitBytes(poolfuzz.SubmitSignPayload{
			WorkerID: req.WorkerID, CampaignID: req.CampaignID, ItemID: req.ItemID,
			InputN: req.InputN, ActualInput: req.ActualInput, InputBytesHex: strings.TrimSpace(req.InputBytesHex),
			CheckResult: req.CheckResult, SubmitNonce: req.SubmitNonce, SegmentExecDone: req.SegmentExecDone,
		})
		okSig, reason, payoutAddr := wm.validateFuzzHybridSignature(fuzzSubmitAuth{
			WorkerID: req.WorkerID, MinerAddress: req.MinerAddress, MinerPubKey: req.MinerPubKey,
			MinerSig: req.MinerSig, MinerSigAlg: req.MinerSigAlg, SubmitNonce: req.SubmitNonce,
		}, signBody)
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
		if payoutAddr != "" {
			wm.mu.Lock()
			if wm.worker == nil {
				wm.worker = make(map[string]workerPayoutStat)
			}
			st := wm.worker[req.WorkerID]
			if locked := strings.TrimSpace(st.PayoutAddress); locked == "" {
				st.PayoutAddress = payoutAddr
			}
			st.SignedSubmits++
			ts := time.Now().Unix()
			st.LastSeenUnix = ts
			st.LastFuzzSeenUnix = ts
			wm.worker[req.WorkerID] = st
			wm.mu.Unlock()
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
			signBody := poolfuzz.CanonicalSubmitBytes(poolfuzz.SubmitSignPayload{
				WorkerID: workerID, CampaignID: it.CampaignID, ItemID: it.ItemID,
				InputN: it.InputN, ActualInput: it.ActualInput, InputBytesHex: strings.TrimSpace(it.InputBytesHex),
				CheckResult: it.CheckResult, SubmitNonce: it.SubmitNonce, SegmentExecDone: it.SegmentExecDone,
			})
			okSig, reason, payoutAddr := wm.validateFuzzHybridSignature(fuzzSubmitAuth{
				WorkerID: workerID, MinerAddress: it.MinerAddress, MinerPubKey: it.MinerPubKey,
				MinerSig: it.MinerSig, MinerSigAlg: it.MinerSigAlg, SubmitNonce: it.SubmitNonce,
			}, signBody)
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
		isAdmin := adminToken != "" && coordAdminOK(r, adminToken)
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
			if adminToken == "" && allowInsecure {
				// loopback dev
			} else if adminToken == "" || !coordAdminOK(r, adminToken) {
				w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
				http.Error(w, "admin authentication required", http.StatusUnauthorized)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 36<<20)
			ct := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
			var hash, sourceRel string
			var data []byte
			var err error
			if ct == "application/octet-stream" || ct == "application/x-hunt-harness" {
				hash = strings.TrimSpace(r.Header.Get("X-Hackme-Harness-Hash"))
				sourceRel = strings.TrimSpace(r.Header.Get("X-Hackme-Source-Rel"))
				data, err = io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, "read body failed", http.StatusBadRequest)
					return
				}
			} else {
				var req struct {
					HarnessHash string `json:"harness_hash"`
					SourceRel   string `json:"source_rel"`
					BinaryB64   string `json:"binary_b64"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					http.Error(w, "invalid json", http.StatusBadRequest)
					return
				}
				hash = strings.TrimSpace(req.HarnessHash)
				sourceRel = strings.TrimSpace(req.SourceRel)
				data, err = base64.StdEncoding.DecodeString(strings.TrimSpace(req.BinaryB64))
				if err != nil {
					http.Error(w, "invalid binary_b64", http.StatusBadRequest)
					return
				}
			}
			if err := huntPutHarnessArtifact(r.Context(), pf.DB, hash, data, sourceRel); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true, "harness_hash": strings.TrimSpace(hash), "byte_size": len(data),
				"storage": huntHarnessStorageMode(),
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
		if !coordinatorWorkPOSTAuthed(r, adminToken, workerToken, allowInsecure) {
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
		if path, ok := hunt.GetHarnessArtifactPath(hash); ok {
			w.Header().Set("Content-Type", "application/octet-stream")
			if fp, err := hunt.GetHarnessContentSHA256(r.Context(), pf.DB, hash); err == nil && fp != "" {
				w.Header().Set("X-Hackme-Content-SHA256", fp)
			}
			http.ServeFile(w, r, path)
			return
		}
		data, err := huntGetHarnessArtifact(r.Context(), pf.DB, hash)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Hackme-Content-SHA256", hunt.ContentFingerprint(data))
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			return
		}
		_, _ = w.Write(data)
	})
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
