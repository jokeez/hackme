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
	WorkerID      string
	CampaignID    string
	ItemID        int64
	Namespace     string
	Seeds         []fuzzengine.PoolCorpusSeed
	Crashes       [][]byte // raw crash artifacts; never trusted without ASAN replay
	MinerAddr     string
	HuntEngine    string // advisory report (libfuzzer|aflpp); stamped into hunt_proven_engine
	EngineVersion string
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

// campaignCrashInputKnown reports whether this crash was already recorded for the
// campaign (pool corpus is_crash row and/or findings by input_sha256). Used to
// refuse full PayRun credit for re-harvested / re-uploaded crash bytes.
func (s *Service) campaignCrashInputKnown(ctx context.Context, campaignID string, inputU64 uint64, inputSHA string) (bool, error) {
	if s == nil || s.DB == nil {
		return false, fmt.Errorf("poolfuzz: no database")
	}
	campaignID = strings.TrimSpace(campaignID)
	inputSHA = strings.TrimSpace(strings.ToLower(inputSHA))
	if campaignID == "" {
		return false, fmt.Errorf("poolfuzz: campaign_id required")
	}
	var n int
	if inputU64 != 0 {
		err := s.DB.QueryRowContext(ctx,
			`SELECT COUNT(1) FROM fuzz_pool_corpus WHERE campaign_id=? AND input_u64=? AND is_crash=1`,
			campaignID, poolCorpusU64Arg(inputU64)).Scan(&n)
		if err != nil {
			return false, err
		}
		if n > 0 {
			return true, nil
		}
	}
	if inputSHA != "" {
		err := s.DB.QueryRowContext(ctx,
			`SELECT COUNT(1) FROM fuzz_findings WHERE campaign_id=? AND lower(input_sha256)=?`,
			campaignID, inputSHA).Scan(&n)
		if err != nil {
			return false, err
		}
		if n > 0 {
			return true, nil
		}
	}
	return false, nil
}

