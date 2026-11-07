package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"hackme/internal/fuzzescrow"
	"hackme/internal/hunt"
)

func (a *app) handleHuntAPI(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/hunt")
	path = strings.Trim(path, "/")
	switch {
	case path == "packages" && r.Method == http.MethodGet:
		a.handleHuntPackages(w, r)
	case path == "targets" && r.Method == http.MethodGet:
		a.handleHuntTargets(w, r)
	case path == "inventory" && r.Method == http.MethodPost:
		a.handleHuntInventory(w, r)
	case path == "repo/pin" && r.Method == http.MethodPost:
		a.handleHuntRepoPin(w, r)
	case path == "harness/build" && r.Method == http.MethodPost:
		a.handleHuntHarnessBuild(w, r)
	case path == "harness/publish" && r.Method == http.MethodPost:
		a.handleHuntHarnessPublish(w, r)
	case strings.HasPrefix(path, "harness/") && r.Method == http.MethodGet:
		a.handleHuntHarnessGet(w, r, strings.TrimPrefix(path, "harness/"))
	case path == "template/preview" && r.Method == http.MethodPost:
		a.handleHuntTemplatePreview(w, r)
	case path == "pack-suggest" && r.Method == http.MethodPost:
		a.handleHuntPackSuggest(w, r)
	case path == "campaigns" && r.Method == http.MethodPost:
		a.handleHuntCampaignCreate(w, r)
	case strings.HasSuffix(path, "/run-local") && r.Method == http.MethodPost:
		id := strings.TrimSuffix(path, "/run-local")
		a.handleHuntCampaignRunLocal(w, r, id)
	default:
		writeAPIError(w, http.StatusNotFound, "not_found", "unknown hunt route", nil)
	}
}

func (a *app) handleHuntPackages(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"ok":       true,
		"packages": hunt.Packages(),
		"note":     "Hunt uses 50/50 escrow — see docs/HUNT_ECONOMICS.md",
	})
}

func (a *app) handleHuntTargets(w http.ResponseWriter, r *http.Request) {
	root := a.repoRoot()
	targets, err := hunt.ListCatalogTargets(root, 0)
	if err != nil {
		// Soft-fail: Orders UI must still load; empty catalog is recoverable.
		writeJSON(w, map[string]any{
			"ok":      true,
			"targets": []any{},
			"count":   0,
			"warning": "targets_unavailable",
			"detail":  err.Error(),
		})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "targets": targets, "count": len(targets)})
}

func (a *app) handleHuntInventory(w http.ResponseWriter, r *http.Request) {
	if !requireAdminAuthStrict(w, r) {
		return
	}
	logAdminAction(r, "hunt_inventory")
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	defer r.Body.Close()
	var req hunt.InventoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid json", nil)
		return
	}
	res, err := hunt.ScanInventory(a.repoRoot(), req.Path, req.MaxFiles, req.MaxDepth)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "inventory_failed", err.Error(), nil)
		return
	}
	suggestions := hunt.SuggestPacksForInventory(res)
	writeJSON(w, map[string]any{"ok": true, "inventory": res, "pack_suggestions": suggestions})
}

