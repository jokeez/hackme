package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"hackme/internal/poolfuzz"
	"hackme/internal/poolsync"
)

// rejectIfExceedsOrdersPublishCaps fails the HTTP create before escrow when the
// Orders publish token on the hub would reject this budget (silent running bug).
func rejectIfExceedsOrdersPublishCaps(w http.ResponseWriter, budgetRuns, budgetSeconds int) bool {
	maxRuns := poolsync.OrdersPublishMaxBudgetRuns()
	if budgetRuns > maxRuns {
		writeAPIError(w, http.StatusBadRequest, "budget_runs_exceeds_publish_cap",
			"pool_distributed budget_runs exceeds Orders publish token cap — raise HACKME_ORDERS_PUBLISH_MAX_BUDGET_RUNS on the hub (and match locally) or use a smaller package",
			map[string]any{"budget_runs": budgetRuns, "publish_max_budget_runs": maxRuns})
		return true
	}
	if budgetSeconds < 0 {
		budgetSeconds = 0
	}
	maxSec := poolsync.OrdersPublishMaxBudgetSeconds()
	if budgetSeconds > maxSec {
		writeAPIError(w, http.StatusBadRequest, "budget_seconds_exceeds_publish_cap",
			"pool_distributed budget_seconds exceeds Orders publish token cap",
			map[string]any{"budget_seconds": budgetSeconds, "publish_max_budget_seconds": maxSec})
		return true
	}
	return false
}

func poolDistributedCampaign(cfg map[string]any) bool {
	return poolfuzz.PoolDistributed(cfg)
}

// attachPoolRegisterFields exposes pool_register_failed (and live sync errors) on campaign GETs.
func (a *app) attachPoolRegisterFields(resp map[string]any, c fuzzCampaign) {
	if resp == nil {
		return
	}
	sum := c.Summary
	if sum == nil {
		sum = map[string]any{}
	}
	status := strings.TrimSpace(toString(sum["pool_register_status"]))
	errMsg := strings.TrimSpace(toString(sum["pool_register_error"]))
	if failed := a.poolSyncFailedIDs(); failed != nil {
		if live, ok := failed[c.ID]; ok && strings.TrimSpace(live) != "" {
			status = "pool_register_failed"
			errMsg = live
		}
	}
	if status == "" && poolDistributedCampaign(c.Config) {
		status = "pending"
	}
	if status != "" {
		resp["pool_register_status"] = status
	}
	if errMsg != "" {
		resp["pool_register_error"] = errMsg
	}
	if status == "pool_register_failed" {
		resp["pool_sync"] = "fail"
		if errMsg != "" {
			resp["pool_sync_warning"] = errMsg
		}
	}
}

func poolSyncCoordinatorConfigured() bool {
	u := strings.TrimRight(strings.TrimSpace(os.Getenv("HACKME_POOL_COORDINATOR_URL")), "/")
	if u == "" {
		u = strings.TrimRight(strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_URL")), "/")
	}
	return u != ""
}

// applyPoolSyncResponse enqueues or runs coordinator registration and sets JSON fields.
func (a *app) applyPoolSyncResponse(resp map[string]any, ctx context.Context, c fuzzAutoCampaign) {
	if !poolDistributedCampaign(parseMapJSON(c.ConfigJSON)) {
		return
	}
	if !poolSyncCoordinatorConfigured() {
		resp["pool_sync"] = "fail"
		resp["pool_sync_warning"] = "pool_distributed: set HACKME_POOL_COORDINATOR_URL on the node"
		return
	}
	mode, warn := a.schedulePoolFuzzSync(ctx, c)
	resp["pool_sync"] = mode
	if warn != "" {
		resp["pool_sync_warning"] = warn
	}
}

func (a *app) syncPoolFuzzCampaign(ctx context.Context, c fuzzAutoCampaign) error {
	if !poolDistributedCampaign(parseMapJSON(c.ConfigJSON)) {
		return nil
	}
	if !poolSyncCoordinatorConfigured() {
		return fmt.Errorf("pool_distributed: set HACKME_POOL_COORDINATOR_URL on the node")
	}
	mode, warn := a.schedulePoolFuzzSync(ctx, c)
	if warn != "" && mode != "queued" {
		return fmt.Errorf("%s", warn)
	}
	if mode == "fail" {
		return fmt.Errorf("pool sync failed (see /api/status pool_sync)")
	}
	return nil
}
