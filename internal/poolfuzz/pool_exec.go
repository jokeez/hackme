package poolfuzz

import (
	"hackme/internal/fuzzengine"
	"hackme/internal/sandbox"
)

// poolExecPerUnitCap limits coordinator full-segment replay on distributed pool until
// sampled worker attestation exists (Phase 2 safety valve).
const poolExecPerUnitCap = 64

// huntExecTimeoutMS matches fuzzupstream.RunInputDetailed per-exec wall budget.
const huntExecTimeoutMS = 3000

// PoolExecPerUnit returns exec_per_unit for pool claim/submit/replay (capped on distributed pool).
func PoolExecPerUnit(cfg map[string]any) int {
	n := fuzzengine.ExecPerUnit(cfg)
	if !poolDistributed(cfg) {
		return n
	}
	if n > poolExecPerUnitCap {
		return poolExecPerUnitCap
	}
	return n
}

// leaseSecondsForConfig scales worker lease to segment wall time (avoid mid-segment reclaim).
func leaseSecondsForConfig(cfg map[string]any) int64 {
	var execPer int
	var timeoutMS int64
	if IsHuntCampaign(cfg) {
		execPer = huntIterationsPerShard(cfg)
		if execPer < 1 {
			execPer = 1
		}
		timeoutMS = huntExecTimeoutMS
	} else {
		execPer = PoolExecPerUnit(cfg)
		if execPer < 1 {
			execPer = 1
		}
		timeoutMS = sandbox.Policy().CheckTimeoutMS
		if timeoutMS <= 0 {
			timeoutMS = 300
		}
	}
	// Wall ≈ exec × timeout; add 60s slack for queue/HTTP jitter.
	sec := int64((execPer * int(timeoutMS)) / 1000)
	sec += 60
	if sec < 30 {
		return 30
	}
	if sec > 600 {
		return 600
	}
	return sec
}

// poolReplayConfig returns cfg with exec_per_unit capped for distributed pool replay.
func poolReplayConfig(cfg map[string]any) map[string]any {
	out := make(map[string]any, len(cfg)+1)
	for k, v := range cfg {
		out[k] = v
	}
	out["exec_per_unit"] = PoolExecPerUnit(cfg)
	return out
}
