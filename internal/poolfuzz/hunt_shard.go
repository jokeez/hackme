package poolfuzz

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"

	"hackme/internal/fuzzengine"
	"hackme/internal/fuzzescrow"
	"hackme/internal/fuzzupstream"
	"hackme/internal/hunt"
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
		HuntSource:         strings.TrimSpace(jsonString(cfg["hunt_source"])),
		HuntPinPath:        strings.TrimSpace(jsonString(cfg["hunt_pin_path"])),
		HuntSourceRel:      strings.TrimSpace(jsonString(cfg["hunt_source_rel"])),
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

func huntReplayEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_POOL_HUNT_REPLAY")))
	return v == "" || v == "1" || v == "true" || v == "yes" || v == "on"
}

func (s *Service) evalHuntSubmitCheck(ctx context.Context, cfg map[string]any, req SubmitRequest, expectedB []byte) (checkResult int32, trap string, pass bool, recordFinding bool, err error) {
	iter := huntIterationsPerShard(cfg)
	if req.SegmentExecDone != iter {
		return 0, "", false, false, nil
	}
	if !huntReplayEnabled() {
		cr, tr, p, rf := s.evalHuntSubmitTrusted(cfg, req, expectedB)
		return cr, tr, p, rf, nil
	}
	targetID := strings.TrimSpace(jsonString(cfg["upstream_target_id"]))
	if targetID == "" {
		return 0, "", false, false, fmt.Errorf("poolfuzz: hunt missing upstream_target_id")
	}
	maxB := fuzzengine.ParseMaxInputBytes(cfg)
	if maxB <= 0 {
		maxB = 4096
	}
	rep, err := hunt.ReplayShard(ctx, hunt.ReplayShardOpts{
		RepoRoot: hunt.RepoRoot(),
		Spec:     hunt.HarnessSpecFromConfig(cfg),
		TargetID: targetID,
		HarnessHash: strings.TrimSpace(jsonString(cfg["harness_hash"])),
		Input:    expectedB,
		MaxInput: maxB,
		ExecPer:  iter,
	})
	if err != nil {
		return 0, "", false, false, fmt.Errorf("poolfuzz: hunt replay: %w", err)
	}
	workerClaimsCrash := strings.HasPrefix(strings.TrimSpace(req.Trap), "hunt_crash:") && req.CheckResult != 0
	if rep.Crash {
		if !fuzzupstream.IsSecuritySanitizer(rep.Sanitizer) {
			return 0, rep.Trap, false, false, nil
		}
		if !workerClaimsCrash {
			return 0, rep.Trap, false, false, nil
		}
		return 1, rep.Trap, true, true, nil
	}
	if workerClaimsCrash {
		return 0, "hunt_replay_reject:fake_crash", false, false, nil
	}
	return 0, "", true, false, nil
}

func (s *Service) evalHuntSubmitTrusted(cfg map[string]any, req SubmitRequest, expectedB []byte) (checkResult int32, trap string, pass bool, recordFinding bool) {
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

func classifyHuntFinding(cfg map[string]any, req SubmitRequest) (ft, sev, title string) {
	trap := strings.TrimSpace(req.Trap)
	target := strings.TrimSpace(jsonString(cfg["upstream_target_id"]))
	if strings.HasPrefix(trap, "hunt_crash:") {
		san := strings.TrimPrefix(trap, "hunt_crash:")
		ft = "native_crash"
		sev = "high"
		title = fmt.Sprintf("Hunt ASAN %s on %s", san, target)
		return ft, sev, title
	}
	ft, sev, title = fuzzengine.ClassifyCheckFail(req.ActualInput, false, fuzzengine.SemanticsDetector)
	if title == "" {
		title = "Hunt shard on " + target
	}
	return ft, sev, title
}
