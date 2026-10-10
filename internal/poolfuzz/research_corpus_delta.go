package poolfuzz

import (
	"context"
	"fmt"
	"strings"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/fuzzupstream"
	"hackme/internal/hunt"
)

const (
	// ResearchNamespacePrefix is the only namespace family workers may write via corpus_delta.
	// Never pack:/owner:/payer: (customer Dig isolation).
	ResearchNamespacePrefix = "research:"
	maxResearchDeltaSeeds   = 256
	maxResearchDeltaCrashes = 16
	maxResearchSeedBytes    = 65536
)

// ResearchCorpusDeltaRequest is a worker-submitted LF persist window result.
type ResearchCorpusDeltaRequest struct {
	WorkerID   string
	CampaignID string
	ItemID     int64
	Namespace  string
	Seeds      []fuzzengine.PoolCorpusSeed
	Crashes    [][]byte // raw crash artifacts; never trusted without ASAN replay
	MinerAddr  string
}

// ResearchCorpusDeltaResult summarizes accept outcomes.
type ResearchCorpusDeltaResult struct {
	SeedsAccepted   int
	CrashesReplayOK int
	CrashesRejected int
	Findings        int
	Namespace       string
}

// ValidResearchCorpusNamespace allows only research:<target> (path-safe).
func ValidResearchCorpusNamespace(ns string) bool {
	ns = strings.TrimSpace(ns)
	if !strings.HasPrefix(ns, ResearchNamespacePrefix) {
		return false
	}
	rest := strings.TrimPrefix(ns, ResearchNamespacePrefix)
	if rest == "" || strings.Contains(rest, ":") {
		return false
	}
	return ValidCorpusNamespace(ns)
}

// ResearchNamespaceForTarget builds research:<targetID>.
func ResearchNamespaceForTarget(targetID string) string {
	tid := fuzzengine.SanitizeCorpusNamespacePublic(strings.TrimSpace(targetID))
	if tid == "" {
		return ""
	}
	return ResearchNamespacePrefix + tid
}

// AcceptResearchCorpusDelta merges worker LF corpus units into a research namespace
// and, for Hunt campaigns, into the leased campaign corpus. Crash artifacts are
// ASAN-replayed before any finding is recorded. Does not settle bounty/escrow
// (findings are verified; payout stays on crash-first shard / deferred paths).
func (s *Service) AcceptResearchCorpusDelta(ctx context.Context, req ResearchCorpusDeltaRequest) (ResearchCorpusDeltaResult, error) {
	var out ResearchCorpusDeltaResult
	if s == nil || s.DB == nil {
		return out, fmt.Errorf("poolfuzz: no database")
	}
	workerID := strings.TrimSpace(req.WorkerID)
	campaignID := strings.TrimSpace(req.CampaignID)
	ns := strings.TrimSpace(req.Namespace)
	if workerID == "" || campaignID == "" || req.ItemID <= 0 {
		return out, fmt.Errorf("poolfuzz: worker_id, campaign_id, item_id required")
	}
	if !ValidResearchCorpusNamespace(ns) {
		return out, fmt.Errorf("poolfuzz: research corpus namespace required (research:<target>)")
	}
	out.Namespace = ns
	if len(req.Seeds) > maxResearchDeltaSeeds {
		return out, fmt.Errorf("poolfuzz: too many corpus seeds (max %d)", maxResearchDeltaSeeds)
	}
	if len(req.Crashes) > maxResearchDeltaCrashes {
		return out, fmt.Errorf("poolfuzz: too many crash artifacts (max %d)", maxResearchDeltaCrashes)
	}

	if err := s.requireResearchLease(ctx, workerID, campaignID, req.ItemID); err != nil {
		return out, err
	}

	var cfgJSON string
	if err := s.DB.QueryRowContext(ctx, `SELECT config_json FROM fuzz_campaigns WHERE id=?`, campaignID).Scan(&cfgJSON); err != nil {
		return out, fmt.Errorf("poolfuzz: campaign: %w", err)
	}
	cfg := parseConfigJSON(cfgJSON)
	isHunt := IsHuntCampaign(cfg)
	if !isHunt && !researchSlotDigAllowed(cfg) {
		return out, fmt.Errorf("poolfuzz: research corpus_delta requires Hunt (or Dig research_slot_ok)")
	}
	targetID := strings.TrimSpace(jsonString(cfg["upstream_target_id"]))
	if isHunt {
		if targetID == "" {
			return out, fmt.Errorf("poolfuzz: hunt missing upstream_target_id")
		}
		wantNS := ResearchNamespaceForTarget(targetID)
		if ns != wantNS {
			return out, fmt.Errorf("poolfuzz: namespace must match campaign target (%s)", wantNS)
		}
	}

	now := time.Now().Unix()
	seeds := make([]fuzzengine.PoolCorpusSeed, 0, len(req.Seeds))
	for _, seed := range req.Seeds {
		if seed.Crash {
			continue
		}
		b := seed.InputBytes
		if len(b) == 0 || len(b) > maxResearchSeedBytes {
			continue
		}
		u := seed.Input
		if u == 0 {
			u = fuzzengine.PackInputBytesToU64(b)
		}
		energy := seed.Energy
		if energy < 1 {
			energy = 2
		}
		seeds = append(seeds, fuzzengine.PoolCorpusSeed{
			Input: u, InputBytes: append([]byte(nil), b...), Energy: energy, Edge: seed.Edge, Path: seed.Path,
		})
	}
	if err := s.UpsertNamespaceCorpusSeeds(ctx, ns, seeds, now); err != nil {
		return out, err
	}
	out.SeedsAccepted = len(seeds)
	if isHunt {
		for _, seed := range seeds {
			if err := s.upsertPoolCorpusSeed(ctx, campaignID, seed.Input, seed.InputBytes, seed.Energy, seed.Edge, seed.Path, false, now); err != nil {
				return out, err
			}
		}
	}

	for _, crashB := range req.Crashes {
		if len(crashB) == 0 || len(crashB) > maxResearchSeedBytes {
			out.CrashesRejected++
			continue
		}
		if !isHunt {
			// Dig research-only: store crash bytes as corpus only after namespace accept — no native ASAN path.
			u := fuzzengine.PackInputBytesToU64(crashB)
			_ = s.UpsertNamespaceCorpusSeeds(ctx, ns, []fuzzengine.PoolCorpusSeed{{
				Input: u, InputBytes: crashB, Energy: 8,
			}}, now)
			out.CrashesRejected++ // not verified as finding
			continue
		}
		ok, trap, err := researchCrashReplayer(ctx, s, campaignID, cfg, crashB)
		if err != nil || !ok {
			out.CrashesRejected++
			continue
		}
		out.CrashesReplayOK++
		u := fuzzengine.PackInputBytesToU64(crashB)
		if err := s.upsertPoolCorpusSeed(ctx, campaignID, u, crashB, 12, 0, 0, true, now); err != nil {
			return out, err
		}
		_ = s.UpsertNamespaceCorpusSeeds(ctx, ns, []fuzzengine.PoolCorpusSeed{{
			Input: u, InputBytes: crashB, Energy: 8,
		}}, now)
		info, _ := fuzzupstream.ParseHuntTrap(trap)
		if !info.Security {
			continue
		}
		sem := fuzzengine.ParseCheckSemantics(cfg)
		findingReq := SubmitRequest{
			WorkerID:        workerID,
			MinerAddress:    strings.TrimSpace(req.MinerAddr),
			CampaignID:      campaignID,
			ItemID:          req.ItemID,
			ActualInput:     u,
			InputBytes:      crashB,
			CheckResult:     1,
			Trap:            trap,
			SegmentExecDone: huntIterationsPerShard(cfg),
		}
		if _, _, _, err := s.insertFinding(ctx, findingReq, cfg, sem, false, now); err != nil {
			return out, err
		}
		out.Findings++
		_ = s.observePoolCorpusNovelty(ctx, campaignID, u, crashB, true, now, true, true, true, "native_crash")
	}
	return out, nil
}

