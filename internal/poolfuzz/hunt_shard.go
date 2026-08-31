package poolfuzz

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"hackme/internal/fuzzengine"
	"hackme/internal/fuzzescrow"
)

const defaultHuntIterationsPerShard = 32

// IsHuntCampaign reports pool Hunt shard work (not Dig WASM).
func IsHuntCampaign(cfg map[string]any) bool {
	if cfg == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(jsonString(cfg["work_kind"])), "hunt_shard") {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(jsonString(cfg["campaign_type"])), "hunt")
}

func huntIterationsPerShard(cfg map[string]any) int {
	n := intFromJSON(cfg["iterations_per_shard"])
	if n < 1 {
		n = defaultHuntIterationsPerShard
	}
	if n > 64 {
		n = 64
	}
	return n
}

// HuntShardInputBytes derives deterministic frozen input for one shard.
func HuntShardInputBytes(campaignID string, inputN uint64, cfg map[string]any) []byte {
	seed := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s", campaignID, inputN, jsonString(cfg["upstream_target_id"]))))
	maxB := fuzzengine.ParseMaxInputBytes(cfg)
	if maxB < 16 {
		maxB = 256
	}
	if maxB > 4096 {
		maxB = 4096
	}
	out := make([]byte, maxB)
	copy(out, seed[:])
	if maxB > 32 {
		for i := 32; i < maxB; i++ {
			out[i] = seed[i%32] ^ byte(i)
		}
	}
	return out
}

func (s *Service) buildHuntClaimedWork(ctx context.Context, campaignID string, itemID int64, inputN uint64, cfg map[string]any, workerID string) (ClaimedWork, error) {
	iter := huntIterationsPerShard(cfg)
	inputB := HuntShardInputBytes(campaignID, inputN, cfg)
	if err := s.storeExpectedInputs(ctx, campaignID, itemID, fuzzengine.PackInputBytesToU64(inputB), inputB); err != nil {
		return ClaimedWork{}, err
	}
	return ClaimedWork{
		WorkID:             fmt.Sprintf("%s:%d", campaignID, itemID),
		CampaignID:         campaignID,
		ItemID:             itemID,
		InputN:             inputN,
		ActualInput:        fuzzengine.PackInputBytesToU64(inputB),
		InputBytes:         inputB,
		InputMode:          string(fuzzengine.InputModeBytes),
		CheckSemantics:     "native_crash",
		DepthTier:          string(fuzzengine.DepthOSSCVE),
		PerRunHMC:          perShardHMCFromConfig(cfg),
		ExecPerUnit:        iter,
		MaxInputBytes:      fuzzengine.ParseMaxInputBytes(cfg),
		CoverageKind:       "input_fingerprint",
		TaskClass:          "hunt",
		WorkKind:           "hunt_shard",
		HarnessHash:        strings.TrimSpace(jsonString(cfg["harness_hash"])),
		UpstreamTargetID:   strings.TrimSpace(jsonString(cfg["upstream_target_id"])),
		IterationsPerShard: iter,
	}, nil
}

func perShardHMCFromConfig(cfg map[string]any) float64 {
	if cfg == nil {
		return 0
	}
	budget := floatFromJSON(cfg["budget_hmc"])
	shards := intFromJSON(cfg["budget_shards"])
	if shards <= 0 {
		shards = intFromJSON(cfg["budget_runs"])
	}
	if budget <= 0 || shards < fuzzescrow.HuntMinShards {
		return 0
	}
	share := 0.20
	if strings.TrimSpace(jsonString(cfg["escrow_split"])) == fuzzescrow.EscrowSplit5050 {
		share = 0.50
	}
	return (budget * share) / float64(shards)
}

func (s *Service) evalHuntSubmit(cfg map[string]any, req SubmitRequest, expectedB []byte) (checkResult int32, trap string, pass bool, recordFinding bool) {
	iter := huntIterationsPerShard(cfg)
	if req.SegmentExecDone != iter {
		return 0, "", false, false
	}
	trap = strings.TrimSpace(req.Trap)
	if strings.HasPrefix(trap, "hunt_crash:") && req.CheckResult != 0 {
		return req.CheckResult, trap, true, true
	}
	if req.CheckResult != 0 {
		return req.CheckResult, trap, false, false
	}
	return 0, "", true, false
}
