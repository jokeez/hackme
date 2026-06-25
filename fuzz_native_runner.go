package main

import (
	"context"
	"log"
	"os"

	"hackme/internal/fuzzengine"
	"hackme/internal/fuzznative"
)

func (a *app) fuzzNativeBridgeTick(ctx context.Context) error {
	if a == nil || a.db == nil {
		return nil
	}
	repoRoot := os.Getenv("HACKME_REPO_ROOT")
	pins, err := fuzznative.LoadPins(repoRoot)
	if err != nil {
		pins = &fuzznative.PinManifest{Repos: map[string]fuzznative.PinRepo{}}
	}
	n, err := fuzznative.ProcessPending(ctx, a.db, pins, 8)
	if err != nil {
		return err
	}
	if n > 0 {
		log.Printf("fuzz native bridge: processed %d repro jobs", n)
	}
	return nil
}

func (a *app) enqueueNativeReproForFinding(ctx context.Context, campaignID, findingID, inputSHA string, input []byte, cfg map[string]any, now int64) {
	if a == nil || a.db == nil || !fuzzengine.NativeReproEnabled(cfg) {
		return
	}
	guard := toString(cfg["guard_name"])
	if guard == "" {
		guard = toString(cfg["upstream_guard"])
	}
	target := fuzzengine.UpstreamTarget(cfg)
	if len(input) == 0 {
		input = make([]byte, 8)
	}
	_ = fuzznative.QueueJob(ctx, a.db, findingID, campaignID, inputSHA, input, target, guard, now)
}
