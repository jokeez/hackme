package poolfuzz

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/fuzzescrow"
	"hackme/internal/fuzzupstream"
	"hackme/internal/hunt"
)

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
	return hunt.ShardIterationsPer(cfg)
}

// HuntShardInputBytes derives deterministic anchor input for one Hunt shard (exec 0).
func HuntShardInputBytes(campaignID string, inputN uint64, cfg map[string]any) []byte {
	return hunt.ShardAnchorBytes(campaignID, inputN, cfg)
}

func (s *Service) buildHuntClaimedWork(ctx context.Context, campaignID string, itemID int64, inputN uint64, cfg map[string]any, workerID string) (ClaimedWork, error) {
	_ = workerID
	iter := huntIterationsPerShard(cfg)
	now := time.Now().Unix()
	var inputB []byte
	var inputU uint64
	var corpusSeeds []fuzzengine.PoolCorpusSeed
	var corpusSHA string
	if hunt.HuntCorpusGuided(cfg) {
		var err error
		inputU, inputB, corpusSeeds, err = s.lockHuntGuidedWorkItem(ctx, campaignID, itemID, inputN, cfg, now)
		if err != nil {
			return ClaimedWork{}, err
		}
		if _, corpusSHA, err = fuzzengine.EncodeCorpusSnapshot(corpusSeeds); err != nil {
			return ClaimedWork{}, err
		}
	} else {
		inputB = HuntShardInputBytes(campaignID, inputN, cfg)
		inputU = fuzzengine.PackInputBytesToU64(inputB)
		if err := s.storeExpectedInputs(ctx, campaignID, itemID, inputU, inputB); err != nil {
			return ClaimedWork{}, err
		}
	}
	return ClaimedWork{
		WorkID:               fmt.Sprintf("%s:%d", campaignID, itemID),
		CampaignID:           campaignID,
		ItemID:               itemID,
		InputN:               inputN,
		ActualInput:          inputU,
		InputBytes:           inputB,
		InputMode:            string(fuzzengine.InputModeBytes),
		CheckSemantics:       "native_crash",
		DepthTier:            string(fuzzengine.DepthOSSCVE),
		PerRunHMC:            perShardHMCFromConfig(cfg),
		ExecPerUnit:          iter,
		MaxInputBytes:        fuzzengine.ParseMaxInputBytes(cfg),
		CoverageKind:         huntCoverageKind(cfg, iter),
		CorpusSeeds:          corpusSeeds,
		CorpusSnapshotSHA256: corpusSHA,
		TaskClass:            "hunt",
		WorkKind:             "hunt_shard",
		HarnessHash:          strings.TrimSpace(jsonString(cfg["harness_hash"])),
		UpstreamTargetID:     strings.TrimSpace(jsonString(cfg["upstream_target_id"])),
		HuntSource:           strings.TrimSpace(jsonString(cfg["hunt_source"])),
		HuntPinPath:          strings.TrimSpace(jsonString(cfg["hunt_pin_path"])),
		HuntSourceRel:        strings.TrimSpace(jsonString(cfg["hunt_source_rel"])),
		HarnessFetchURL:      huntHarnessFetchURL(cfg),
		IterationsPerShard:   iter,
		HuntDetectLeaks:      hunt.DetectLeaksFromConfig(cfg),
	}, nil
}

func huntCoverageKind(cfg map[string]any, iter int) string {
	if hunt.HuntCorpusGuided(cfg) {
		return "hunt_corpus_guided"
	}
	if hunt.ShardSegmentMutating(cfg) && iter > 1 {
		return "hunt_segment_mutating"
	}
	return "input_fingerprint"
}

func huntHarnessFetchURL(cfg map[string]any) string {
	if v := strings.TrimSpace(jsonString(cfg["harness_fetch_url"])); v != "" {
		return v
	}
	if v := strings.TrimSpace(jsonString(cfg["harness_fetch_path"])); v != "" {
		return v
	}
	return hunt.HarnessFetchURL(strings.TrimSpace(jsonString(cfg["harness_hash"])))
}

func huntCrashSeverity(san string) string {
	switch {
	case strings.Contains(san, "heap-buffer-overflow"),
		strings.Contains(san, "use-after-free"),
		strings.Contains(san, "double-free"),
		strings.Contains(san, "heap-use-after-free"):
		return "critical"
	default:
		return "high"
	}
}

