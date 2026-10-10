package poolfuzz

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/sandbox"
)

// Dig canary / challenge shards: coordinator occasionally locks a known detector-hit
// input so a worker that returns CLEAN without hitting it is rejected (canary_miss).
// Honest workers that report the crash/finding take the normal full-replay path.
//
// Env:
//
//	HACKME_POOL_CANARY_PCT   percent of Dig claims to mark as canary (default 2; 0=off)
//	HACKME_POOL_CANARY       0/false/off disables even when pct>0

func canaryEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_POOL_CANARY")))
	if v == "" {
		return true
	}
	switch v {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func canaryPct() int {
	if !canaryEnabled() {
		return 0
	}
	return envIntClamped("HACKME_POOL_CANARY_PCT", 2, 0, 100)
}

// digCanaryPayload returns a pack-known detector-hit input, or nil if unavailable.
func digCanaryPayload(cfg map[string]any) (u64 uint64, bytesIn []byte, ok bool) {
	pack := strings.TrimSpace(strings.ToLower(jsonString(cfg["guard_pack"])))
	if pack == "" {
		pack = strings.TrimSpace(strings.ToLower(jsonString(cfg["guard_name"])))
	}
	switch pack {
	case "secrets":
		b := []byte("AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE")
		return fuzzengine.PackInputBytesToU64(b), b, true
	case "filter_utf8":
		b := []byte{0xc7, '='}
		return fuzzengine.PackInputBytesToU64(b), b, true
	case "script_bounds":
		u := uint64(0x4c | (521 << 8))
		return u, nil, true
	case "parser_expat":
		b := []byte("<root><child")
		return fuzzengine.PackInputBytesToU64(b), b, true
	default:
		return 0, nil, false
	}
}

func shouldInjectDigCanary(campaignID string, itemID int64, cfg map[string]any) bool {
	if IsHuntCampaign(cfg) || !poolDistributed(cfg) {
		return false
	}
	// Canary locks a known-hit seed into the guided corpus so exec 0 hits it.
	if !fuzzengine.GuidedSchedulingEnabled(cfg) {
		return false
	}
	pct := canaryPct()
	if pct <= 0 {
		return false
	}
	_, _, ok := digCanaryPayload(cfg)
	if !ok {
		return false
	}
	return replaySampleHit(campaignID, itemID, pct)
}

func (s *Service) markWorkCanary(ctx context.Context, campaignID string, itemID int64) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("poolfuzz: no database")
	}
	_, err := s.DB.ExecContext(ctx,
		`UPDATE fuzz_work_items SET is_canary=1, updated_at=? WHERE id=? AND campaign_id=?`,
		time.Now().Unix(), itemID, campaignID)
	return err
}

func (s *Service) workItemIsCanary(ctx context.Context, campaignID string, itemID int64) (bool, error) {
	if s == nil || s.DB == nil {
		return false, nil
	}
	var n int
	err := s.DB.QueryRowContext(ctx,
		`SELECT COALESCE(is_canary,0) FROM fuzz_work_items WHERE id=? AND campaign_id=?`,
		itemID, campaignID).Scan(&n)
	if err != nil {
		// Column may be missing on ancient DBs mid-migrate — treat as non-canary.
		if strings.Contains(strings.ToLower(err.Error()), "no such column") {
			return false, nil
		}
		return false, err
	}
	return n != 0, nil
}

// applyDigCanary locks a known-hit input onto the claim so honest workers report it.
func (s *Service) applyDigCanary(ctx context.Context, work *ClaimedWork, cfg map[string]any) error {
	if work == nil {
		return fmt.Errorf("poolfuzz: nil work")
	}
	u, b, ok := digCanaryPayload(cfg)
	if !ok {
		return fmt.Errorf("poolfuzz: no canary payload")
	}
	if err := s.storeExpectedInputs(ctx, work.CampaignID, work.ItemID, u, b); err != nil {
		return err
	}
	work.ActualInput = u
	work.InputBytes = append([]byte(nil), b...)
	if fuzzengine.GuidedSchedulingEnabled(cfg) || len(work.CorpusSeeds) > 0 {
		seed := fuzzengine.PoolCorpusSeed{Input: u, InputBytes: append([]byte(nil), b...), Energy: 1000}
		seeds := append([]fuzzengine.PoolCorpusSeed{seed}, work.CorpusSeeds...)
		if err := s.storeCorpusSnapshot(ctx, work.CampaignID, work.ItemID, seeds); err != nil {
			return err
		}
		work.CorpusSeeds = seeds
		if _, sha, err := fuzzengine.EncodeCorpusSnapshot(seeds); err == nil {
			work.CorpusSnapshotSHA256 = sha
		}
	}
	if err := s.markWorkCanary(ctx, work.CampaignID, work.ItemID); err != nil {
		return err
	}
	work.IsCanary = true
	return nil
}

// rejectCanaryMissIfNeeded rejects CLEAN hygiene when the locked canary input is a real hit.
func (s *Service) rejectCanaryMissIfNeeded(ctx context.Context, cfg map[string]any, req SubmitRequest, expectedU uint64, expectedB []byte) error {
	isCanary, err := s.workItemIsCanary(ctx, req.CampaignID, req.ItemID)
	if err != nil || !isCanary {
		return err
	}
	// Worker already claimed a finding/crash — handled on full-replay path.
	if submitClaimsFinding(fuzzengine.ParseCheckSemantics(cfg), req) {
		return nil
	}
	wasmHex := wasmHexFromConfig(cfg)
	if wasmHex == "" {
		return nil
	}
	raw, err := hex.DecodeString(wasmHex)
	if err != nil || len(raw) == 0 {
		return nil
	}
	sem := fuzzengine.ParseCheckSemantics(cfg)
	var check int32
	var execErr error
	if len(expectedB) > 0 {
		out, err := sandbox.InvokeCheckOutcomeInput(ctx, raw, expectedB)
		execErr = err
		if err == nil && out.OK {
			check = 1
		}
	} else {
		out, err := sandbox.InvokeCheckOutcome(ctx, raw, expectedU)
		execErr = err
		if err == nil && out.OK {
			check = 1
		}
	}
	_, record := fuzzengine.EvalCheck(sem, check, execErr)
	if record {
		return fmt.Errorf("poolfuzz: canary_miss: CLEAN submit missed known challenge input")
	}
	return nil
}
