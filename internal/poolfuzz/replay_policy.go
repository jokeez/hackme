package poolfuzz

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
	"strings"

	"hackme/internal/fuzzengine"
	"hackme/internal/fuzzingcli"
)

// Replay decision reasons (observability / SubmitOutcome.ReplayStatus).
const (
	replayReasonFull         = "full"
	replayReasonCrashClaim   = "crash_claim"
	replayReasonFindingClaim = "finding_claim"
	replayReasonSample       = "sample"
	replayReasonHygieneSkip  = "hygiene_skip"
	replayReasonAlwaysEnv    = "always_env"
	replayReasonSingleExec   = "single_exec"
	replayReasonHuntDefault  = "hunt_full"
)

// poolReplayDecision says whether Dig/Hunt submit must run coordinator segment replay.
type poolReplayDecision struct {
	Full   bool
	Reason string
}

func envTruthyDefaultOn(key string) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return true
	}
	switch strings.ToLower(v) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func envIntClamped(key string, def, min, max int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

// digPackageKey returns scan|audit|deep for Dig campaigns (best-effort).
func digPackageKey(cfg map[string]any) string {
	if cfg == nil {
		return "audit"
	}
	pkg := strings.TrimSpace(strings.ToLower(jsonString(cfg["dig_package"])))
	switch pkg {
	case "scan", "starter":
		return "scan"
	case "audit", "pro":
		return "audit"
	case "deep", "enterprise":
		return "deep"
	}
	return fuzzingcli.DigPackageFromDepthTier(fuzzengine.ParseDepthTier(cfg))
}

// poolReplaySamplePct is the % of clean Dig submits that still get full segment replay.
// Defaults: scan 1 · audit 5 (HACKME_POOL_REPLAY_SAMPLE_PCT) · deep 10 · hunt 100.
func poolReplaySamplePct(cfg map[string]any) int {
	if IsHuntCampaign(cfg) {
		return envIntClamped("HACKME_POOL_REPLAY_SAMPLE_PCT_HUNT", 100, 0, 100)
	}
	switch digPackageKey(cfg) {
	case "scan":
		return envIntClamped("HACKME_POOL_REPLAY_SAMPLE_PCT_SCAN", 1, 0, 100)
	case "deep":
		return envIntClamped("HACKME_POOL_REPLAY_SAMPLE_PCT_DEEP", 10, 0, 100)
	default: // audit
		return envIntClamped("HACKME_POOL_REPLAY_SAMPLE_PCT", 5, 0, 100)
	}
}

// poolReplayCrashAlways is true unless HACKME_POOL_REPLAY_CRASH_ALWAYS is explicitly off.
func poolReplayCrashAlways() bool {
	return envTruthyDefaultOn("HACKME_POOL_REPLAY_CRASH_ALWAYS")
}

// poolFullReplayForced disables hygiene skip (ops rollback / A/B).
func poolFullReplayForced() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_POOL_FULL_REPLAY")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// submitClaimsFinding reports whether the worker asserted a crash/finding that
// must be coordinator-proved (cannot mint bounty via hygiene skip).
func submitClaimsFinding(sem fuzzengine.CheckSemantics, req SubmitRequest) bool {
	if strings.TrimSpace(req.Trap) != "" {
		return true
	}
	_, record := fuzzengine.EvalCheck(sem, req.CheckResult, nil)
	return record
}

// replaySampleHit is deterministic under HACKME_POOL_REPLAY_SAMPLE_SEED (tests pin seed).
func replaySampleHit(campaignID string, itemID int64, pct int) bool {
	if pct <= 0 {
		return false
	}
	if pct >= 100 {
		return true
	}
	seed := strings.TrimSpace(os.Getenv("HACKME_POOL_REPLAY_SAMPLE_SEED"))
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s", strings.TrimSpace(campaignID), itemID, seed)))
	n := binary.BigEndian.Uint32(sum[:4]) % 100
	return int(n) < pct
}

// decidePoolReplay chooses full segment replay vs clean hygiene accept.
// Hygiene skip never records findings — forging found/crash always forces Full.
func decidePoolReplay(cfg map[string]any, sem fuzzengine.CheckSemantics, req SubmitRequest, execPer int) poolReplayDecision {
	if !poolDistributed(cfg) {
		return poolReplayDecision{Full: true, Reason: replayReasonFull}
	}
	if poolFullReplayForced() {
		return poolReplayDecision{Full: true, Reason: replayReasonAlwaysEnv}
	}
	// Single-exec Dig is cheap; keep full verify.
	if execPer <= 1 && !IsHuntCampaign(cfg) {
		return poolReplayDecision{Full: true, Reason: replayReasonSingleExec}
	}
	// Hunt defaults to full/async verify (sample pct default 100).
	if IsHuntCampaign(cfg) {
		pct := poolReplaySamplePct(cfg)
		if submitClaimsFinding(sem, req) && poolReplayCrashAlways() {
			return poolReplayDecision{Full: true, Reason: replayReasonCrashClaim}
		}
		if submitClaimsFinding(sem, req) {
			return poolReplayDecision{Full: true, Reason: replayReasonFindingClaim}
		}
		if replaySampleHit(req.CampaignID, req.ItemID, pct) {
			return poolReplayDecision{Full: true, Reason: replayReasonSample}
		}
		if pct >= 100 {
			return poolReplayDecision{Full: true, Reason: replayReasonHuntDefault}
		}
		return poolReplayDecision{Full: false, Reason: replayReasonHygieneSkip}
	}
	// Crash/found/sanitizer claims always require coordinator prove — hygiene skip
	// must never mint findings (HACKME_POOL_REPLAY_CRASH_ALWAYS documents this; default ON).
	if submitClaimsFinding(sem, req) {
		if strings.TrimSpace(req.Trap) != "" {
			return poolReplayDecision{Full: true, Reason: replayReasonCrashClaim}
		}
		return poolReplayDecision{Full: true, Reason: replayReasonFindingClaim}
	}
	pct := poolReplaySamplePct(cfg)
	if replaySampleHit(req.CampaignID, req.ItemID, pct) {
		return poolReplayDecision{Full: true, Reason: replayReasonSample}
	}
	return poolReplayDecision{Full: false, Reason: replayReasonHygieneSkip}
}
