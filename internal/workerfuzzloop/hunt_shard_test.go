package workerfuzzloop

import (
	"bytes"
	"testing"

	"hackme/internal/fuzzengine"
	"hackme/internal/hunt"
)

func TestIsHuntClaim(t *testing.T) {
	if !IsHuntClaim(ClaimResp{TaskClass: "hunt"}) {
		t.Fatal("task_class hunt")
	}
	if !IsHuntClaim(ClaimResp{WorkKind: "hunt_shard"}) {
		t.Fatal("work_kind hunt_shard")
	}
	if IsHuntClaim(ClaimResp{TaskClass: "fuzz"}) {
		t.Fatal("fuzz is not hunt")
	}
}

func TestHuntClaimMissingFields(t *testing.T) {
	if HuntClaimMissingFields(ClaimResp{UpstreamTargetID: "x", InputBytesHex: "ab"}) != nil {
		t.Fatal("complete claim")
	}
	if HuntClaimMissingFields(ClaimResp{InputBytesHex: "ab"}) == nil {
		t.Fatal("missing target")
	}
}

func TestHuntShardConfigFromClaim(t *testing.T) {
	cfg := huntShardConfigFromClaim(ClaimResp{
		UpstreamTargetID: "jsmn",
		MaxInputBytes:    128,
		ExecPerUnit:      8,
		DepthTier:        "oss_cve",
	}, true)
	if cfg["upstream_target_id"] != "jsmn" || cfg["iterations_per_shard"] != 8 {
		t.Fatalf("cfg=%v", cfg)
	}
}

func TestHuntShardConfigCarriesMutationScheduling(t *testing.T) {
	cfg := huntShardConfigFromClaim(ClaimResp{
		UpstreamTargetID: "jsmn",
		MaxInputBytes:    128,
		ExecPerUnit:      8,
		DepthTier:        "oss_cve",
		PowerMutCap:      14,
		HavocDeepV28:     true,
		HavocDeepV210:    true,
	}, true)
	if got := fuzzengine.PowerMutCap(cfg); got != 14 {
		t.Fatalf("power_mut_cap: want 14 got %d", got)
	}
	if !fuzzengine.DeepHavocV28(cfg) {
		t.Fatal("claim with havoc_deep_v28 must enable the deep stack")
	}
	if !fuzzengine.DeepHavocV210(cfg) {
		t.Fatal("claim with havoc_deep_v210 must enable the v2.10 burst")
	}

	legacy := huntShardConfigFromClaim(ClaimResp{
		UpstreamTargetID: "jsmn",
		MaxInputBytes:    128,
		ExecPerUnit:      8,
		DepthTier:        "oss_cve",
	}, true)
	if fuzzengine.DeepHavocV28(legacy) {
		t.Fatal("claims without havoc_deep_v28 must keep legacy replay identity")
	}
	if fuzzengine.DeepHavocV210(legacy) {
		t.Fatal("claims without havoc_deep_v210 must not enable burst")
	}
	if got := fuzzengine.PowerMutCap(legacy); got != 12 {
		t.Fatalf("legacy power_mut_cap default: want 12 got %d", got)
	}
}

func TestHuntClaimConfigMatchesCampaignExecInputsV210(t *testing.T) {
	seeds := []fuzzengine.PoolCorpusSeed{
		{InputBytes: []byte(`{"a":1}`), Energy: 2},
		{InputBytes: []byte(`{"b":[1,2,3]}`), Energy: 3},
	}
	campaign := campaignCfgFixture()
	campaign["havoc_deep_v210"] = true
	claim := huntShardConfigFromClaim(ClaimResp{
		UpstreamTargetID: "jsmn",
		MaxInputBytes:    128,
		ExecPerUnit:      8,
		DepthTier:        "oss_cve",
		CoverageKind:     "hunt_corpus_guided",
		PowerMutCap:      14,
		HavocDeepV28:     true,
		HavocDeepV210:    true,
	}, true)
	for inputN := uint64(0); inputN < 4; inputN++ {
		for exec := uint64(0); exec < 4; exec++ {
			want := hunt.ShardSegmentExecInput("camp-v210", inputN, exec, campaign, seeds)
			got := hunt.ShardSegmentExecInput("camp-v210", inputN, exec, claim, seeds)
			if string(want) != string(got) {
				t.Fatalf("v210 inputN=%d exec=%d: worker/replay diverged", inputN, exec)
			}
		}
	}
}