func (a *app) handleHuntPackSuggest(w http.ResponseWriter, r *http.Request) {
	if !requireAdminAuthStrict(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	defer r.Body.Close()
	var req struct {
		Path          string `json:"path"`
		SourceRel     string `json:"source_rel"`
		ContentSample string `json:"content_sample"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid json", nil)
		return
	}
	sourceRel := strings.TrimSpace(req.SourceRel)
	if sourceRel == "" {
		sourceRel = strings.TrimSpace(req.Path)
	}
	if sourceRel == "" {
		writeAPIError(w, http.StatusBadRequest, "path_required", "source_rel or path required", nil)
		return
	}
	sample := strings.TrimSpace(req.ContentSample)
	if sample == "" && strings.TrimSpace(req.Path) != "" {
		if b, err := hunt.SafeReadFileUnder(a.repoRoot(), req.Path); err == nil {
			if len(b) > 4096 {
				b = b[:4096]
			}
			sample = string(b)
		}
	}
	suggestions := hunt.SuggestPacksForPath(sourceRel, sample)
	writeJSON(w, map[string]any{"ok": true, "pack_suggestions": suggestions, "source_rel": sourceRel})
}

func (a *app) handleHuntCampaignCreate(w http.ResponseWriter, r *http.Request) {
	if !requireFuzzCampaignCreateAuth(w, r) {
		return
	}
	if hasValidAdminAuth(r) {
		logAdminAction(r, "hunt_campaign_create")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	defer r.Body.Close()
	var req hunt.CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid json", nil)
		return
	}
	root := a.repoRoot()
	cfgMap, title, err := hunt.CampaignConfig(r.Context(), root, req)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_hunt_config", err.Error(), nil)
		return
	}
	budgetHMC, shards, _, err := hunt.BudgetForCreate(req)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_hunt_budget", err.Error(), nil)
		return
	}
	// Fail closed before escrow: publish-token coordinator rejects budget_runs above cap
	// (field report: hunt_heavy 12000 / default cap 2048 → silent running + never on pool).
	if poolDistributedCampaign(cfgMap) && rejectIfExceedsOrdersPublishCaps(w, shards, 86400) {
		return
	}
	id := cleanFuzzID(req.ID, "hunt")
	ownerRef := strings.TrimSpace(req.OwnerRef)
	if ownerRef == "" && strings.HasPrefix(id, "hunt-customer-") {
		ownerRef = "customer:" + strings.TrimPrefix(id, "hunt-customer-")
	}
	now := time.Now().Unix()
	reportToken := newReportToken()
	reportTokenHashHex := reportTokenHash(reportToken)
	status := strings.TrimSpace(strings.ToLower(req.Status))
	if status == "" {
		status = "planned"
	}
	if !allowedCampaignStatus(status) {
		writeAPIError(w, http.StatusBadRequest, "invalid_status", "invalid status", nil)
		return
	}
	startedAt := int64(0)
	if status == "running" {
		startedAt = now
	}
	budgetSeconds := 86400
	if !poolDistributedCampaign(cfgMap) {
		budgetSeconds = hunt.LocalRunTimeLimitFromConfig(cfgMap, hunt.PackageKeyFromConfig(cfgMap))
		if budgetSeconds < 3600 {
			budgetSeconds = 3600
		}
	}
	cfg := marshalMapJSON(cfgMap)
	initialSummary := map[string]any{"hunt_depth": hunt.DepthSummaryForConfig(cfgMap)}
	err = execContextRetryBusy(r.Context(), a.db,
		`INSERT INTO fuzz_campaigns
		 (id, campaign_type, status, title, description, owner_ref, task_id, target_ref, budget_runs, budget_seconds, config_json, summary_json, report_token_hash, report_token_issued_at, created_at, started_at, completed_at)
		 VALUES (?, 'hunt', ?, ?, '', ?, '', ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		id, status, title, ownerRef, cfgMap["upstream_target_id"], shards, budgetSeconds, cfg, marshalMapJSON(initialSummary), reportTokenHashHex, now, now, startedAt)
	if err != nil {
		writeAPIError(w, http.StatusConflict, "create_failed", "campaign create failed", map[string]any{"detail": err.Error()})
		return
	}
	if poolDistributedCampaign(cfgMap) && !poolSyncCoordinatorConfigured() {
		_, _ = a.db.ExecContext(r.Context(), `DELETE FROM fuzz_campaigns WHERE id=?`, id)
		writeAPIError(w, http.StatusBadRequest, "pool_coordinator_required",
			"pool_distributed requires HACKME_POOL_COORDINATOR_URL", nil)
		return
	}
	if _, fundErr := a.ensureOrdersSpendableForBudget(r.Context(), budgetHMC); fundErr != nil {
		_, _ = a.db.ExecContext(r.Context(), `DELETE FROM fuzz_campaigns WHERE id=?`, id)
		writeOrdersAutoFundOrEscrowFailed(w, fundErr)
		return
	}
	escrow, err := a.chain.OpenFuzzEscrowWithFee(r.Context(), id, budgetHMC, shards, fuzzescrow.EscrowSplit5050, toString(cfgMap["fee_asset"]))
	if err != nil {
		_, _ = a.db.ExecContext(r.Context(), `DELETE FROM fuzz_campaigns WHERE id=?`, id)
		writeFuzzEscrowFailed(w, err)
		return
	}
	cfgMap["budget_hmc"] = budgetHMC
	cfgMap["escrow_split"] = fuzzescrow.EscrowSplit5050
	cfgMap["budget_shards"] = shards
	cfg = marshalMapJSON(cfgMap)
	_, _ = a.db.ExecContext(r.Context(), `UPDATE fuzz_campaigns SET config_json=? WHERE id=?`, cfg, id)
	c, err := a.getFuzzCampaign(r.Context(), id)
	if err != nil {
		a.rollbackNewCampaignEscrow(r.Context(), id)
		writeAPIError(w, http.StatusInternalServerError, "load_failed", "campaign created but readback failed", nil)
		return
	}
	resp := map[string]any{
		"ok":                     true,
		"campaign":               c,
		"product":                "hunt",
		"escrow_split":           fuzzescrow.EscrowSplit5050,
		"customer_report_token":  reportToken,
		"customer_report_header": "X-Hackme-Report-Token",
		"escrow":                 escrow,
		"prepay_disclaimer":      "No CVE guarantee · CLEAN = budget statement · pool shards verify ASAN on miners",
	}
	// Surface honest edge-pay posture (inventory without afl-cc → floor_only).
	if v, ok := cfgMap["showmap_instrumented"]; ok {
		resp["showmap_instrumented"] = v
	}
	if v := strings.TrimSpace(toString(cfgMap["hunt_edge_pay"])); v != "" {
		resp["hunt_edge_pay"] = v
	}
	if note := strings.TrimSpace(toString(cfgMap["showmap_instrument_note"])); note != "" {
		resp["showmap_instrument_note"] = note
	}
	mergeDeliverableURLs(resp, id)
	if poolDistributedCampaign(cfgMap) {
		if pubErr := a.publishHuntHarnessForConfig(r.Context(), cfgMap); pubErr != nil {
			a.rollbackNewCampaignEscrow(r.Context(), id)
			writeAPIError(w, http.StatusBadRequest, "harness_publish_failed", pubErr.Error(), nil)
			return
		}
		cfg = marshalMapJSON(cfgMap)
		_, _ = a.db.ExecContext(r.Context(), `UPDATE fuzz_campaigns SET config_json=? WHERE id=?`, cfg, id)
		deferred := false
		if v, ok := cfgMap["harness_build_deferred"].(bool); ok && v {
			deferred = true
		}
		if !deferred {
			if syncErr := a.syncHuntHarnessToCoordinator(r.Context(), cfgMap); syncErr != nil {
				a.rollbackNewCampaignEscrow(r.Context(), id)
				writeAPIError(w, http.StatusBadGateway, "harness_pool_sync_failed", syncErr.Error(), map[string]any{
					"harness_hash": cfgMap["harness_hash"],
				})
				return
			}
			a.syncCorpusNamespaceToCoordinator(r.Context(), cfgMap)
		} else {
			resp["harness_build_deferred"] = true
			if note := strings.TrimSpace(toString(cfgMap["harness_publish_note"])); note != "" {
				resp["harness_publish_note"] = note
			}
		}
		if attached, _ := cfgMap["harness_pool_attached"].(bool); attached {
			resp["harness_pool_attached"] = true
			if note := strings.TrimSpace(toString(cfgMap["harness_publish_note"])); note != "" {
				resp["harness_publish_note"] = note
			}
		}
		resp["pool_distributed"] = true
		resp["harness_fetch_path"] = cfgMap["harness_fetch_path"]
		resp["harness_hash"] = cfgMap["harness_hash"]
		fc := fuzzAutoCampaign{ID: id, BudgetRuns: shards, BudgetSeconds: 86400, ConfigJSON: cfg, OwnerRef: ownerRef}
		a.applyPoolSyncResponse(resp, r.Context(), fc)
		if fail, _ := resp["pool_sync"].(string); fail == "fail" {
			a.rollbackNewCampaignEscrow(r.Context(), id)
			writeAPIError(w, http.StatusBadGateway, "pool_sync_failed",
				toString(resp["pool_sync_warning"]), map[string]any{"rolled_back": true})
			return
		}
	}
	writeJSON(w, resp)
}

func (a *app) handleHuntCampaignRunLocal(w http.ResponseWriter, r *http.Request, campaignID string) {
	if !requireAdminAuthStrict(w, r) {
		return
	}
	campaignID = cleanFuzzID(campaignID, "hunt")
	c, err := a.getFuzzCampaign(r.Context(), campaignID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "campaign not found", nil)
		return
	}
	if strings.ToLower(strings.TrimSpace(c.CampaignType)) != "hunt" {
		writeAPIError(w, http.StatusBadRequest, "not_hunt", "campaign is not hunt type", nil)
		return
	}
	cfg := c.Config
	if cfg == nil {
		cfg = map[string]any{}
	}
	pkgKey := hunt.PackageKeyFromConfig(cfg)
	hunt.ApplyPackageDepthDefaults(cfg, pkgKey, false)
	targetID := strings.TrimSpace(toString(cfg["upstream_target_id"]))
	if targetID == "" {
		writeAPIError(w, http.StatusBadRequest, "no_target", "upstream_target_id missing", nil)
		return
	}
	tickIter := hunt.LocalRunBudgetFromConfig(cfg, pkgKey)
	if v := strings.TrimSpace(r.URL.Query().Get("tick_iterations")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			tickIter = n
		}
	} else if n := intFromAny(cfg["hunt_local_tick_iterations"]); n > 0 {
		tickIter = n
	}
	if tickIter > 10_000 {
		tickIter = 10_000
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	rep, err := hunt.LocalRunWithConfig(ctx, hunt.LocalRunOptions{
		RepoRoot:         a.repoRoot(),
		TargetID:         targetID,
		BudgetIterations: tickIter,
		TimeLimitSec:     180,
		Config:           cfg,
	})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "hunt_run_failed", err.Error(), nil)
		return
	}
	_, _ = a.db.ExecContext(r.Context(),
		`UPDATE fuzz_campaigns SET status='running', started_at=CASE WHEN started_at=0 THEN ? ELSE started_at END, summary_json=? WHERE id=?`,
		time.Now().Unix(), marshalMapJSON(map[string]any{
			"hunt_local_run": true,
			"verdict":        rep.Verdict,
			"iterations":     rep.Iterations,
			"crashes":        len(rep.Crashes),
		}), campaignID)
	writeJSON(w, map[string]any{"ok": true, "report": rep})
}

