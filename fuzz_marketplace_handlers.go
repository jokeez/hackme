package main

import (
	"context"
	"net/http"
	"strings"
	"time"

	"hackme/internal/poolfuzz"
)

const fuzzMarketplaceCacheTTL = 12 * time.Second

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

func (a *app) fuzzMarketplaceStore(items []map[string]any) {
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
	if !force {
		if cached, ok := a.fuzzMarketplaceCached(); ok {
			writeJSON(w, map[string]any{"ok": true, "campaigns": cached, "cached": true})
			return
		}
	}

	svc := &poolfuzz.Service{DB: a.db}
	items, listErr := svc.ListPublicCampaigns(r.Context(), 50)
	localWarn := ""
	if listErr != nil {
		// Local SQLite can be busy/corrupt on desktop; still serve the public pool list.
		items = nil
		localWarn = listErr.Error()
	}

	mergeCtx, mergeCancel := context.WithTimeout(context.Background(), 15*time.Second)
	items = a.mergeCoordinatorPoolMarketplace(mergeCtx, items)
	mergeCancel()

	a.fuzzMarketplaceStore(items)
	resp := map[string]any{"ok": true, "campaigns": items}
	if localWarn != "" {
		resp["local_warning"] = localWarn
		resp["source"] = "coordinator"
	}
	writeJSON(w, resp)
}
