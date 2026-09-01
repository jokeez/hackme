package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/poolfuzz"
)

func addFuzzPoolRoutes(mux *http.ServeMux, adminToken, workerToken string, allowInsecure bool, wm *workManager, pf *poolfuzz.Service) {
	if pf == nil {
		return
	}
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
		items, err := pf.ListPublicCampaigns(r.Context(), limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=15")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "campaigns": items})
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
		prog, err := pf.CampaignProgress(r.Context(), id)
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
		repaired, err := pf.RepairZombiePoolCampaigns(r.Context(), 50)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "cancelled": n, "repaired": repaired})
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
			WorkerID     string `json:"worker_id"`
			MinerPubKey  string `json:"miner_pubkey"`
			MinerAddress string `json:"miner_address"`
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
		if okID, reasonID := wm.checkClaimMinerIdentity(workerID, req.MinerPubKey, req.MinerAddress); !okID {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": reasonID})
			return
		}
		ipKey := clientIPKey(r)
		now := time.Now().Unix()
		if ok, reason := wm.allowClaim(workerID, ipKey, now); !ok {
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
		wm.noteWorkerClientIP(workerID, ipKey)
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
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
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
		if seeds := fuzzengine.CorpusSeedsClaimMaps(work.CorpusSeeds); len(seeds) > 0 {
			payload["corpus_seeds"] = seeds
		}
		if sha := strings.TrimSpace(work.CorpusSnapshotSHA256); sha != "" {
			payload["corpus_snapshot_sha256"] = sha
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
			payload["hunt_detect_leaks"] = work.HuntDetectLeaks
			payload["shard_spec"] = map[string]any{
				"iterations_per_shard": work.IterationsPerShard,
				"check_semantics":      work.CheckSemantics,
			}
		}
		_ = json.NewEncoder(w).Encode(payload)
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
			MinerSig        string `json:"miner_sig"`
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
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
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
		if okSub, reasonSub := wm.allowSubmit(req.WorkerID, ipKey, now); !okSub {
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
		if payoutAddr != "" {
			wm.mu.Lock()
			locked := ""
			if wm.worker != nil {
				locked = strings.TrimSpace(wm.worker[req.WorkerID].PayoutAddress)
			}
			wm.mu.Unlock()
			if locked != "" && !strings.EqualFold(locked, payoutAddr) {
				wm.markSubmitOutcome(req.WorkerID, ipKey, "payout_address_locked", now)
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"ok":                       false,
					"reason":                   payoutAddressLockedReason(locked, payoutAddr),
					"locked_payout_address":    locked,
					"submitted_payout_address": payoutAddr,
				})
				return
			}
		}
		var inputBytes []byte
		if h := strings.TrimSpace(req.InputBytesHex); h != "" {
			inputBytes, _ = hex.DecodeString(h)
		}
		out, err := pf.SubmitWithOutcome(r.Context(), poolfuzz.SubmitRequest{
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
		})
		if err != nil {
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
			st.LastSeenUnix = time.Now().Unix()
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
			resp["replay_status"] = out.ReplayStatus
			if out.QueueID > 0 {
				resp["queue_id"] = out.QueueID
			}
		}
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/api/fuzz/work/replay-status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
			var req struct {
				HarnessHash string `json:"harness_hash"`
				SourceRel   string `json:"source_rel"`
				BinaryB64   string `json:"binary_b64"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(req.BinaryB64))
			if err != nil {
				http.Error(w, "invalid binary_b64", http.StatusBadRequest)
				return
			}
			if err := huntPutHarnessArtifact(r.Context(), pf.DB, req.HarnessHash, data, req.SourceRel); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "harness_hash": strings.TrimSpace(req.HarnessHash), "byte_size": len(data)})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/fuzz/pool/hunt/harness/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
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
		data, err := huntGetHarnessArtifact(r.Context(), pf.DB, hash)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Cache-Control", "private, max-age=3600")
		_, _ = w.Write(data)
	})
}

func startPoolFuzzTicker(ctx context.Context, pf *poolfuzz.Service) {
	if pf == nil {
		return
	}
	poolfuzz.StartHuntReplayWorkers(ctx, pf)
	go func() {
		t := time.NewTicker(3 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := pf.Tick(ctx); err != nil {
					// best-effort
				}
			}
		}
	}()
}
