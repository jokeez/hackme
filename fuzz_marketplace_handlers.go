package main

import (
	"context"
	"net/http"
	"strings"
	"time"

	"hackme/internal/poolfuzz"
)

const fuzzMarketplaceCacheTTL = 45 * time.Second
const fuzzMarketplaceStaleTTL = 10 * time.Minute

func (a *app) fuzzMarketplaceCached() ([]map[string]any, bool) {
	a.fuzzMarketMu.RLock()
	defer a.fuzzMarketMu.RUnlock()
	if len(a.fuzzMarketCache) == 0 || time.Since(a.fuzzMarketAt) > fuzzMarketplaceCacheTTL {
		return nil, false
	}
	out := make([]map[string]any, len(a.fuzzMarketCache))
	copy(out, a.fuzzMarketCache)
	return out, true
}

func (a *app) fuzzMarketplaceStale() ([]map[string]any, bool) {
	a.fuzzMarketMu.RLock()
	defer a.fuzzMarketMu.RUnlock()
	if len(a.fuzzMarketCache) == 0 || time.Since(a.fuzzMarketAt) > fuzzMarketplaceStaleTTL {
		return nil, false
	}
	out := make([]map[string]any, len(a.fuzzMarketCache))
	copy(out, a.fuzzMarketCache)
	return out, true
}

func (a *app) fuzzMarketplaceStore(items []map[string]any) {
	// Never overwrite a good cache with an empty timeout/failure result.
	if len(items) == 0 {
		return
	}
	a.fuzzMarketMu.Lock()
	defer a.fuzzMarketMu.Unlock()
	a.fuzzMarketCache = items
	a.fuzzMarketAt = time.Now()
}

func (a *app) fuzzMarketplaceInvalidate() {
	a.fuzzMarketMu.Lock()
	defer a.fuzzMarketMu.Unlock()
	a.fuzzMarketCache = nil
	a.fuzzMarketAt = time.Time{}
}

func (a *app) handleFuzzMarketplace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	force := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("refresh"))) == "1"
	limit := 30
	if force {
		limit = 8 // refresh bypasses cache — tighter per-IP cap (fimasw relay amp)
	}
	if !a.allowRate("fuzz_marketplace:"+clientIP(r), limit) {
		http.Error(w, "rate limit", http.StatusTooManyRequests)
		return
	}
	if !force {
		if cached, ok := a.fuzzMarketplaceCached(); ok {
			cap, _ := a.fetchCoordinatorFleetCapacity(r.Context())
			items := cloneMarketplaceCampaigns(cached)
			if cap != nil {
				poolfuzz.AnnotateCampaignFleetETA(items, floatFromAny(cap["est_shards_per_hour"]))
			}
			resp := map[string]any{"ok": true, "campaigns": items, "cached": true}
			if cap != nil {
				resp["fleet_capacity"] = cap
			}
			writeJSON(w, resp)
			return
		}
	}

	svc := &poolfuzz.Service{DB: a.db}
	items, listErr := svc.ListPublicCampaigns(r.Context(), 50)
	localWarn := ""
	if listErr != nil {
		items = nil
		localWarn = listErr.Error()
	}

	mergeCtx, mergeCancel := context.WithTimeout(context.Background(), 12*time.Second)
	items = a.mergeCoordinatorPoolMarketplace(mergeCtx, items)
	cap, _ := a.fetchCoordinatorFleetCapacity(mergeCtx)
	mergeCancel()

	if len(items) == 0 {
		if stale, ok := a.fuzzMarketplaceStale(); ok {
			staleItems := cloneMarketplaceCampaigns(stale)
			if cap != nil {
				poolfuzz.AnnotateCampaignFleetETA(staleItems, floatFromAny(cap["est_shards_per_hour"]))
			}
			resp := map[string]any{"ok": true, "campaigns": staleItems, "cached": true, "stale": true}
			if localWarn != "" {
				resp["local_warning"] = localWarn
			}
			if cap != nil {
				resp["fleet_capacity"] = cap
			}
			writeJSON(w, resp)
			return
		}
	}

	a.fuzzMarketplaceStore(items)
	out := cloneMarketplaceCampaigns(items)
	if cap != nil {
		poolfuzz.AnnotateCampaignFleetETA(out, floatFromAny(cap["est_shards_per_hour"]))
	}
	resp := map[string]any{"ok": true, "campaigns": out}
	if localWarn != "" {
		resp["local_warning"] = localWarn
		resp["source"] = "coordinator"
	}
	if cap != nil {
		resp["fleet_capacity"] = cap
	}
	writeJSON(w, resp)
}