func (a *app) handleHuntRepoPin(w http.ResponseWriter, r *http.Request) {
	if !requireAdminAuthStrict(w, r) {
		return
	}
	logAdminAction(r, "hunt_repo_pin")
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	defer r.Body.Close()
	var req hunt.RepoPinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid json", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	pin, err := hunt.PinRepo(ctx, a.repoRoot(), req)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "pin_failed", err.Error(), nil)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "pin": pin})
}

func (a *app) handleHuntHarnessBuild(w http.ResponseWriter, r *http.Request) {
	if !requireAdminAuthStrict(w, r) {
		return
	}
	logAdminAction(r, "hunt_harness_build")
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	defer r.Body.Close()
	var req hunt.HarnessBuildAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid json", nil)
		return
	}
	root := a.repoRoot()
	var pin *hunt.RepoPinResult
	if req.Repo != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		defer cancel()
		p, err := hunt.PinRepo(ctx, root, *req.Repo)
		cancel()
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "pin_failed", err.Error(), nil)
			return
		}
		pin = p
	} else if strings.TrimSpace(req.SourceRel) != "" {
		// allow build against already-pinned path in source_rel parent — require repo.path
		writeAPIError(w, http.StatusBadRequest, "pin_required", "repo pin required", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	build, err := hunt.BuildInventoryHarness(ctx, root, hunt.HarnessBuildRequest{
		Pin:            pin,
		SourceRel:      req.SourceRel,
		TemplateAccept: req.TemplateAccept,
	})
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "build_failed", err.Error(), nil)
		return
	}
	if err := hunt.PublishHarnessFile(r.Context(), a.db, build.HarnessHash, build.BinaryPath, build.SourceRel); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "publish_failed", err.Error(), nil)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "build": build, "pin": pin, "harness_published": true, "harness_fetch_path": hunt.HarnessFetchURL(build.HarnessHash)})
}

