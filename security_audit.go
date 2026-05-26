package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"hackme/internal/chain"
	"hackme/internal/fuzzescrow"
	"hackme/internal/sandbox"
)

type securityAuditRequest struct {
	Title           string   `json:"title"`
	PayerRef        string   `json:"payer_ref"`
	OrderID         string   `json:"order_id"`
	CampaignID      string   `json:"campaign_id"`
	Language        string   `json:"language"`
	Code            string   `json:"code"`
	WasmCheckHex    string   `json:"wasm_check_hex"`
	BudgetHMC       float64  `json:"budget_hmc"`
	BudgetRuns      int      `json:"budget_runs"`
	BudgetSeconds   int      `json:"budget_seconds"`
	SeedCorpus      []uint64 `json:"seed_corpus"`
	CreatePoHOrder  *bool    `json:"create_poh_order"`
	RewardHMC       float64  `json:"reward_hmc"`
	DifficultyScore int      `json:"difficulty_score"`
	TargetSolves    int      `json:"target_solves"`
	PoolDistributed *bool    `json:"pool_distributed"`
}

func (a *app) handleSecurityAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	if !a.allowRate("security_audit:"+clientIP(r), 3) {
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited", "rate limited", nil)
		return
	}
	if !requireFuzzCampaignCreateAuth(w, r) {
		return
	}
	if hasValidAdminAuth(r) {
		logAdminAction(r, "security_audit_create")
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	defer r.Body.Close()
	var req securityAuditRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid json", nil)
		return
	}

	ts := time.Now().UTC().Format("20060102t150405")
	orderID := strings.TrimSpace(req.OrderID)
	if orderID == "" {
		orderID = "order-audit-" + ts
	} else {
		orderID = cleanFuzzID(orderID, "order")
	}
	campaignID := strings.TrimSpace(req.CampaignID)
	if campaignID == "" {
		campaignID = "campaign-audit-" + ts
	} else {
		campaignID = cleanFuzzID(campaignID, "campaign")
	}

	wasmHex := strings.TrimSpace(strings.ToLower(req.WasmCheckHex))
	var compileLog string
	if wasmHex == "" {
		code := strings.TrimSpace(req.Code)
		if code == "" {
			writeAPIError(w, http.StatusBadRequest, "guard_required", "provide wasm_check_hex or language+code", nil)
			return
		}
		lang := normalizeFromCodeLanguage(req.Language)
		if lang == "" {
			writeAPIError(w, http.StatusBadRequest, "language_required", "language required with code", nil)
			return
		}
		wasmBytes, _, _, logText, err := a.compileTaskFromCode(r.Context(), taskFromCodeRequest{
			ID:       orderID,
			Language: lang,
			Code:     code,
		})
		compileLog = logText
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "compile_failed", "compile failed", map[string]any{
				"compile_log": compileLog,
				"detail":      err.Error(),
			})
			return
		}
		wasmHex = hex.EncodeToString(wasmBytes)
	} else {
		raw, err := hex.DecodeString(wasmHex)
		if err != nil || len(raw) == 0 {
			writeAPIError(w, http.StatusBadRequest, "invalid_wasm_hex", "wasm_check_hex must be valid hex", nil)
			return
		}
		if err := sandbox.ValidateCheckWasm(r.Context(), raw); err != nil {
			writeAPIError(w, http.StatusBadRequest, "wasm_validation_failed", err.Error(), nil)
			return
		}
	}

	createPoH := true
	if req.CreatePoHOrder != nil {
		createPoH = *req.CreatePoHOrder
	}
	poolDist := true
	if req.PoolDistributed != nil {
		poolDist = *req.PoolDistributed
	}

	budgetHMC := req.BudgetHMC
	if budgetHMC <= 0 {
		budgetHMC = 1.0
	}
	if budgetHMC < fuzzescrow.MinCampaignBudgetHMC {
		writeAPIError(w, http.StatusBadRequest, "budget_too_low",
			fmt.Sprintf("budget_hmc must be >= %.2f", fuzzescrow.MinCampaignBudgetHMC), nil)
		return
	}
	budgetRuns := req.BudgetRuns
	if budgetRuns < 8 {
		budgetRuns = 64
	}
	budgetSeconds := req.BudgetSeconds
	if budgetSeconds < 60 {
		budgetSeconds = 3600
	}
	targetSolves := req.TargetSolves
	if targetSolves < 1 {
		targetSolves = 1
	}
	difficulty := req.DifficultyScore
	if difficulty < chain.MinDifficultyScore {
		difficulty = 10
	}
	rewardHMC := req.RewardHMC
	minReward := float64(difficulty) * chain.RewardPerDifficultyUnit
	if rewardHMC+1e-12 < minReward {
		rewardHMC = minReward
	}
	if rewardHMC <= 0 {
		rewardHMC = minReward
	}
	if createPoH {
		minPerSolve := chain.MinOrderPrepaidHMC / float64(targetSolves)
		if rewardHMC+1e-12 < minPerSolve {
			rewardHMC = minPerSolve
		}
	}
	payerRef := strings.TrimSpace(req.PayerRef)
	if payerRef == "" {
		payerRef = "audit:" + campaignID
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Security audit " + campaignID
	}

	resp := map[string]any{
		"ok":          true,
		"order_id":    orderID,
		"campaign_id": campaignID,
		"title":       title,
	}
	if compileLog != "" {
		resp["compile_log"] = compileLog
	}

	if createPoH {
		manifest := map[string]any{
			"id":               orderID,
			"kind":             "synthetic_poh_v1",
			"reward_hmc":       rewardHMC,
			"difficulty_score": difficulty,
			"target_solves":    targetSolves,
			"payer_ref":        payerRef,
			"wasm_check_hex":   wasmHex,
		}
		rawManifest, _ := json.Marshal(manifest)
		res, err := a.chain.InsertOrderTask(r.Context(), rawManifest)
		if err != nil {
			code := http.StatusBadRequest
			if errors.Is(err, chain.ErrInsufficientBalance) {
				code = http.StatusPaymentRequired
			} else if errors.Is(err, chain.ErrOrderEscrowRateLimited) {
				code = http.StatusTooManyRequests
			}
			writeAPIError(w, code, "order_failed", err.Error(), map[string]any{"order_id": orderID})
			return
		}
		resp["order"] = map[string]any{
			"id":            res.ID,
			"status":        "open",
			"prepaid_hmc":   res.PrepaidHMC,
			"balance_after": res.BalanceAfter,
		}
	} else {
		resp["order"] = nil
	}

	seeds := req.SeedCorpus
	if len(seeds) == 0 {
		seeds = []uint64{133452, 999001}
	}
	cfgMap := map[string]any{
		"pool_distributed": poolDist,
		"check_semantics":  "detector",
		"wasm_check_hex":   wasmHex,
		"seed_corpus":      seeds,
		"auto_runner":      "1",
		"budget_hmc":       budgetHMC,
		"escrow_split":     "20_80",
	}
	cfgMap = normalizeFuzzCampaignConfig(cfgMap, "property")
	cfg := marshalMapJSON(cfgMap)

	reportToken := newReportToken()
	reportTokenHashHex := reportTokenHash(reportToken)
	now := time.Now().Unix()
	ownerRef := payerRef

	err := execContextRetryBusy(r.Context(), a.db,
		`INSERT INTO fuzz_campaigns
		 (id, campaign_type, status, title, description, owner_ref, task_id, target_ref, budget_runs, budget_seconds, config_json, summary_json, report_token_hash, report_token_issued_at, created_at, started_at, completed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		campaignID, "property", "running", title, "", ownerRef, orderID, "", budgetRuns, budgetSeconds,
		cfg, "{}", reportTokenHashHex, now, now, now, 0)
	if err != nil {
		switch sqliteErrKind(err) {
		case "unique":
			writeAPIError(w, http.StatusConflict, "campaign_exists", "campaign id already exists", nil)
		default:
			writeAPIError(w, http.StatusInternalServerError, "campaign_failed", "fuzz campaign create failed", map[string]any{"detail": err.Error()})
		}
		return
	}

	escrow, err := a.chain.OpenFuzzEscrow(r.Context(), campaignID, budgetHMC, budgetRuns)
	if err != nil {
		_, _ = a.db.ExecContext(r.Context(), `DELETE FROM fuzz_campaigns WHERE id=?`, campaignID)
		writeAPIError(w, http.StatusPaymentRequired, "escrow_failed", err.Error(), nil)
		return
	}

	c, err := a.getFuzzCampaign(r.Context(), campaignID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "load_failed", "campaign created but readback failed", nil)
		return
	}

	resp["campaign"] = c
	resp["escrow"] = escrow
	resp["customer_report_token"] = reportToken
	resp["customer_report_header"] = "X-Hackme-Report-Token"
	resp["fuzz_engine"] = fuzzEngineMetaFromConfig(cfgMap)
	resp["report_url"] = "/api/fuzz/campaigns/" + campaignID + "/report.html"

	if poolDistributedCampaign(cfgMap) {
		resp["pool_distributed"] = true
		fc := fuzzAutoCampaign{ID: campaignID, BudgetRuns: budgetRuns, BudgetSeconds: budgetSeconds, ConfigJSON: cfg}
		a.applyPoolSyncResponse(resp, r.Context(), fc)
	}

	writeJSON(w, resp)
}