func huntBountyEligible(cfg map[string]any, sev string) bool {
	if !IsHuntCampaign(cfg) {
		return bountySeverity(sev)
	}
	switch strings.TrimSpace(strings.ToLower(sev)) {
	case "critical", "high":
		return true
	default:
		return false
	}
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

func (s *Service) evalHuntSubmitCheck(ctx context.Context, campaignID string, inputN uint64, cfg map[string]any, req SubmitRequest, expectedB []byte, seeds []fuzzengine.PoolCorpusSeed) (checkResult int32, trap string, pass bool, recordFinding bool, findingB []byte, findingOrigLen int, err error) {
	iter := huntIterationsPerShard(cfg)
	if req.SegmentExecDone != iter {
		return 0, "", false, false, nil, 0, nil
	}
	if !huntReplayEnabled() {
		cr, tr, p, rf := s.evalHuntSubmitTrusted(cfg, req, expectedB)
		orig := len(expectedB)
		if rf && orig > 0 {
			return cr, tr, p, rf, expectedB, orig, nil
		}
		return cr, tr, p, rf, nil, 0, nil
	}
	targetID := strings.TrimSpace(jsonString(cfg["upstream_target_id"]))
	if targetID == "" {
		return 0, "", false, false, nil, 0, fmt.Errorf("poolfuzz: hunt missing upstream_target_id")
	}
	maxB := fuzzengine.ParseMaxInputBytes(cfg)
	if maxB <= 0 {
		maxB = 4096
	}
	rep, err := hunt.ReplayShard(ctx, hunt.ReplayShardOpts{
		RepoRoot:        hunt.RepoRoot(),
		Spec:            hunt.HarnessSpecFromConfig(cfg),
		TargetID:        targetID,
		HarnessHash:     strings.TrimSpace(jsonString(cfg["harness_hash"])),
		HarnessFetchURL: huntHarnessFetchURL(cfg),
		CampaignID:      campaignID,
		InputN:          inputN,
		Config:          cfg,
		CorpusSeeds:     seeds,
		Input:           expectedB,
		MaxInput:        maxB,
		ExecPer:         iter,
	})
	if err != nil {
		return 0, "", false, false, nil, 0, fmt.Errorf("poolfuzz: hunt replay: %w", err)
	}
	workerTrap := strings.TrimSpace(req.Trap)
	workerClaims := req.CheckResult != 0 && (strings.HasPrefix(workerTrap, "hunt_crash:") || strings.HasPrefix(workerTrap, "hunt_sanitizer:"))
	fb := expectedB
	origLen := rep.CrashInputOriginalLen
	if len(rep.CrashInput) > 0 {
		fb = rep.CrashInput
	}
	if origLen <= 0 && len(fb) > 0 {
		origLen = len(fb)
	}
	if rep.Crash {
		if rep.SanitizerInfo.Security {
			if !workerClaims || !strings.HasPrefix(workerTrap, "hunt_crash:") {
				return 0, rep.Trap, false, false, nil, 0, nil
			}
			return 1, rep.Trap, true, true, fb, origLen, nil
		}
		if !workerClaims || !strings.HasPrefix(workerTrap, "hunt_sanitizer:") {
			return 0, rep.Trap, true, false, nil, 0, nil
		}
		return 0, rep.Trap, true, true, fb, origLen, nil
	}
	if workerClaims && strings.HasPrefix(workerTrap, "hunt_crash:") {
		return 0, "hunt_replay_reject:fake_crash", false, false, nil, 0, nil
	}
	if workerClaims && strings.HasPrefix(workerTrap, "hunt_sanitizer:") {
		return 0, "hunt_replay_reject:fake_sanitizer", false, false, nil, 0, nil
	}
	return 0, "", true, false, nil, 0, nil
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
	if strings.HasPrefix(trap, "hunt_sanitizer:") && req.CheckResult != 0 {
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
	if info, ok := fuzzupstream.ParseHuntTrap(trap); ok {
		if info.Security {
			ft = "native_crash"
			sev = huntCrashSeverity(info.Subtype)
			title = fmt.Sprintf("Hunt %s on %s", info.Label, target)
			return ft, sev, title
		}
		ft = "sanitizer_informational"
		sev = fuzzupstream.InformationalSeverity(info)
		if sev == "" {
			sev = "info"
		}
		title = fmt.Sprintf("Hunt %s on %s", info.Label, target)
		return ft, sev, title
	}
	ft, sev, title = fuzzengine.ClassifyCheckFail(req.ActualInput, false, fuzzengine.SemanticsDetector)
	if title == "" {
		title = "Hunt shard on " + target
	}
	return ft, sev, title
}
