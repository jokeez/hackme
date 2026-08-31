package main

import (
	"context"

	"hackme/internal/fuzzengine"
	"hackme/internal/poolsync"
	"hackme/internal/poolfuzz"
)

func (a *app) syncCorpusNamespaceToCoordinator(ctx context.Context, cfg map[string]any) {
	if a == nil || a.db == nil || cfg == nil || !fuzzengine.CorpusPersistEnabled(cfg) {
		return
	}
	ns := fuzzengine.CorpusPersistNamespace(cfg)
	if ns == "" {
		return
	}
	coord := poolsync.ResolveCoordinatorURL()
	token := poolsync.AdminToken()
	if coord == "" || token == "" {
		return
	}
	svc := &poolfuzz.Service{DB: a.db}
	seeds, err := svc.ListNamespaceCorpus(ctx, ns, fuzzengine.CorpusPersistMax(cfg))
	if err != nil || len(seeds) == 0 {
		return
	}
	_ = poolsync.UploadCorpusNamespace(ctx, coord, token, ns, seeds)
}

func (a *app) publishDigPoolArtifacts(ctx context.Context, cfg map[string]any) {
	a.syncCorpusNamespaceToCoordinator(ctx, cfg)
	a.syncHuntHarnessToCoordinator(ctx, cfg)
}
