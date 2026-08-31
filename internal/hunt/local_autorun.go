package hunt

import (
	"context"
	"fmt"
	"strings"

	"hackme/internal/fuzzengine"
	"hackme/internal/fuzzupstream"
)

// LocalAutorunState is persisted in campaign summary_json between autorunner ticks.
type LocalAutorunState struct {
	IterationsDone int    `json:"hunt_local_iterations"`
	CrashesFound   int    `json:"hunt_local_crashes"`
	Verdict        string `json:"hunt_local_verdict,omitempty"`
	StartedAt      int64  `json:"hunt_local_started_at"`
	Completed      bool   `json:"hunt_local_done"`
	LastTickAt     int64  `json:"hunt_local_last_tick_at"`
}

// ParseLocalAutorunState reads overnight progress from campaign summary.
func ParseLocalAutorunState(summary map[string]any) LocalAutorunState {
	st := LocalAutorunState{}
	if summary == nil {
		return st
	}
	st.IterationsDone = int(cfgInt(summary, "hunt_local_iterations"))
	st.CrashesFound = int(cfgInt(summary, "hunt_local_crashes"))
	st.Verdict = strings.TrimSpace(cfgString(summary, "hunt_local_verdict"))
	st.StartedAt = int64(cfgInt(summary, "hunt_local_started_at"))
	st.LastTickAt = int64(cfgInt(summary, "hunt_local_last_tick_at"))
	st.Completed = cfgTruthy(summary["hunt_local_done"])
	return st
}

// LocalAutorunStateToSummary merges state into summary map for DB persistence.
func LocalAutorunStateToSummary(summary map[string]any, st LocalAutorunState) map[string]any {
	if summary == nil {
		summary = map[string]any{}
	}
	summary["hunt_local_iterations"] = st.IterationsDone
	summary["hunt_local_crashes"] = st.CrashesFound
	summary["hunt_local_verdict"] = st.Verdict
	summary["hunt_local_started_at"] = st.StartedAt
	summary["hunt_local_last_tick_at"] = st.LastTickAt
	summary["hunt_local_done"] = st.Completed
	summary["runs_done"] = st.IterationsDone
	return summary
}

// LocalAutorunTick runs one batch of node-local Hunt iterations for overnight mode.
func LocalAutorunTick(ctx context.Context, repoRoot string, cfg map[string]any, st LocalAutorunState, now int64) (LocalAutorunState, *fuzzupstream.HuntReport, error) {
	if st.Completed {
		return st, nil, nil
	}
	targetID := strings.TrimSpace(cfgString(cfg, "upstream_target_id"))
	if targetID == "" {
		return st, nil, fmt.Errorf("hunt local autorun: upstream_target_id required")
	}
	pkgKey := PackageKeyFromConfig(cfg)
	if st.StartedAt == 0 {
		st.StartedAt = now
	}
	budgetTotal := LocalRunBudgetFromConfig(cfg, pkgKey)
	wallSec := LocalRunTimeLimitFromConfig(cfg, pkgKey)
	tickIter := cfgInt(cfg, "hunt_local_tick_iterations")
	if tickIter <= 0 {
		tickIter = huntLocalTickIter
	}
	remaining := budgetTotal - st.IterationsDone
	if remaining <= 0 {
		st.Completed = true
		return st, nil, nil
	}
	if tickIter > remaining {
		tickIter = remaining
	}
	if wallSec > 0 && st.StartedAt > 0 && now-st.StartedAt >= int64(wallSec) {
		st.Completed = true
		return st, nil, nil
	}

	rep, err := LocalRunWithConfig(ctx, LocalRunOptions{
		RepoRoot:         repoRoot,
		TargetID:         targetID,
		BudgetIterations: tickIter,
		TimeLimitSec:     120,
		Config:           cfg,
	})
	if err != nil {
		return st, nil, err
	}
	st.IterationsDone += rep.Iterations
	st.CrashesFound += len(rep.Crashes)
	st.Verdict = rep.Verdict
	st.LastTickAt = now
	if st.IterationsDone >= budgetTotal {
		st.Completed = true
	}
	if wallSec > 0 && st.StartedAt > 0 && now-st.StartedAt >= int64(wallSec) {
		st.Completed = true
	}
	return st, rep, nil
}

// HuntRunOptionsFromConfig builds fuzzupstream Hunt options from campaign config.
func HuntRunOptionsFromConfig(cfg map[string]any) fuzzupstream.HuntRunOptions {
	opts := fuzzupstream.HuntRunOptions{DetectLeaks: DetectLeaksFromConfig(cfg)}
	if cfg != nil {
		opts.MutatorDict = fuzzengine.ParseMutatorDict(cfg)
	}
	return opts
}
