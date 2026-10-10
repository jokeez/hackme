package workerfuzzloop

import (
	"context"
	"os"
	"strings"
)

// ResearchSlotConfig is the opt-in worker-local libFuzzer persist window (Stage D).
// Default OFF. Does not enable HACKME_POOL_SEED_FROM_RESEARCH (that stays a separate
// Dig seed-feed gate, also default OFF).
type ResearchSlotConfig struct {
	Enabled       bool
	WindowSec     int // short local LF persist window
	MaxDeltaFiles int
}

// ResearchSlotFromEnv reads HACKME_WORKER_RESEARCH_SLOT (default off).
// When enabled later: run a short local LF session between Dig claims and submit
// only corpus deltas + crashes (not full segment replay of research execs).
func ResearchSlotFromEnv() ResearchSlotConfig {
	cfg := ResearchSlotConfig{
		WindowSec:     EnvInt("HACKME_WORKER_RESEARCH_WINDOW_SEC", 30),
		MaxDeltaFiles: EnvInt("HACKME_WORKER_RESEARCH_MAX_DELTA", 32),
	}
	v := strings.TrimSpace(os.Getenv("HACKME_WORKER_RESEARCH_SLOT"))
	cfg.Enabled = Truthy(v) // empty → false
	if cfg.WindowSec < 5 {
		cfg.WindowSec = 5
	}
	if cfg.WindowSec > 120 {
		cfg.WindowSec = 120
	}
	if cfg.MaxDeltaFiles < 1 {
		cfg.MaxDeltaFiles = 1
	}
	if cfg.MaxDeltaFiles > 256 {
		cfg.MaxDeltaFiles = 256
	}
	return cfg
}

// ResearchSlotResult is the stub outcome of a local research window.
type ResearchSlotResult struct {
	Ran           bool
	CorpusDeltas  int
	Crashes       int
	SkippedReason string
}

// MaybeRunResearchSlot is a no-op stub unless HACKME_WORKER_RESEARCH_SLOT=1.
// Residual: full LF persist + coordinator corpus-delta submit API not wired yet.
func MaybeRunResearchSlot(ctx context.Context, cfg ResearchSlotConfig) ResearchSlotResult {
	if !cfg.Enabled {
		return ResearchSlotResult{SkippedReason: "disabled"}
	}
	if err := ctx.Err(); err != nil {
		return ResearchSlotResult{SkippedReason: "ctx_done"}
	}
	// Stub: acknowledge enablement without starting LF or mutating pool corpus.
	// Seed-from-research Dig handoff remains a separate opt-in
	// (HACKME_POOL_SEED_FROM_RESEARCH, default OFF).
	return ResearchSlotResult{
		Ran:           false,
		SkippedReason: "stub_not_wired",
	}
}
