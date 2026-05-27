package main

import (
	"context"
	"strings"
	"time"
)

const localTipCacheMaxAgeSec = 180

func (a *app) cacheLocalLedgerTip(hasGenesis bool, height uint64, tipHash string) {
	if a == nil {
		return
	}
	tipHash = strings.TrimSpace(tipHash)
	if tipHash == "" {
		return
	}
	a.localTipMu.Lock()
	a.localTipHasGenesis = hasGenesis
	a.localTipHeight = height
	a.localTipHash = tipHash
	a.localTipCachedUnix = time.Now().Unix()
	a.localTipMu.Unlock()
}

func (a *app) readLocalLedgerTipCache(maxAgeSec int64) (bool, uint64, string, bool) {
	if a == nil {
		return false, 0, "", false
	}
	a.localTipMu.RLock()
	defer a.localTipMu.RUnlock()
	if strings.TrimSpace(a.localTipHash) == "" || a.localTipCachedUnix == 0 {
		return false, 0, "", false
	}
	if maxAgeSec > 0 && time.Now().Unix()-a.localTipCachedUnix > maxAgeSec {
		return false, 0, "", false
	}
	return a.localTipHasGenesis, a.localTipHeight, a.localTipHash, true
}

// chainTipForStatus returns local ledger tip for dashboards. Uses TipFast with a short timeout,
// then last-good cache so /api/status never flashes tip_height=0 under SQLITE_BUSY.
func (a *app) chainTipForStatus(ctx context.Context) (hasGenesis bool, height uint64, hash string, stale bool) {
	if a == nil || a.chain == nil {
		if hasC, hC, tC, ok := a.readLocalLedgerTipCache(localTipCacheMaxAgeSec); ok {
			return hasC, hC, tC, true
		}
		return false, 0, "", false
	}
	tipCtx, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
	defer cancel()
	h, tip, err := a.chain.TipFast(tipCtx)
	if err == nil && strings.TrimSpace(tip) != "" {
		hasGenesis = true
		a.cacheLocalLedgerTip(true, h, tip)
		return hasGenesis, h, tip, false
	}
	hgCtx, hgCancel := context.WithTimeout(ctx, 150*time.Millisecond)
	hasGenesis, _ = a.chain.HasGenesis(hgCtx)
	hgCancel()
	if hasC, hC, tC, ok := a.readLocalLedgerTipCache(localTipCacheMaxAgeSec); ok {
		return hasC, hC, tC, true
	}
	return hasGenesis, 0, "", false
}

func (a *app) warmLocalLedgerTipCache(ctx context.Context) {
	if a == nil || a.chain == nil {
		return
	}
	h, tip, err := a.chain.TipFast(ctx)
	if err != nil || strings.TrimSpace(tip) == "" {
		return
	}
	has, _ := a.chain.HasGenesis(ctx)
	a.cacheLocalLedgerTip(has, h, tip)
}
