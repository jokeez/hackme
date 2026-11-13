package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hackme/internal/chain"
	"hackme/internal/fuzzengine"
	"hackme/internal/fuzzescrow"
	"hackme/internal/fuzzingcli"
	"hackme/internal/pathsafe"
	"hackme/internal/sandbox"
)

type securityAuditRequest struct {
	Title            string   `json:"title"`
	PayerRef         string   `json:"payer_ref"`
	OrderID          string   `json:"order_id"`
	CampaignID       string   `json:"campaign_id"`
	Language         string   `json:"language"`
	Code             string   `json:"code"`
	WasmCheckHex     string   `json:"wasm_check_hex"`
	WasmPath         string   `json:"wasm_path"`  // relative .wasm under repo (tasks/artifacts/…)
	DigPreset        string   `json:"dig_preset"` // one-click named pack → shipped WASM
	BudgetHMC        float64  `json:"budget_hmc"`
	UseSUPDiscount   *bool    `json:"use_sup_discount"`
	PayerWallet      string   `json:"payer_wallet"`
	BudgetRuns       int      `json:"budget_runs"`
	BudgetSeconds    int      `json:"budget_seconds"`
	SeedCorpus       []uint64 `json:"seed_corpus"`
	SeedByteCorpus   []string `json:"seed_byte_corpus"`
	DepthTier        string   `json:"depth_tier"`
	GuardName        string   `json:"guard_name"`
	GuardPack        string   `json:"guard_pack"`
	InputMode        string   `json:"input_mode"`
	MaxInputBytes    int      `json:"max_input_bytes"`
	GuidedScheduling *bool    `json:"guided_scheduling"`
	MutationRounds   int      `json:"mutation_rounds"`
	ExecPerUnit      int      `json:"exec_per_unit"`
	PublicProof      *bool    `json:"public_proof"`
	CreatePoHOrder   *bool    `json:"create_poh_order"`
	RewardHMC        float64  `json:"reward_hmc"`
	DifficultyScore  int      `json:"difficulty_score"`
	TargetSolves     int      `json:"target_solves"`
	PoolDistributed  *bool    `json:"pool_distributed"`
	DigPackage       string   `json:"dig_package"`
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
	resolvedPreset := strings.TrimSpace(req.DigPreset)
	resolvedGuardPack := strings.TrimSpace(req.GuardPack)
	repoRoot := strings.TrimSpace(os.Getenv("HACKME_REPO_ROOT"))
	if repoRoot == "" {
		repoRoot = fuzzingcli.DiscoverRepoRoot()
	}
	if wasmHex == "" {
		code := strings.TrimSpace(req.Code)
		wasmPath := strings.TrimSpace(req.WasmPath)
		switch {
		case resolvedPreset != "" || resolvedGuardPack != "":
			pack, hexStr, preset, err := fuzzingcli.ResolveDigGuardWasm(repoRoot, resolvedPreset, resolvedGuardPack)
			if err != nil {
				codeName := "pack_wasm_unavailable"
				if strings.Contains(err.Error(), "unknown") {
					codeName = "unknown_dig_preset"
				}
				writeAPIError(w, http.StatusBadRequest, codeName, err.Error(), nil)
				return
			}
			raw, decErr := hex.DecodeString(hexStr)
			if decErr != nil || len(raw) == 0 {
				writeAPIError(w, http.StatusBadRequest, "pack_wasm_unavailable", "resolved pack wasm is empty", nil)
				return
			}
			if err := sandbox.ValidateCheckWasm(r.Context(), raw); err != nil {
				writeAPIError(w, http.StatusBadRequest, "wasm_validation_failed", err.Error(), nil)
				return
			}
			wasmHex = hexStr
			resolvedGuardPack = pack.ID
			if resolvedPreset == "" {
				resolvedPreset = preset.ID
			}
			if strings.TrimSpace(req.DigPackage) == "" && preset.DigPackage != "" {
				req.DigPackage = preset.DigPackage
			}
			if strings.TrimSpace(req.Title) == "" && preset.Title != "" {
				req.Title = "HackMe Dig · " + preset.Title
			}
			if strings.TrimSpace(req.GuardName) == "" {
				req.GuardName = pack.ID
			}
			// Apply pack defaults when caller did not set depth/budget (one-click path).
			if strings.TrimSpace(req.DepthTier) == "" && strings.TrimSpace(req.DigPackage) != "" {
				if pkg, perr := fuzzingcli.B2BPackageFor(req.DigPackage); perr == nil {
					req.DepthTier = string(pkg.DepthTier)
					if req.BudgetHMC <= 0 {
						req.BudgetHMC = pkg.BudgetHMC
					}
					if req.BudgetRuns < 8 {
						req.BudgetRuns = pkg.BudgetRuns
					}
					if req.BudgetSeconds < 60 {
						req.BudgetSeconds = pkg.BudgetSeconds
					}
					if req.PoolDistributed == nil {
						v := pkg.PoolDistributed
						req.PoolDistributed = &v
					}
					if req.CreatePoHOrder == nil {
						v := pkg.CreatePoHOrder
						req.CreatePoHOrder = &v
					}
					pkgAdj := fuzzingcli.AdjustPackageForPack(pkg, pack)
					if pkgAdj.BudgetRuns > 0 {
						req.BudgetRuns = pkgAdj.BudgetRuns
					}
					if pkgAdj.BudgetSeconds > 0 {
						req.BudgetSeconds = pkgAdj.BudgetSeconds
					}
					if req.MutationRounds <= 0 && pkgAdj.MutationRounds > 0 {
						req.MutationRounds = pkgAdj.MutationRounds
					}
				}
			}
			req.GuardPack = pack.ID
		case wasmPath != "":
			raw, err := readSecurityAuditWasmPath(repoRoot, wasmPath)
			if err != nil {
				writeAPIError(w, http.StatusBadRequest, "invalid_wasm_path", err.Error(), nil)
				return
			}
			if err := sandbox.ValidateCheckWasm(r.Context(), raw); err != nil {
				writeAPIError(w, http.StatusBadRequest, "wasm_validation_failed", err.Error(), nil)
				return
			}
			wasmHex = hex.EncodeToString(raw)
		case code != "":
			if !fromCodeEnabled() {
				writeAPIError(w, http.StatusForbidden, "from_code_disabled", "compile-from-code is disabled on this node; provide wasm_check_hex, dig_preset/guard_pack, or set HACKME_FROM_CODE=1", nil)
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
		default:
			writeAPIError(w, http.StatusBadRequest, "guard_required",
				"provide dig_preset/guard_pack (shipped WASM), wasm_check_hex, wasm_path, or language+code", nil)
			return
		}
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
	// Default pool_distributed=true for public audits, but fall back to local when the
	// caller left it unset and this node has no coordinator URL (CI / solo / unit tests).
	// Explicit pool_distributed=true still fails closed in campaign create.
	poolDist := true
	poolDistExplicit := req.PoolDistributed != nil
	if poolDistExplicit {
		poolDist = *req.PoolDistributed
	}
	if poolDist && !poolDistExplicit && !poolSyncCoordinatorConfigured() {
		poolDist = false
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
	if poolDist && rejectIfExceedsOrdersPublishCaps(w, budgetRuns, budgetSeconds) {
		return
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
	// SUP loyalty discount stays OFF until treasury can top up escrow for a real
	// cash cut. Opt-in alone would still burn SUP with no customer benefit and a
	// lying budget_hmc_cash — so we neither burn nor advertise a discount.
	useSUP := req.UseSUPDiscount != nil && *req.UseSUPDiscount
	var supDiscountUsed float64
	escrowBudgetHMC := budgetHMC
	cashBudgetHMC := budgetHMC
	if useSUP {
		// Do not log escrowBudgetHMC: it is derived from req.BudgetHMC (user input).
		log.Printf("security_audit: use_sup_discount ignored (no treasury top-up); full escrow retained")
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
	if resolvedPreset != "" {
		resp["dig_preset"] = resolvedPreset
	}
	if resolvedGuardPack != "" {
		resp["guard_pack"] = resolvedGuardPack
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
	digPkgHint := strings.TrimSpace(strings.ToLower(req.DigPackage))
	if digPkgHint == "" {
		digPkgHint = fuzzingcli.DigPackageFromDepthTier(depthTier)
	}
	escrowSplit := fuzzescrow.EscrowSplitForDigPackage(digPkgHint)
	cfgMap := map[string]any{
		"pool_distributed": poolDist,
		"check_semantics":  "detector",
		"wasm_check_hex":   wasmHex,
		"seed_corpus":      seeds,
		"auto_runner":      "1",
		"budget_hmc":       budgetHMC,
		"escrow_split":     escrowSplit,
		"depth_tier":       string(depthTier),
		"dig_package":      digPkgHint,
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
	if gp := strings.TrimSpace(req.GuardPack); gp != "" {
		if pack, err := fuzzingcli.GuardPackFor(gp); err == nil {
			cfgMap = fuzzingcli.ApplyPackConfig(cfgMap, pack)
		}
		cfgMap["guard_pack"] = gp
		if strings.TrimSpace(toString(cfgMap["guard_name"])) == "" {
			cfgMap["guard_name"] = gp
		}
	}
	if im := strings.TrimSpace(strings.ToLower(req.InputMode)); im != "" {
		cfgMap["input_mode"] = im
	}
	if req.MaxInputBytes > 0 {
		cfgMap["max_input_bytes"] = req.MaxInputBytes
	}
	// A guard the customer brought (not a shipped pack/preset): its violations are findings.
	if strings.TrimSpace(req.GuardPack) == "" && strings.TrimSpace(req.DigPreset) == "" {
		cfgMap["customer_guard"] = true
	}
	// The module's ABI decides the input mode: a check_bytes(ptr,len) guard driven in uint64
	// mode gets an 8-byte integer where it expects a pointer and length, so every run
	// "violates". The wizard always sends the tier default (uint64), so ignore it here.
	if toString(cfgMap["input_mode"]) != "bytes" {
		if raw, err := hex.DecodeString(wasmHex); err == nil && sandbox.WasmExportsCheckBytes(r.Context(), raw) {
			cfgMap["input_abi_detected"] = "check_bytes"
			cfgMap["input_mode"] = "bytes"
			if intFromAny(cfgMap["max_input_bytes"]) <= 8 {
				cfgMap["max_input_bytes"] = 1024 // the uint64 tier default is 8
			}
			if _, ok := cfgMap["guided_scheduling"]; !ok {
				cfgMap["guided_scheduling"] = true
			}
			if _, ok := cfgMap["coverage_kind"]; !ok {
				cfgMap["coverage_kind"] = "wasm_edge_bitmap"
			}
		}
	}
	// Bytes input needs the bytes tier (same pairing the wizard uses for bytes guard packs):
	// the WASM-only tiers are uint64-shaped.
	if toString(cfgMap["input_mode"]) == "bytes" {
		switch fuzzengine.ParseDepthTier(cfgMap) {
		case fuzzengine.DepthWasmOnly, fuzzengine.DepthWasmNative:
			cfgMap["depth_tier"] = string(fuzzengine.DepthBytesCorpus)
		}
	}
	if len(req.SeedByteCorpus) > 0 {
		asAny := make([]any, len(req.SeedByteCorpus))
		for i, s := range req.SeedByteCorpus {
			asAny[i] = s
		}
		cfgMap["seed_byte_corpus"] = asAny
	}
	if req.GuidedScheduling != nil {
		cfgMap["guided_scheduling"] = *req.GuidedScheduling
	}
	if req.MutationRounds > 0 {
		cfgMap["mutation_rounds"] = req.MutationRounds
	}
	if req.ExecPerUnit > 0 {
		cfgMap["exec_per_unit"] = req.ExecPerUnit
	}
	if req.PublicProof != nil && *req.PublicProof {
		cfgMap["public_proof"] = true
	}
	cfgMap = fuzzingcli.FinalizeDigCampaignConfig(cfgMap, digPkgHint, strings.TrimSpace(req.GuardPack), repoRoot)
	digPkgFinal := strings.TrimSpace(strings.ToLower(toString(cfgMap["dig_package"])))
	if digPkgFinal == "" {
		digPkgFinal = digPkgHint
	}
	escrowSplit = fuzzescrow.EscrowSplitForDigPackage(digPkgFinal)
	cfgMap["escrow_split"] = escrowSplit
	if minDig := fuzzescrow.MinBudgetHMCForDigPackage(digPkgFinal); escrowBudgetHMC+1e-12 < minDig {
		writeAPIError(w, http.StatusBadRequest, "budget_too_low",
			fmt.Sprintf("dig package %s requires budget_hmc >= %.2f", digPkgFinal, minDig), nil)
		return
	}
	cfgMap = normalizeFuzzCampaignConfig(cfgMap, "property")
	cfg := marshalMapJSON(cfgMap)

	reportToken := newReportToken()
	reportTokenHashHex := reportTokenHash(reportToken)
	now := time.Now().Unix()
	ownerRef := payerRef
	if ownerRef == "" {
		ownerRef = a.defaultOrdersOwnerRef() // pool publish needs an owner; the wizard sends none
	}

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

	if _, fundErr := a.ensureOrdersSpendableForBudget(r.Context(), escrowBudgetHMC); fundErr != nil {
		_, _ = a.db.ExecContext(r.Context(), `DELETE FROM fuzz_campaigns WHERE id=?`, campaignID)
		writeOrdersAutoFundOrEscrowFailed(w, fundErr)
		return
	}
	escrow, err := openFuzzEscrowRetrySplit(r.Context(), a.chain, campaignID, escrowBudgetHMC, budgetRuns, escrowSplit)
	if err != nil {
		_, _ = a.db.ExecContext(r.Context(), `DELETE FROM fuzz_campaigns WHERE id=?`, campaignID)
		writeFuzzEscrowFailed(w, err)
		return
	}

	c, err := a.getFuzzCampaign(r.Context(), campaignID)
	if err != nil {
		a.rollbackNewCampaignEscrow(r.Context(), campaignID)
		writeAPIError(w, http.StatusInternalServerError, "load_failed", "campaign created but readback failed", nil)
		return
	}

	if supDiscountUsed > 0 {
		units := chain.SUPToUnits(supDiscountUsed)
		code, burnErr := a.chain.BurnSUPForService(r.Context(), payerWallet, units, "security_audit:"+campaignID)
		if burnErr != nil || code != "" {
			a.rollbackNewCampaignEscrow(r.Context(), campaignID)
			msg := "sup burn failed"
			if burnErr != nil {
				msg = burnErr.Error()
			}
			writeAPIError(w, http.StatusBadRequest, "sup_burn_failed", msg, map[string]any{"code": code})
			return
		}
	}

	resp["campaign"] = c
	resp["escrow"] = escrow
	if pkg, err := fuzzingcli.B2BPackageFor(digPkgFinal); err == nil {
		q := fuzzingcli.QuoteDigBilling(pkg)
		q.BudgetHMC = budgetHMC
		q.BudgetRuns = budgetRuns
		q.BudgetSeconds = budgetSeconds
		q.EscrowSplit = escrowSplit
		resp["dig_billing"] = q
	}
	resp["product_mode"] = fuzzingcli.DigProductMode(digPkgFinal)
	resp["escrow_split"] = escrowSplit
	if supDiscountUsed > 0 {
		resp["sup_discount_used"] = supDiscountUsed
		resp["budget_hmc_cash"] = cashBudgetHMC
		resp["budget_hmc_nominal"] = budgetHMC
		resp["budget_hmc_escrow"] = escrowBudgetHMC
	}
	resp["customer_report_token"] = reportToken
	resp["customer_report_header"] = "X-Hackme-Report-Token"
	resp["fuzz_engine"] = fuzzEngineMetaFromConfig(cfgMap)
	resp["depth_tier"] = string(depthTier)
	mergeDeliverableURLs(resp, campaignID)
	if publicProofEnabled(cfgMap) {
		resp["public_proof"] = true
		resp["proof_url"] = "/proof/" + campaignID
		resp["badge_url"] = "/proof/" + campaignID + "/badge.svg"
	}

	if poolDistributedCampaign(cfgMap) {
		resp["pool_distributed"] = true
		fc := fuzzAutoCampaign{ID: campaignID, OwnerRef: ownerRef, BudgetRuns: budgetRuns, BudgetSeconds: budgetSeconds, ConfigJSON: cfg}
		a.applyPoolSyncResponse(resp, r.Context(), fc)
		if fail, _ := resp["pool_sync"].(string); fail == "fail" {
			a.rollbackNewCampaignEscrow(r.Context(), campaignID)
			writeAPIError(w, http.StatusBadGateway, "pool_sync_failed",
				toString(resp["pool_sync_warning"]), map[string]any{"rolled_back": true})
			return
		}
	}

	writeJSON(w, resp)
}

// readSecurityAuditWasmPath loads a .wasm under the repo (tasks/artifacts/… only).
func readSecurityAuditWasmPath(repoRoot, rel string) ([]byte, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return nil, errors.New("wasm_path empty")
	}
	if filepath.IsAbs(rel) {
		return nil, errors.New("wasm_path must be relative to the repo root")
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	if strings.HasPrefix(rel, "../") || rel == ".." {
		return nil, errors.New("wasm_path escapes repo root")
	}
	if !strings.HasPrefix(rel, "tasks/artifacts/") {
		return nil, errors.New("wasm_path must be under tasks/artifacts/")
	}
	if !strings.HasSuffix(strings.ToLower(rel), ".wasm") {
		return nil, errors.New("wasm_path must end with .wasm")
	}
	root := strings.TrimSpace(repoRoot)
	if root == "" {
		root = fuzzingcli.DiscoverRepoRoot()
	}
	absRoot, ok := pathsafe.Allow(root)
	if !ok || !pathsafe.AbsRE.MatchString(absRoot) {
		return nil, errors.New("invalid repo root for wasm_path")
	}
	full, ok := pathsafe.WithinRoot(absRoot, filepath.Join(filepath.FromSlash(absRoot), filepath.FromSlash(rel)))
	if !ok || !pathsafe.AbsRE.MatchString(full) {
		return nil, errors.New("wasm_path rejected")
	}
	// Reject symlink escape: path string may sit under tasks/artifacts/ while the
	// target resolves outside the repo (arbitrary file read as wasm bytes).
	resolved, rerr := filepath.EvalSymlinks(full)
	if rerr != nil {
		return nil, fmt.Errorf("read wasm_path: %w", rerr)
	}
	resolvedSlash := filepath.ToSlash(resolved)
	if !pathsafe.AbsRE.MatchString(resolvedSlash) {
		return nil, errors.New("wasm_path rejected")
	}
	if _, ok := pathsafe.WithinRoot(absRoot, resolvedSlash); !ok {
		return nil, errors.New("wasm_path escapes repo root via symlink")
	}
	raw, err := os.ReadFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("read wasm_path: %w", err)
	}
	if len(raw) == 0 {
		return nil, errors.New("wasm_path file is empty")
	}
	if len(raw) > sandbox.MaxCheckWasmBytes() {
		return nil, fmt.Errorf("wasm_path too large (%d bytes)", len(raw))
	}
	return raw, nil
}

func openFuzzEscrowRetry(ctx context.Context, ch *chain.Service, campaignID string, budgetHMC float64, budgetRuns int) (*chain.FuzzEscrowRow, error) {
	return openFuzzEscrowRetrySplit(ctx, ch, campaignID, budgetHMC, budgetRuns, fuzzescrow.EscrowSplit2080)
}

func openFuzzEscrowRetrySplit(ctx context.Context, ch *chain.Service, campaignID string, budgetHMC float64, budgetRuns int, escrowSplit string) (*chain.FuzzEscrowRow, error) {
	var escrow *chain.FuzzEscrowRow
	var err error
	for attempt := 0; attempt < 6; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt*50) * time.Millisecond)
		}
		escrow, err = ch.OpenFuzzEscrowSplit(ctx, campaignID, budgetHMC, budgetRuns, escrowSplit)
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "database is locked") {
			return escrow, err
		}
	}
	return escrow, err
}