// campaignCrashBucketKnown reports whether a StableCrashBucket was already paid/recorded
// for this campaign. Exact-byte dedupe alone is not enough: a worker can flip one byte in
// the first 8 while keeping the same ASAN trap and re-earn crash-tier PayRun (valax).
func (s *Service) campaignCrashBucketKnown(ctx context.Context, campaignID, bucket string) (bool, error) {
	if s == nil || s.DB == nil {
		return false, fmt.Errorf("poolfuzz: no database")
	}
	campaignID = strings.TrimSpace(campaignID)
	bucket = strings.TrimSpace(bucket)
	if campaignID == "" || bucket == "" || bucket == "crash|generic" {
		return false, nil
	}
	var n int
	err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM fuzz_findings
		 WHERE campaign_id=? AND json_valid(detail_json)
		   AND json_extract(detail_json,'$.stable_crash_key')=?`,
		campaignID, bucket).Scan(&n)
	if err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}
	// Legacy findings (pre-key stamp): compare StableCrashBucket on stored trap.
	rows, err := s.DB.QueryContext(ctx,
		`SELECT COALESCE(finding_type,''), COALESCE(json_extract(detail_json,'$.trap'),'')
		 FROM fuzz_findings WHERE campaign_id=? AND json_valid(detail_json)
		   AND COALESCE(json_extract(detail_json,'$.trap'),'') <> ''`,
		campaignID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var ft, trap string
		if err := rows.Scan(&ft, &trap); err != nil {
			return false, err
		}
		if fuzzengine.StableCrashBucket(ft, trap) == bucket {
			return true, nil
		}
	}
	return false, rows.Err()
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
	var targetID string
	if isHunt {
		targetID = strings.TrimSpace(jsonString(cfg["upstream_target_id"]))
		if targetID == "" {
			return out, fmt.Errorf("poolfuzz: hunt missing upstream_target_id")
		}
	} else {
		// Dig research_slot: bind to guard pack / campaign id so leases cannot
		// spray arbitrary research:<target> namespaces.
		if gp := strings.TrimSpace(jsonString(cfg["guard_pack"])); gp != "" {
			targetID = "dig:" + gp
		} else {
			targetID = "dig:" + campaignID
		}
	}
	wantNS := ResearchNamespaceForTarget(targetID)
	if ns != wantNS {
		return out, fmt.Errorf("poolfuzz: namespace must match campaign target (%s)", wantNS)
	}

	now := time.Now().Unix()
	const maxResearchEnergy = 64
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
		if energy > maxResearchEnergy {
			energy = maxResearchEnergy
		}
		// L2: Hunt coverage engines neutralize miner-supplied energy (scheduling bias).
		if isHunt && HuntEngineEnabled() && hunt.IsCoverageEngine(hunt.HuntEngineFromConfig(cfg)) {
			energy = 2
		}
		seeds = append(seeds, fuzzengine.PoolCorpusSeed{
			Input: u, InputBytes: append([]byte(nil), b...), Energy: energy, Edge: seed.Edge, Path: seed.Path,
		})
	}
	shareNS := sharesResearchNamespace(cfg)
	if shareNS {
		if err := s.UpsertNamespaceCorpusSeeds(ctx, ns, seeds, now); err != nil {
			return out, err
		}
	}
	out.SeedsAccepted = len(seeds)
	if isHunt {
		if err := s.upsertPoolCorpusSeedsBatch(ctx, campaignID, seeds, now); err != nil {
			return out, err
		}
	}
	// Claim wire reads campaign seed_byte_corpus (then the shared catalog cache).
	// Pool rows alone are dropped when claim-light omits corpus_seeds or
	// first-fit keeps boot starters. Inventory orders do not publish into that
	// cache: catalogSharedTarget is empty for them.
	l2Target := ""
	if isHunt {
		l2Target = catalogSharedTarget(cfg)
	}
	if err := s.persistAcceptedClaimSeeds(ctx, campaignID, l2Target, seeds); err != nil {
		return out, err
	}
	if isHunt {
		// Coverage-guided Hunt: prove edges via coordinator replay (never trust miner counts).
		if HuntEngineEnabled() && hunt.IsCoverageEngine(hunt.HuntEngineFromConfig(cfg)) {
			proven, perr := s.ProveHuntCorpusSeedEdges(ctx, campaignID, cfg, seeds, now)
			if perr != nil {
				return out, perr
			}
			eng := strings.TrimSpace(req.HuntEngine)
			if eng == "" {
				eng = hunt.HuntEngineFromConfig(cfg)
			}
			s.noteLeaseProvenPersist(ctx, campaignID, req.ItemID, workerID, len(seeds), proven, 0, eng, strings.TrimSpace(req.EngineVersion))
		}
	}

	seenCrashPay := map[string]struct{}{} // sha256 and/or stable crash bucket
	for _, crashB := range req.Crashes {
		if len(crashB) == 0 || len(crashB) > maxResearchSeedBytes {
			out.CrashesRejected++
			continue
		}
		if !isHunt {
			// Dig research-only: do not store unverified crash bytes into corpus.
			out.CrashesRejected++
			continue
		}
		ok, trap, err := researchCrashReplayer(ctx, s, campaignID, cfg, crashB)
		if err != nil || !ok {
			out.CrashesRejected++
			continue
		}
		u := fuzzengine.PackInputBytesToU64(crashB)
		sha := fuzzengine.InputBytesSHA256(crashB)
		bucket := fuzzengine.StableCrashBucket("native_crash", trap)
		// Serialize novelty check + finding insert so two concurrent leases cannot
		// both credit the same crash family (valax F5 race sibling of F1).
		lockErr := s.withCampaignSettle(campaignID, func() error {
			// Pay-credit only for campaign-novel crashes (exact bytes OR stable bucket).
			// Stale re-upload (Dicky) and crash-preserving mutations (valax) must not
			// unlock full PayRun. Corpus upsert still records the hit outside.
			novelPay := true
			if _, dup := seenCrashPay[sha]; dup {
				novelPay = false
			} else if bucket != "" && bucket != "crash|generic" {
				if _, dup := seenCrashPay["bucket:"+bucket]; dup {
					novelPay = false
				}
			}
			if novelPay {
				if known, kerr := s.campaignCrashInputKnown(ctx, campaignID, u, sha); kerr != nil {
					return kerr
				} else if known {
					novelPay = false
				}
			}
			if novelPay && bucket != "" && bucket != "crash|generic" {
				if known, kerr := s.campaignCrashBucketKnown(ctx, campaignID, bucket); kerr != nil {
					return kerr
				} else if known {
					novelPay = false
				}
			}
			if !novelPay {
				out.CrashesRejected++
				return nil
			}
			seenCrashPay[sha] = struct{}{}
			if bucket != "" && bucket != "crash|generic" {
				seenCrashPay["bucket:"+bucket] = struct{}{}
			}
			out.CrashesReplayOK++
			if HuntEngineEnabled() {
				eng := strings.TrimSpace(req.HuntEngine)
				if eng == "" {
					eng = hunt.HuntEngineFromConfig(cfg)
				}
				s.noteLeaseProvenPersist(ctx, campaignID, req.ItemID, workerID, 0, 0, 1, eng, strings.TrimSpace(req.EngineVersion))
			}
			info, _ := fuzzupstream.ParseHuntTrap(trap)
			if !info.Security {
				return nil
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
				return err
			}
			out.Findings++
			_ = s.observePoolCorpusNovelty(ctx, campaignID, u, crashB, true, now, true, true, true, "native_crash")
			return nil
		})
		if lockErr != nil {
			return out, lockErr
		}
		if err := s.upsertPoolCorpusSeed(ctx, campaignID, u, crashB, 12, 0, 0, true, now); err != nil {
			return out, err
		}
		if shareNS {
			_ = s.UpsertNamespaceCorpusSeeds(ctx, ns, []fuzzengine.PoolCorpusSeed{{
				Input: u, InputBytes: crashB, Energy: 8,
			}}, now)
		}
	}
	return out, nil
}

// persistAcceptedClaimSeeds writes accepted coverage units to the front of the
// campaign seed_byte_corpus (wire-capped) and into the L2 import cache so the
// next miner's claim ships them. Crash-only deltas have an empty seed list and
// do not touch the cache. Export failure is returned so the worker retries;
// the corpus upsert above is idempotent.
func (s *Service) persistAcceptedClaimSeeds(ctx context.Context, campaignID, l2Target string, seeds []fuzzengine.PoolCorpusSeed) error {
	if s == nil || s.DB == nil || len(seeds) == 0 {
		return nil
	}
	fresh := make([][]byte, 0, len(seeds))
	for _, seed := range seeds {
		if seed.Crash || len(seed.InputBytes) == 0 || genericBootSeed(seed.InputBytes) {
			continue
		}
		fresh = append(fresh, seed.InputBytes)
	}
	if len(fresh) == 0 {
		return nil
	}
	// Bound the JSON rewrite to the claim wire budget before hex-copying.
	fresh = capAcceptedSeedBytes(fresh)
	if len(fresh) == 0 {
		return nil
	}
	err := s.withCampaignSettle(campaignID, func() error {
		var cfgJSON string
		if err := s.DB.QueryRowContext(ctx, `SELECT config_json FROM fuzz_campaigns WHERE id=?`, campaignID).Scan(&cfgJSON); err != nil {
			return err
		}
		cfg := parseConfigJSON(cfgJSON)
		var existing []any
		switch raw := cfg["seed_byte_corpus"].(type) {
		case []any:
			existing = raw
		case []string:
			existing = make([]any, len(raw))
			for i, item := range raw {
				existing[i] = item
			}
		}
		cfg["seed_byte_corpus"] = MergeAcceptedSeedByteCorpus(fresh, existing)
		_, err := s.DB.ExecContext(ctx, `UPDATE fuzz_campaigns SET config_json=? WHERE id=?`, marshalConfigJSON(cfg), campaignID)
		return err
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(l2Target) == "" || strings.HasPrefix(l2Target, "dig:") {
		return nil
	}
	if _, err := hunt.ExportLibFuzzerSeeds(hunt.RepoRoot(), l2Target, fresh); err != nil {
		return fmt.Errorf("poolfuzz: export accepted seeds to L2: %w", err)
	}
	return nil
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
	// Native ASAN replay follows libFuzzer -max_len. The WASM 4096 ceil must not
	// cut length-confusion crashes before prove. This path is Hunt-only.
	maxB := 65536
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
