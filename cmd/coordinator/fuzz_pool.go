package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

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
		items, err := pf.ListPublicCampaigns(r.Context(), limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=15")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "campaigns": items})
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
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "campaign_id": req.ID, "pool_distributed": true, "work_queue": "async"})
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
			WorkerID string `json:"worker_id"`
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
		ipKey := clientIPKey(r)
		now := time.Now().Unix()
		if ok, reason := wm.allowClaim(workerID, ipKey, now); !ok {
			wm.recordDrop(reason)
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": reason})
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
		wm.noteWorkerClientIP(workerID, ipKey)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
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
			"wasm_check_hex":  work.WasmCheckHex,
			"check_semantics": work.CheckSemantics,
			"task_class":      "fuzz",
			"scheduler_mode":  "fuzz",
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
			WorkerID     string `json:"worker_id"`
			MinerAddress string `json:"miner_address"`
			MinerPubKey  string `json:"miner_pubkey"`
			MinerSig     string `json:"miner_sig"`
			MinerSigAlg  string `json:"miner_sig_alg"`
			SubmitNonce  uint64 `json:"submit_nonce"`
			WorkID       string `json:"work_id"`
			CampaignID   string `json:"campaign_id"`
			ItemID       int64  `json:"item_id"`
			InputN       uint64 `json:"input_n"`
			ActualInput  uint64 `json:"actual_input"`
			InputBytesHex string `json:"input_bytes_hex"`
			CheckResult  int32  `json:"check_result"`
			DurationMS   int    `json:"duration_ms"`
			Trap         string `json:"trap"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.WorkerID) == "" || strings.TrimSpace(req.CampaignID) == "" || req.ItemID <= 0 {
			http.Error(w, "invalid submit payload", http.StatusBadRequest)
			return
		}
		signBody := poolfuzz.CanonicalSubmitBytes(poolfuzz.SubmitSignPayload{
			WorkerID: req.WorkerID, CampaignID: req.CampaignID, ItemID: req.ItemID,
			InputN: req.InputN, ActualInput: req.ActualInput, CheckResult: req.CheckResult, SubmitNonce: req.SubmitNonce,
		})
		okSig, reason, payoutAddr := wm.validateFuzzHybridSignature(fuzzSubmitAuth{
			WorkerID: req.WorkerID, MinerAddress: req.MinerAddress, MinerPubKey: req.MinerPubKey,
			MinerSig: req.MinerSig, MinerSigAlg: req.MinerSigAlg, SubmitNonce: req.SubmitNonce,
		}, signBody)
		if !okSig {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "reason": reason})
			return
		}
		var inputBytes []byte
		if h := strings.TrimSpace(req.InputBytesHex); h != "" {
			inputBytes, _ = hex.DecodeString(h)
		}
		if err := pf.Submit(r.Context(), poolfuzz.SubmitRequest{
			WorkerID:     req.WorkerID,
			MinerAddress: payoutAddr,
			WorkID:       req.WorkID,
			CampaignID:   req.CampaignID,
			ItemID:       req.ItemID,
			InputN:       req.InputN,
			ActualInput:  req.ActualInput,
			InputBytes:   inputBytes,
			CheckResult:  req.CheckResult,
			DurationMS:   req.DurationMS,
			Trap:         strings.TrimSpace(req.Trap),
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "accepted": true})
	})
}

func startPoolFuzzTicker(ctx context.Context, pf *poolfuzz.Service) {
	if pf == nil {
		return
	}
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
