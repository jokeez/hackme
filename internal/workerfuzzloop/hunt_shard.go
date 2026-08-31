package workerfuzzloop

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/hunt"
)

// HuntShardsEnabled is false when HACKME_WORKER_HUNT_SHARDS=0.
func HuntShardsEnabled() bool {
	return !Falsy(os.Getenv("HACKME_WORKER_HUNT_SHARDS"))
}

// RunHuntShard executes one Hunt pool shard (ASAN harness, anchor + L1 segment mutations).
func RunHuntShard(ctx context.Context, cr ClaimResp, timeoutMS int) (checkResult int32, durationMS int, trap string, execDone int) {
	if !HuntShardsEnabled() {
		return 0, 0, "hunt disabled", 0
	}
	start := time.Now()
	inputB, err := hex.DecodeString(strings.TrimSpace(cr.InputBytesHex))
	if err != nil || len(inputB) == 0 {
		return 0, 0, "missing input_bytes", 0
	}
	targetID := strings.TrimSpace(cr.UpstreamTargetID)
	if targetID == "" {
		return 0, 0, "missing upstream_target_id", 0
	}
	repoRoot := hunt.RepoRoot()
	runCtx := ctx
	if timeoutMS > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
		defer cancel()
	}
	maxB := cr.MaxInputBytes
	if maxB <= 0 {
		maxB = 4096
	}
	execPer := cr.ExecPerUnit
	if execPer < 1 {
		execPer = 1
	}
	seeds, _ := fuzzengine.CorpusSeedsFromClaimMaps(cr.CorpusSeeds)
	cfg := huntShardConfigFromClaim(cr, len(seeds) > 0)
	rep, err := hunt.ReplayShard(runCtx, hunt.ReplayShardOpts{
		RepoRoot: repoRoot,
		Spec: hunt.HarnessSpec{
			Source:      strings.TrimSpace(cr.HuntSource),
			TargetID:    targetID,
			HarnessHash: strings.TrimSpace(cr.HarnessHash),
			PinPath:     strings.TrimSpace(cr.HuntPinPath),
			SourceRel:   strings.TrimSpace(cr.HuntSourceRel),
		},
		TargetID:        targetID,
		HarnessHash:     strings.TrimSpace(cr.HarnessHash),
		HarnessFetchURL: huntFetchURL(cr),
		CampaignID:      strings.TrimSpace(cr.CampaignID),
		InputN:          cr.InputN,
		Config:          cfg,
		CorpusSeeds:     seeds,
		Input:           inputB,
		MaxInput:        maxB,
		ExecPer:         execPer,
	})
	if err != nil {
		return 0, int(time.Since(start).Milliseconds()), "build: " + err.Error(), 0
	}
	if rep.Crash {
		return 1, int(time.Since(start).Milliseconds()), rep.Trap, execPer
	}
	return 0, int(time.Since(start).Milliseconds()), "", execPer
}

// IsHuntClaim reports Hunt shard work from coordinator claim JSON.
func IsHuntClaim(cr ClaimResp) bool {
	if strings.EqualFold(strings.TrimSpace(cr.TaskClass), "hunt") {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(cr.WorkKind), "hunt_shard")
}

// HuntClaimMissingFields returns an error if required Hunt fields are absent.
func HuntClaimMissingFields(cr ClaimResp) error {
	if strings.TrimSpace(cr.UpstreamTargetID) == "" {
		return fmt.Errorf("hunt claim missing upstream_target_id")
	}
	if strings.TrimSpace(cr.InputBytesHex) == "" {
		return fmt.Errorf("hunt claim missing input_bytes_hex")
	}
	return nil
}

func huntFetchURL(cr ClaimResp) string {
	u := strings.TrimSpace(cr.HarnessFetchURL)
	if u == "" {
		return ""
	}
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_URL")), "/")
	if base == "" {
		base = strings.TrimRight(strings.TrimSpace(os.Getenv("HACKME_POOL_COORDINATOR_URL")), "/")
	}
	if base == "" {
		return u
	}
	return base + "/" + strings.TrimPrefix(u, "/")
}

func huntShardConfigFromClaim(cr ClaimResp, corpusGuided bool) map[string]any {
	cfg := map[string]any{
		"upstream_target_id":   strings.TrimSpace(cr.UpstreamTargetID),
		"max_input_bytes":      cr.MaxInputBytes,
		"input_mode":           "bytes",
		"iterations_per_shard": cr.ExecPerUnit,
	}
	if tier := strings.TrimSpace(cr.DepthTier); tier != "" {
		cfg["depth_tier"] = tier
	} else {
		cfg["depth_tier"] = "oss_cve"
	}
	if corpusGuided || strings.TrimSpace(cr.CoverageKind) == "hunt_corpus_guided" {
		cfg["hunt_corpus_guided"] = true
		cfg["guided_scheduling"] = true
	}
	if cr.HuntDetectLeaks {
		cfg["hunt_detect_leaks"] = true
	}
	return cfg
}
