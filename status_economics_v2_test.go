package main

import (
	"testing"

	"hackme/internal/chain"
)

func TestEconomicsV2StatusPayloadScheduledDefaults(t *testing.T) {
	t.Setenv("HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT", "")
	t.Setenv("HACKME_ECONOMICS_V2_ANNOUNCE_UNIX", "")
	chain.SetV2CompiledDefaultsOffForTest(false)
	t.Cleanup(func() { chain.SetV2CompiledDefaultsOffForTest(false) })

	p := economicsV2StatusPayload(chain.DefaultEconomicsV2ActivationHeight)
	if got, ok := p["activation_height"].(uint64); !ok || got != chain.DefaultEconomicsV2ActivationHeight {
		t.Fatalf("activation_height = %v, want compiled default %d", p["activation_height"], chain.DefaultEconomicsV2ActivationHeight)
	}
	if active, ok := p["active_at_tip"].(bool); !ok || !active {
		t.Fatalf("active_at_tip = %v, want true at the activation height", p["active_at_tip"])
	}
	if got, ok := p["announce_unix"].(int64); !ok || got != 0 {
		t.Fatalf("announce_unix = %v, want 0 while unset", p["announce_unix"])
	}
	if got := p["order_burn_rate"].(float64); got != chain.V2OrderBurnRate {
		t.Fatalf("order_burn_rate = %v, want %v", got, chain.V2OrderBurnRate)
	}
	if got := p["order_platform_fee_rate"].(float64); got != chain.V2OrderPlatformFeeRate {
		t.Fatalf("order_platform_fee_rate = %v, want %v", got, chain.V2OrderPlatformFeeRate)
	}
	if got := p["reward_halving_interval_blocks"].(uint64); got != chain.V2RewardHalvingIntervalBlocks {
		t.Fatalf("reward_halving_interval_blocks = %v, want %v", got, chain.V2RewardHalvingIntervalBlocks)
	}

	before := economicsV2StatusPayload(chain.DefaultEconomicsV2ActivationHeight - 1)
	if before["active_at_tip"] != false {
		t.Fatalf("active_at_tip = %v, want false one block before activation", before["active_at_tip"])
	}
}

func TestEconomicsV2StatusPayloadNotScheduled(t *testing.T) {
	t.Setenv("HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT", "")
	t.Setenv("HACKME_ECONOMICS_V2_ANNOUNCE_UNIX", "")
	chain.SetV2CompiledDefaultsOffForTest(true)
	t.Cleanup(func() { chain.SetV2CompiledDefaultsOffForTest(false) })

	p := economicsV2StatusPayload(42)
	if got, ok := p["activation_height"].(uint64); !ok || got != 0 {
		t.Fatalf("activation_height = %v, want 0 when v2 is not scheduled", p["activation_height"])
	}
	if p["active_at_tip"] != false {
		t.Fatalf("active_at_tip = %v, want false when v2 is not scheduled", p["active_at_tip"])
	}
	if got, ok := p["announce_unix"].(int64); !ok || got != 0 {
		t.Fatalf("announce_unix = %v, want 0 when v2 is not scheduled", p["announce_unix"])
	}
	// Schedule constants stay published even when the switch is off: they describe
	// the announced plan, not the active state.
	if got := p["initial_base_reward_hmc"].(float64); got != chain.V2InitialBaseRewardHMC {
		t.Fatalf("initial_base_reward_hmc = %v, want %v", got, chain.V2InitialBaseRewardHMC)
	}
}
