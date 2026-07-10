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

	ctx, cancel := context.WithTimeout(r.Context(), 18*time.Second)
	defer cancel()

	type listResult struct {
		items []map[string]any
		err   error
	}
	type remoteResult struct {
		remote map[string]coordinatorPoolCampaign
		err    error
	}
	listCh := make(chan listResult, 1)
	remoteCh := make(chan remoteResult, 1)

	go func() {
		svc := &poolfuzz.Service{DB: a.db}
		items, err := svc.ListPublicCampaigns(context.Background(), 50)
		listCh <- listResult{items, err}
	}()
	go func() {
		mergeCtx, mergeCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer mergeCancel()
		remote, err := a.fetchCoordinatorPoolCampaigns(mergeCtx)
		remoteCh <- remoteResult{remote, err}
	}()

	var items []map[string]any
	var listErr error
	select {
	case <-ctx.Done():
		if cached, ok := a.fuzzMarketplaceCached(); ok {
			writeJSON(w, map[string]any{"ok": true, "campaigns": cached, "cached": true, "warning": "stale_cache"})
			return
		}
		writeJSON(w, map[string]any{"ok": true, "campaigns": []any{}, "warning": "marketplace_timeout"})
		return
	case res := <-listCh:
		items, listErr = res.items, res.err
	}
	if listErr != nil {
		writeAPIError(w, http.StatusInternalServerError, "marketplace_failed", listErr.Error(), nil)
		return
	}

	var remote map[string]coordinatorPoolCampaign
	select {
	case <-ctx.Done():
		remote = map[string]coordinatorPoolCampaign{}
	case res := <-remoteCh:
		remote = res.remote
		if remote == nil {
			remote = map[string]coordinatorPoolCampaign{}
		}
	default:
		select {
		case res := <-remoteCh:
			remote = res.remote
			if remote == nil {
				remote = map[string]coordinatorPoolCampaign{}
			}
		case <-time.After(6 * time.Second):
			remote = map[string]coordinatorPoolCampaign{}
		}
	}

	mergeCtx, mergeCancel := context.WithTimeout(context.Background(), 6*time.Second)
	items = a.mergeCoordinatorPoolMarketplaceWithRemote(mergeCtx, items, remote)
	mergeCancel()

	a.fuzzMarketplaceStore(items)
	writeJSON(w, map[string]any{"ok": true, "campaigns": items})
}