func (a *app) handleHuntTemplatePreview(w http.ResponseWriter, r *http.Request) {
	if !requireAdminAuthStrict(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	defer r.Body.Close()
	var req struct {
		PinPath   string               `json:"pin_path"`
		SourceRel string               `json:"source_rel"`
		Repo      *hunt.RepoPinRequest `json:"repo,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid json", nil)
		return
	}
	pinPath := strings.TrimSpace(req.PinPath)
	if req.Repo != nil && pinPath == "" {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		defer cancel()
		pin, err := hunt.PinRepo(ctx, a.repoRoot(), *req.Repo)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "pin_failed", err.Error(), nil)
			return
		}
		pinPath = pin.Path
	}
	if pinPath != "" {
		safePin, err := hunt.MustUnderRoot(a.repoRoot(), pinPath)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "pin_path_forbidden", "pin_path must be under repo root", nil)
			return
		}
		pinPath = safePin
	}
	prev, err := hunt.PreviewTemplate(a.repoRoot(), pinPath, req.SourceRel)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "preview_failed", err.Error(), nil)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "preview": prev, "pin_path": pinPath})
}

func (a *app) repoRoot() string {
	if r := strings.TrimSpace(os.Getenv("HACKME_REPO_ROOT")); r != "" {
		return r
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	dir := wd
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return wd
}

func (a *app) handleHuntHarnessPublish(w http.ResponseWriter, r *http.Request) {
	if !requireAdminAuthStrict(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 36<<20)
	defer r.Body.Close()
	var req struct {
		HarnessHash string `json:"harness_hash"`
		BinaryPath  string `json:"binary_path"`
		SourceRel   string `json:"source_rel"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid json", nil)
		return
	}
	hash := strings.TrimSpace(req.HarnessHash)
	path := strings.TrimSpace(req.BinaryPath)
	if hash == "" || path == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "harness_hash and binary_path required", nil)
		return
	}
	if err := hunt.PublishHarnessFile(r.Context(), a.db, hash, path, req.SourceRel); err != nil {
		writeAPIError(w, http.StatusBadRequest, "publish_failed", err.Error(), nil)
		return
	}
	if syncErr := a.syncHuntHarnessToCoordinator(r.Context(), map[string]any{
		"harness_hash": hash, "hunt_source_rel": req.SourceRel,
	}); syncErr != nil {
		writeAPIError(w, http.StatusBadGateway, "harness_pool_sync_failed", syncErr.Error(), map[string]any{
			"harness_hash": hash,
		})
		return
	}
	writeJSON(w, map[string]any{
		"ok": true, "harness_hash": hash,
		"harness_fetch_path": hunt.HarnessFetchURL(hash),
	})
}

func (a *app) handleHuntHarnessGet(w http.ResponseWriter, r *http.Request, hash string) {
	if !requireAdminAuthStrict(w, r) {
		return
	}
	hash = strings.TrimSpace(hash)
	data, err := hunt.GetHarnessArtifact(r.Context(), a.db, hash)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", err.Error(), nil)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = w.Write(data)
}