// campaignCfgFixture mirrors what CampaignConfig persists for a JSON catalog
// target running hunt_standard under rc17.2: power schedule, deep-havoc opt-in
// and the target's static mutator dict.
func campaignCfgFixture() map[string]any {
	return map[string]any{
		"upstream_target_id":    "jsmn",
		"max_input_bytes":       128,
		"input_mode":            "bytes",
		"iterations_per_shard":  8,
		"depth_tier":            "oss_cve",
		"hunt_corpus_guided":    true,
		"guided_scheduling":     true,
		"coverage_feedback_v1":  true,
		"corpus_explore_v2":     true,
		"hunt_segment_mutating": true,
		"power_mut_cap":         14,
		"havoc_deep_v28":        true,
		"mutator_dict":          hunt.MutatorDictForTarget("jsmn"),
		"hunt_mutator_profile":  "json",
	}
}

func claimForCampaign() ClaimResp {
	return ClaimResp{
		UpstreamTargetID: "jsmn",
		MaxInputBytes:    128,
		ExecPerUnit:      8,
		DepthTier:        "oss_cve",
		CoverageKind:     "hunt_corpus_guided",
		PowerMutCap:      14,
		HavocDeepV28:     true,
	}
}

// The claim-rebuilt config must resolve to the same mutation inputs as the
// persisted campaign config: same power cap, same deep-havoc opt-in, and the
// same effective mutator dict (campaign static dict merged with the same
// corpus autodict tokens). Asserting the dict bytes pins the root cause
// directly - exec-input equality alone can mask a dict difference whenever the
// scheduled stages happen to avoid dict-aware operators.
func TestHuntClaimConfigMatchesCampaignMutator(t *testing.T) {
	corpus := [][]byte{
		[]byte(`{"a":1}`),
		[]byte(`{"b":[1,2,3]}`),
		[]byte(`{"c":"x"}`),
	}
	claim := huntShardConfigFromClaim(claimForCampaign(), true)
	if got := fuzzengine.PowerMutCap(claim); got != 14 {
		t.Fatalf("power_mut_cap: want 14 got %d", got)
	}
	if !fuzzengine.DeepHavocV28(claim) {
		t.Fatal("claim config must opt into deep v2.8 like the campaign")
	}
	want := fuzzengine.EffectiveMutatorDict(campaignCfgFixture(), corpus)
	got := fuzzengine.EffectiveMutatorDict(claim, corpus)
	if !bytes.Equal(want, got) {
		t.Fatalf("effective mutator dict diverged: campaign %d bytes vs claim %d bytes", len(want), len(got))
	}
}

// The worker derives every exec input from the claim-reconstructed config while
// the coordinator's verification replay uses the persisted campaign config; both
// must produce identical bytes for the same (campaign, inputN, execIdx, seeds).
func TestHuntClaimConfigMatchesCampaignExecInputs(t *testing.T) {
	seeds := []fuzzengine.PoolCorpusSeed{
		{InputBytes: []byte(`{"a":1}`), Energy: 2},
		{InputBytes: []byte(`{"b":[1,2,3]}`), Energy: 3},
		{InputBytes: []byte(`{"c":"x"}`), Energy: 1},
	}
	campaign := campaignCfgFixture()
	claim := huntShardConfigFromClaim(claimForCampaign(), true)
	for inputN := uint64(0); inputN < 8; inputN++ {
		for exec := uint64(0); exec < 8; exec++ {
			want := hunt.ShardSegmentExecInput("camp-x", inputN, exec, campaign, seeds)
			got := hunt.ShardSegmentExecInput("camp-x", inputN, exec, claim, seeds)
			if string(want) != string(got) {
				t.Fatalf("inputN=%d exec=%d: worker and replay inputs diverged (%d vs %d bytes)", inputN, exec, len(want), len(got))
			}
		}
	}
}
