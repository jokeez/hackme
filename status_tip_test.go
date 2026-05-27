package main

import (
	"context"
	"testing"
)

func TestLocalLedgerTipCacheRoundTrip(t *testing.T) {
	a := &app{}
	a.cacheLocalLedgerTip(true, 42, "abc123")
	has, h, tip, ok := a.readLocalLedgerTipCache(localTipCacheMaxAgeSec)
	if !ok || !has || h != 42 || tip != "abc123" {
		t.Fatalf("cache read got has=%v h=%d tip=%q ok=%v", has, h, tip, ok)
	}
}

func TestChainTipForStatusUsesCacheWhenDBEmpty(t *testing.T) {
	a := &app{}
	a.cacheLocalLedgerTip(true, 99, "cached-tip-hash")
	has, h, tip, stale := a.chainTipForStatus(context.Background())
	if !stale || !has || h != 99 || tip != "cached-tip-hash" {
		t.Fatalf("expected stale cache tip, got has=%v h=%d tip=%q stale=%v", has, h, tip, stale)
	}
}
