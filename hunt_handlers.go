package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
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
	case path == "template/preview" && r.Method == http.MethodPost:
		a.handleHuntTemplatePreview(w, r)
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
		writeAPIError(w, http.StatusInternalServerError, "targets_failed", err.Error(), nil)
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
	writeJSON(w, map[string]any{"ok": true, "inventory": res})
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
	id := cleanFuzzID(req.ID, "hunt")
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
	cfg := marshalMapJSON(cfgMap)
	err = execContextRetryBusy(r.Context(), a.db,
		`INSERT INTO fuzz_campaigns
		 (id, campaign_type, status, title, description, owner_ref, task_id, target_ref, budget_runs, budget_seconds, config_json, summary_json, report_token_hash, report_token_issued_at, created_at, started_at, completed_at)
		 VALUES (?, 'hunt', ?, ?, '', '', '', ?, ?, 86400, ?, '{}', ?, ?, ?, ?, 0)`,
		id, status, title, cfgMap["upstream_target_id"], shards, cfg, reportTokenHashHex, now, now, startedAt)
	if err != nil {
		writeAPIError(w, http.StatusConflict, "create_failed", "campaign create failed", map[string]any{"detail": err.Error()})
		return
	}
	escrow, err := a.chain.OpenHuntEscrow(r.Context(), id, budgetHMC, shards)
	if err != nil {
		_, _ = a.db.ExecContext(r.Context(), `DELETE FROM fuzz_campaigns WHERE id=?`, id)
		writeAPIError(w, http.StatusPaymentRequired, "escrow_failed", err.Error(), nil)
		return
	}
	cfgMap["budget_hmc"] = budgetHMC
	cfgMap["escrow_split"] = fuzzescrow.EscrowSplit5050
	cfgMap["budget_shards"] = shards
	cfg = marshalMapJSON(cfgMap)
	_, _ = a.db.ExecContext(r.Context(), `UPDATE fuzz_campaigns SET config_json=? WHERE id=?`, cfg, id)
	c, err := a.getFuzzCampaign(r.Context(), id)
	if err != nil {
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
	if poolDistributedCampaign(cfgMap) {
		resp["pool_distributed"] = true
		fc := fuzzAutoCampaign{ID: id, BudgetRuns: shards, BudgetSeconds: 86400, ConfigJSON: cfg}
		a.applyPoolSyncResponse(resp, r.Context(), fc)
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
	targetID := strings.TrimSpace(toString(cfg["upstream_target_id"]))
	if targetID == "" {
		writeAPIError(w, http.StatusBadRequest, "no_target", "upstream_target_id missing", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	rep, err := hunt.LocalRun(ctx, hunt.LocalRunOptions{
		RepoRoot:         a.repoRoot(),
		TargetID:         targetID,
		BudgetIterations: 1500,
		TimeLimitSec:     90,
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
	writeJSON(w, map[string]any{"ok": true, "build": build, "pin": pin})
}

func (a *app) handleHuntTemplatePreview(w http.ResponseWriter, r *http.Request) {
	if !requireAdminAuthStrict(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	defer r.Body.Close()
	var req struct {
		PinPath   string `json:"pin_path"`
		SourceRel string `json:"source_rel"`
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
