package chain

import (
	"math"
	"testing"
)

// Consensus fingerprint changes whenever DevFeeAddress or other locked economics constants change.
func TestLockedPolicyHash(t *testing.T) {
	const want = "6ac9493c36d60c29faddcc9b4b7860330a0d6ce3674719779df0ef09de2a8072"
	if got := lockedPolicyHash(); got != want {
		t.Fatalf("lockedPolicyHash: want %q got %q", want, got)
	}
}

func TestValidateLockedPolicy(t *testing.T) {
	if err := validateLockedPolicy(); err != nil {
		t.Fatal(err)
	}
}

func TestBaseRewardForBlockIndex(t *testing.T) {
	if got := BaseRewardForBlockIndex(0); got != 0 {
		t.Fatalf("genesis index: got %v", got)
	}
	if got := BaseRewardForBlockIndex(1); got != InitialBaseRewardHMC {
		t.Fatalf("first PoH block: got %v want %v", got, InitialBaseRewardHMC)
	}
	// Last block still in epoch 0 for halving math: (index-1)/interval == 0
	idx := RewardHalvingIntervalBlocks
	if got := BaseRewardForBlockIndex(idx); got != InitialBaseRewardHMC {
		t.Fatalf("last block epoch0 reward: got %v want %v", got, InitialBaseRewardHMC)
	}
	// First block of epoch 1
	idx = RewardHalvingIntervalBlocks + 1
	want := InitialBaseRewardHMC / 2
	if got := BaseRewardForBlockIndex(idx); math.Abs(got-want) > 1e-12 {
		t.Fatalf("first halved reward: got %v want %v", got, want)
	}
	// Floor: force tiny reward then clamp to tail
	veryHigh := RewardHalvingIntervalBlocks*64 + 1
	r := BaseRewardForBlockIndex(veryHigh)
	if r < RewardTailFloorHMC-1e-15 || r > RewardTailFloorHMC+1e-12 {
		t.Fatalf("expected tail floor %v, got %v at index %d", RewardTailFloorHMC, r, veryHigh)
	}
}

func TestNextHalvingBlock(t *testing.T) {
	if got := NextHalvingBlock(0); got != RewardHalvingIntervalBlocks+1 {
		t.Fatalf("from tip 0: got %d want %d", got, RewardHalvingIntervalBlocks+1)
	}
	if got := NextHalvingBlock(RewardHalvingIntervalBlocks); got != 2*RewardHalvingIntervalBlocks+1 {
		t.Fatalf("at epoch boundary: got %d", got)
	}
}

func TestExpectedEmptyMiningHMCPerHour(t *testing.T) {
	h := ExpectedEmptyMiningHMCPerHour(100)
	if h <= 0 {
		t.Fatalf("expected positive HMC/hour, got %v", h)
	}
	// 120 blocks/hour at 30s target × base reward at next block
	next := BaseRewardForBlockIndex(101)
	want := next * (3600.0 / float64(PoHRetargetTargetSec))
	if math.Abs(h-want) > 1e-9 {
		t.Fatalf("got %v want %v", h, want)
	}
}

func TestDifficultyFairnessFloor(t *testing.T) {
	// Fairness guard in order ingest: reward_hmc >= difficulty_score × RewardPerDifficultyUnit
	minFair := float64(MaxDifficultyScore) * RewardPerDifficultyUnit
	if minFair <= 0 || minFair > 1.0 {
		t.Fatalf("unexpected fairness bound: %v", minFair)
	}
	if MinDifficultyScore < 1 || MaxDifficultyScore < MinDifficultyScore {
		t.Fatal("difficulty bounds broken")
	}
}

func TestNetworkFeeSharesSumToOne(t *testing.T) {
	s := NetworkFeeBurnShare + NetworkFeeDevShare
	if math.Abs(s-1.0) > 1e-9 {
		t.Fatalf("burn+dev share = %v", s)
	}
}
