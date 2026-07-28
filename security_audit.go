package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"hackme/internal/chain"
	"hackme/internal/fuzzengine"
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
	UseSUPDiscount  *bool    `json:"use_sup_discount"`
	PayerWallet     string   `json:"payer_wallet"`
	BudgetRuns      int      `json:"budget_runs"`
	BudgetSeconds   int      `json:"budget_seconds"`
	SeedCorpus      []uint64 `json:"seed_corpus"`
	DepthTier       string   `json:"depth_tier"`
	GuardName       string   `json:"guard_name"`
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
	if !requireAdminAuthStrict(w, r) {
		return
	}
	logAdminAction(r, "security_audit_create")

	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	defer r.Body.Close()
	var req securityAuditRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid json", nil)
		return
	}

	ts := time.Now().UTC().Format("20060102t150405") + fmt.Sprintf("%06d", time.Now().Nanosecond()/1000)
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
		if !fromCodeEnabled() {
			writeAPIError(w, http.StatusForbidden, "from_code_disabled", "compile-from-code is disabled on this node; provide wasm_check_hex or set HACKME_FROM_CODE=1", nil)
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
			compileLog = enrichFromCodeCompileLog(compileLog, err)
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

	depthTier := fuzzengine.DepthWasmOnly
	if t := strings.TrimSpace(strings.ToLower(req.DepthTier)); t != "" {
		depthTier = fuzzengine.DepthTier(t)
	}

	budgetHMC := req.BudgetHMC
	budgetRuns := req.BudgetRuns
	if preset, ok := fuzzengine.DepthPresetFor(depthTier); ok && strings.TrimSpace(req.DepthTier) != "" {
		if budgetHMC <= 0 {
			budgetHMC = preset.BudgetHMC
		}
		if budgetRuns < 8 {
			budgetRuns = preset.BudgetRuns
		}
	}
	if budgetHMC <= 0 {
		budgetHMC = 1.0
	}
	if budgetHMC < fuzzescrow.MinCampaignBudgetHMC {
		writeAPIError(w, http.StatusBadRequest, "budget_too_low",
			fmt.Sprintf("budget_hmc must be >= %.2f", fuzzescrow.MinCampaignBudgetHMC), nil)
		return
	}
	if budgetRuns < 8 {
		budgetRuns = 64
	}
	budgetSeconds := req.BudgetSeconds
	if budgetSeconds < 60 {
		budgetSeconds = 3600
	}
	// Pool deep audits (1000+ runs) need more wall-clock than the default 1h or they
	// complete on budget_seconds with almost no pool worker progress.
	if poolDist && budgetRuns >= 256 {
		minSec := budgetRuns * 120
		if minSec > budgetSeconds {
			budgetSeconds = minSec
		}
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
	payerWallet := strings.TrimSpace(req.PayerWallet)
	if payerWallet == "" && strings.HasPrefix(payerRef, "HMC-") {
		payerWallet = payerRef
	}
	if payerWallet == "" {
		if addr, _, err := a.chain.Wallet(r.Context()); err == nil {
			payerWallet = strings.TrimSpace(addr)
		}
	}
	useSUP := req.UseSUPDiscount == nil || *req.UseSUPDiscount
	var supDiscountUsed float64
	escrowBudgetHMC := budgetHMC
	if useSUP && payerWallet != "" {
		nodeAddr := ""
		if addr, _, err := a.chain.Wallet(r.Context()); err == nil {
			nodeAddr = strings.TrimSpace(addr)
		}
		// C11: refuse unsigned burn of a third-party payer_wallet. Only discount the node wallet.
		if nodeAddr != "" && strings.EqualFold(payerWallet, nodeAddr) {
			supSt, err := a.chain.SupAddressState(r.Context(), payerWallet)
			if err == nil && supSt.BalanceSUP > 0 {
				cash, supUsed := chain.ApplyAuditSUPDiscount(budgetHMC, supSt.BalanceSUP)
				if supUsed > 0 {
					units := chain.SUPToUnits(supUsed)
					if code, err := a.chain.BurnSUPForService(r.Context(), payerWallet, units, "security_audit:"+campaignID); err == nil && code == "" {
						escrowBudgetHMC = cash
						supDiscountUsed = supUsed
					}
				}
			}
		}
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

	// Local PoH escrow when not handing attach to the pool coordinator.
	// Pool fleets probe ORDERS_URL (command chain), not a remote customer SQLite —
	// so pool_distributed + create_poh_order attach via coordinator when sync is configured.
	attachPoolPoH := createPoH && poolDist && poolSyncCoordinatorConfigured()
	if createPoH && !attachPoolPoH {
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
		var res *chain.InsertOrderResult
		var err error
		for attempt := 0; attempt < 6; attempt++ {
			if attempt > 0 {
				time.Sleep(time.Duration(attempt*50) * time.Millisecond)
			}
			res, err = a.chain.InsertOrderTask(r.Context(), rawManifest)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), "database is locked") {
				break
			}
		}
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
	} else if attachPoolPoH {
		resp["order"] = map[string]any{
			"id":               orderID,
			"status":           "attach_pending",
			"attach_via_pool":  true,
			"reward_hmc":       rewardHMC,
			"target_solves":    targetSolves,
			"difficulty_score": difficulty,
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
		"depth_tier":       string(depthTier),
	}
	if raw, err := hex.DecodeString(strings.TrimSpace(wasmHex)); err == nil && len(raw) > 0 {
		sum := sha256.Sum256(raw)
		cfgMap["wasm_sha256"] = hex.EncodeToString(sum[:])
		cfgMap["artifact_hash"] = hex.EncodeToString(sum[:])
	}
	if attachPoolPoH {
		cfgMap["attach_poh_order"] = true
		cfgMap["create_poh_order"] = true
		cfgMap["poh_order_id"] = orderID
		cfgMap["poh_reward_hmc"] = rewardHMC
		cfgMap["poh_target_solves"] = targetSolves
		cfgMap["poh_difficulty_score"] = difficulty
		cfgMap["poh_payer_ref"] = payerRef
	}
	if strings.HasPrefix(strings.ToLower(payerRef), "gate:") ||
		strings.EqualFold(title, "pool-sync-gate") ||
		strings.Contains(strings.ToLower(title), "pool sync") && strings.Contains(strings.ToLower(title), "gate") {
		cfgMap["internal_gate"] = true
		cfgMap["auto_runner"] = "0"
	}
	cfgMap = fuzzengine.ApplyDepthTier(cfgMap, depthTier)
	if gn := strings.TrimSpace(req.GuardName); gn != "" {
		cfgMap["guard_name"] = gn
		cfgMap["upstream_guard"] = gn
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

	escrow, err := openFuzzEscrowRetry(r.Context(), a.chain, campaignID, escrowBudgetHMC, budgetRuns)
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
	if supDiscountUsed > 0 {
		resp["sup_discount_used"] = supDiscountUsed
		resp["budget_hmc_cash"] = escrowBudgetHMC
		resp["budget_hmc_nominal"] = budgetHMC
	}
	resp["customer_report_token"] = reportToken
	resp["customer_report_header"] = "X-Hackme-Report-Token"
	resp["fuzz_engine"] = fuzzEngineMetaFromConfig(cfgMap)
	resp["depth_tier"] = string(depthTier)
	resp["report_url"] = "/api/fuzz/campaigns/" + campaignID + "/report.html"

	if poolDistributedCampaign(cfgMap) {
		resp["pool_distributed"] = true
		fc := fuzzAutoCampaign{ID: campaignID, BudgetRuns: budgetRuns, BudgetSeconds: budgetSeconds, ConfigJSON: cfg}
		a.applyPoolSyncResponse(resp, r.Context(), fc)
	}

	writeJSON(w, resp)
}

func openFuzzEscrowRetry(ctx context.Context, ch *chain.Service, campaignID string, budgetHMC float64, budgetRuns int) (*chain.FuzzEscrowRow, error) {
	var escrow *chain.FuzzEscrowRow
	var err error
	for attempt := 0; attempt < 6; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*50) * time.Millisecond)
		}
		escrow, err = ch.OpenFuzzEscrow(ctx, campaignID, budgetHMC, budgetRuns)
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "database is locked") {
			return escrow, err
		}
	}
	return escrow, err
}
