package poolfuzz

import (
	"os"
	"strconv"
	"strings"

	"hackme/internal/fuzzengine"
	"hackme/internal/hunt"
	"hackme/internal/sandbox"
)

// poolExecPerUnitCap limits Dig exec/unit on distributed pool (hub default 64).
// Sampled hygiene skip (HACKME_POOL_REPLAY_SAMPLE_PCT*) reduces coordinator replay load;
// override with HACKME_POOL_EXEC_PER_UNIT_CAP (e.g. 256) when fleet + sampled replay can absorb deeper segments.
const poolExecPerUnitCap = 64

// huntExecTimeoutMS matches fuzzupstream.RunInputDetailed per-exec wall budget.
const huntExecTimeoutMS = 3000

func effectivePoolExecPerUnitCap() int {
	v := strings.TrimSpace(os.Getenv("HACKME_POOL_EXEC_PER_UNIT_CAP"))
	if v == "" {
		return poolExecPerUnitCap
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return poolExecPerUnitCap
	}
	if n > fuzzengine.MaxExecPerUnitHardCeil() {
		return fuzzengine.MaxExecPerUnitHardCeil()
	}
	return n
}

// PoolExecPerUnit returns exec_per_unit for pool claim/submit/replay (capped on distributed pool).
func PoolExecPerUnit(cfg map[string]any) int {
	n := fuzzengine.ExecPerUnit(cfg)
	if !poolDistributed(cfg) {
		return n
	}
	capN := effectivePoolExecPerUnitCap()
	if n > capN {
		return capN
	}
	return n
}

// leaseSecondsForConfig scales worker lease to segment wall time (avoid mid-segment reclaim).
func leaseSecondsForConfig(cfg map[string]any) int64 {
	var execPer int
	var timeoutMS int64
	if IsHuntCampaign(cfg) {
		// Coverage-guided Hunt: lease ≈ engine wall + slack (not L1 iter × 3s).
		if HuntEngineEnabled() {
			engine := hunt.HuntEngineFromConfig(cfg)
			if hunt.IsCoverageEngine(engine) {
				// Claim lease must outlive the engine wall + HTTP/corpus_delta slack.
				// Was engine+90; 120s walls under a 90s advertised lease still fit in
				// engine+90=180, but hybrid-unit restarts mid-shard caused reclaim races.
				// Keep ≥2× wall (min +120s slack) so finalize wins before reclaim.
				eng := int64(hunt.EngineLeaseSecFromConfig(cfg, engine))
				sec := eng * 2
				if alt := eng + 120; alt > sec {
					sec = alt
				}
				if sec < 120 {
					sec = 120
				}
				// Must stay above the longest engine wall: a lease that expires while
				// the engine is still running hands the range to another worker and the
				// finished shard is thrown away. Tied to the wall constants so raising
				// one without the other cannot silently invert it.
				if cap := int64(hunt.PoolReclaimLeaseCapSec); sec > cap {
					sec = cap
				}
				return sec
			}
		}
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
	// Wall ≈ exec × timeout; add slack for queue/HTTP jitter (Dig segments need more headroom).
	sec := int64((execPer * int(timeoutMS)) / 1000)
	slack := int64(60)
	if !IsHuntCampaign(cfg) && execPer >= 32 {
		slack = 90
	}
	sec += slack
	minSec := int64(30)
	if !IsHuntCampaign(cfg) && execPer >= 32 {
		minSec = 90
	}
	if sec < minSec {
		return minSec
	}
	// Hunt ASAN shards: keep leases ≤6m so dead/misconfigured workers free shards
	// for reclaim (was 10m; fleet lease pile-up starved bootstrap progress).
	maxSec := int64(600)
	if IsHuntCampaign(cfg) {
		maxSec = 360
	}
	if sec > maxSec {
		return maxSec
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