func (s *Service) requireResearchLease(ctx context.Context, workerID, campaignID string, itemID int64) error {
	var owner, status string
	err := s.DB.QueryRowContext(ctx,
		`SELECT COALESCE(lease_owner,''), COALESCE(status,'') FROM fuzz_work_items
		 WHERE id=? AND campaign_id=?`, itemID, campaignID).Scan(&owner, &status)
	if err != nil {
		return fmt.Errorf("poolfuzz: work item: %w", err)
	}
	if status != "leased" || owner != workerID {
		return fmt.Errorf("poolfuzz: research corpus_delta requires active lease")
	}
	return nil
}

func researchSlotDigAllowed(cfg map[string]any) bool {
	if cfg == nil {
		return false
	}
	v := strings.TrimSpace(strings.ToLower(jsonString(cfg["research_slot_ok"])))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// researchCrashReplayer verifies a crash input against the campaign Hunt harness.
// Overridable in tests.
var researchCrashReplayer = defaultResearchCrashReplay

func defaultResearchCrashReplay(ctx context.Context, s *Service, campaignID string, cfg map[string]any, input []byte) (bool, string, error) {
	if !huntReplayEnabled() {
		// Fail closed: never trust worker crash bytes without ASAN replay.
		return false, "", fmt.Errorf("poolfuzz: hunt replay required for research crashes")
	}
	targetID := strings.TrimSpace(jsonString(cfg["upstream_target_id"]))
	if targetID == "" {
		return false, "", fmt.Errorf("poolfuzz: hunt missing upstream_target_id")
	}
	release, err := acquireHuntReplaySlot(ctx)
	if err != nil {
		return false, "", err
	}
	defer release()
	maxB := fuzzengine.ParseMaxInputBytes(cfg)
	if maxB <= 0 {
		maxB = 4096
	}
	if len(input) > maxB {
		input = input[:maxB]
	}
	// Single-input replay: CampaignID empty so ReplayShard uses opts.Input.
	rep, err := hunt.ReplayShard(ctx, hunt.ReplayShardOpts{
		RepoRoot:             hunt.RepoRoot(),
		Spec:                 hunt.HarnessSpecFromConfig(cfg),
		TargetID:             targetID,
		HarnessHash:          strings.TrimSpace(jsonString(cfg["harness_hash"])),
		HarnessFetchURL:      huntHarnessFetchURL(cfg),
		HarnessContentSHA256: huntHarnessContentSHA256(ctx, s, strings.TrimSpace(jsonString(cfg["harness_hash"])), cfg),
		ArtifactDB:           s.DB,
		Input:                input,
		MaxInput:             maxB,
		ExecPer:              1,
		Config:               cfg,
	})
	if err != nil {
		return false, "", err
	}
	if !rep.Crash {
		return false, "hunt_replay_reject:fake_crash", nil
	}
	return true, rep.Trap, nil
}
