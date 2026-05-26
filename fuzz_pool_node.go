package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"hackme/internal/poolfuzz"
)

func poolDistributedCampaign(cfg map[string]any) bool {
	return poolfuzz.PoolDistributed(cfg)
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
