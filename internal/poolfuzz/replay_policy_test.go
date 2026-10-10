package poolfuzz

import (
	"testing"

	"hackme/internal/fuzzengine"
)

func TestReplaySampleHitDeterministic(t *testing.T) {
	t.Setenv("HACKME_POOL_REPLAY_SAMPLE_SEED", "unit-test-seed")
	a := replaySampleHit("camp-a", 7, 5)
	b := replaySampleHit("camp-a", 7, 5)
	if a != b {
		t.Fatal("sample hit must be deterministic under fixed seed")
	}
	if replaySampleHit("camp-a", 7, 0) {
		t.Fatal("pct=0 must never hit")
	}
	if !replaySampleHit("camp-a", 7, 100) {
		t.Fatal("pct=100 must always hit")
	}
}

func TestPoolReplaySamplePctPackages(t *testing.T) {
	t.Setenv("HACKME_POOL_REPLAY_SAMPLE_PCT", "")
	t.Setenv("HACKME_POOL_REPLAY_SAMPLE_PCT_SCAN", "")
	t.Setenv("HACKME_POOL_REPLAY_SAMPLE_PCT_DEEP", "")
	t.Setenv("HACKME_POOL_REPLAY_SAMPLE_PCT_HUNT", "")
	if got := poolReplaySamplePct(map[string]any{"dig_package": "scan"}); got != 1 {
		t.Fatalf("scan pct=%d want 1", got)
	}
	if got := poolReplaySamplePct(map[string]any{"dig_package": "audit"}); got != 5 {
		t.Fatalf("audit pct=%d want 5", got)
	}
	if got := poolReplaySamplePct(map[string]any{"dig_package": "deep"}); got != 10 {
		t.Fatalf("deep pct=%d want 10", got)
	}
	if got := poolReplaySamplePct(map[string]any{"work_kind": "hunt_shard"}); got != 100 {
		t.Fatalf("hunt pct=%d want 100", got)
	}
}

func TestDecidePoolReplayCleanHygieneSkip(t *testing.T) {
	t.Setenv("HACKME_POOL_FULL_REPLAY", "")
	t.Setenv("HACKME_POOL_REPLAY_SAMPLE_PCT", "0")
	t.Setenv("HACKME_POOL_REPLAY_SAMPLE_SEED", "x")
	cfg := map[string]any{
		"pool_distributed": true,
		"dig_package":      "audit",
		"exec_per_unit":    64,
	}
	sem := fuzzengine.SemanticsDetector
	req := SubmitRequest{CampaignID: "c1", ItemID: 1, CheckResult: 0}
	dec := decidePoolReplay(cfg, sem, req, 64)
	if dec.Full || dec.Reason != replayReasonHygieneSkip {
		t.Fatalf("expected hygiene skip, got full=%v reason=%q", dec.Full, dec.Reason)
	}
}

func TestDecidePoolReplayFindingClaimForcesFull(t *testing.T) {
	t.Setenv("HACKME_POOL_FULL_REPLAY", "")
	t.Setenv("HACKME_POOL_REPLAY_SAMPLE_PCT", "0")
	cfg := map[string]any{
		"pool_distributed": true,
		"dig_package":      "audit",
		"exec_per_unit":    64,
	}
	sem := fuzzengine.SemanticsDetector
	req := SubmitRequest{CampaignID: "c1", ItemID: 1, CheckResult: 1}
	dec := decidePoolReplay(cfg, sem, req, 64)
	if !dec.Full || dec.Reason != replayReasonFindingClaim {
		t.Fatalf("finding claim must force replay, got full=%v reason=%q", dec.Full, dec.Reason)
	}
	req2 := SubmitRequest{CampaignID: "c1", ItemID: 1, CheckResult: 0, Trap: "wasm trap: oob"}
	dec2 := decidePoolReplay(cfg, sem, req2, 64)
	if !dec2.Full || dec2.Reason != replayReasonCrashClaim {
		t.Fatalf("trap claim must force replay, got full=%v reason=%q", dec2.Full, dec2.Reason)
	}
}

func TestDecidePoolReplaySamplePath(t *testing.T) {
	t.Setenv("HACKME_POOL_FULL_REPLAY", "")
	t.Setenv("HACKME_POOL_REPLAY_SAMPLE_PCT", "100")
	cfg := map[string]any{
		"pool_distributed": true,
		"dig_package":      "audit",
		"exec_per_unit":    8,
	}
	sem := fuzzengine.SemanticsDetector
	req := SubmitRequest{CampaignID: "c1", ItemID: 9, CheckResult: 0}
	dec := decidePoolReplay(cfg, sem, req, 8)
	if !dec.Full || dec.Reason != replayReasonSample {
		t.Fatalf("pct=100 clean must sample-replay, got full=%v reason=%q", dec.Full, dec.Reason)
	}
}
