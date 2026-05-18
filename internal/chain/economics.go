package chain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	// MaxSupplyHMC is the hard cap for total minted HMC.
	MaxSupplyHMC = 100_000_000.0
	// InitialBaseRewardHMC is PoH reward per block when mining without a paid order.
	InitialBaseRewardHMC = 0.01
	// RewardHalvingIntervalBlocks defines epoch length for base PoH reward halvings.
	// At target 30s/block this is ~2 years: 2 * 365 * 24 * 120 = 2,102,400 blocks.
	RewardHalvingIntervalBlocks uint64 = 2_102_400
	// OrderBurnRate is the part of prepaid order escrow counted as burned supply.
	OrderBurnRate = 0.10
	// OrderPlatformFeeRate is additional platform fee on top of prepaid order escrow.
	// Example: reward_hmc * target_solves = 100 HMC -> +5 HMC platform fee.
	OrderPlatformFeeRate = 0.05
	// NetworkFeeBurnShare is the burned share of transfer fee_units.
	NetworkFeeBurnShare = 0.30
	// NetworkFeeDevShare is the platform share of transfer fee_units.
	NetworkFeeDevShare = 0.70
	// DevFeeAddress is a consensus-locked recipient for platform/service fee and genesis treasury mint.
	// To change this address, code changes and a coordinated network upgrade are required.
	// Pre-mainnet rotation (2026-05): genesis mint + fee shares go here; operator holds matching Ed25519 seed (see docs/POOL_FINAL_FREEZE.md).
	DevFeeAddress = "HMC-719006d93916ad52"
	// RewardPerDifficultyUnit is minimal fair reward per one difficulty score unit.
	RewardPerDifficultyUnit = 0.0005
	MinDifficultyScore      = 1
	MaxDifficultyScore      = 100
	// RewardTailFloorHMC documents the intended long-term floor for future reward schedule work.
	RewardTailFloorHMC = 0.002
)

type EconomicsSnapshot struct {
	MaxSupplyHMC  float64 `json:"max_supply_hmc"`
	TotalMinted   float64 `json:"total_minted_hmc"`
	TotalBurned   float64 `json:"total_burned_hmc"`
	Circulating   float64 `json:"circulating_hmc"`
	MintRemaining float64 `json:"mint_remaining_hmc"`
	BurnRateOrder float64 `json:"burn_rate_order"`
	OrderFeeRate  float64 `json:"order_fee_rate"`
	NetFeeBurn    float64 `json:"network_fee_burn_share"`
	NetFeeDev     float64 `json:"network_fee_dev_share"`
	DevFeeAddress string  `json:"dev_fee_address"`
	PolicyHash    string  `json:"policy_hash"`
}

func lockedPolicyHash() string {
	wire := "order_burn_rate=" + strconv.FormatFloat(OrderBurnRate, 'f', 6, 64) + ";" +
		"order_platform_fee_rate=" + strconv.FormatFloat(OrderPlatformFeeRate, 'f', 6, 64) + ";" +
		"network_fee_burn_share=" + strconv.FormatFloat(NetworkFeeBurnShare, 'f', 6, 64) + ";" +
		"network_fee_dev_share=" + strconv.FormatFloat(NetworkFeeDevShare, 'f', 6, 64) + ";" +
		"dev_fee_address=" + DevFeeAddress + ";" +
		"max_supply_hmc=" + strconv.FormatFloat(MaxSupplyHMC, 'f', 6, 64) + ";" +
		"initial_base_reward_hmc=" + strconv.FormatFloat(InitialBaseRewardHMC, 'f', 6, 64) + ";" +
		"halving_interval_blocks=" + strconv.FormatUint(RewardHalvingIntervalBlocks, 10) + ";" +
		"reward_tail_floor_hmc=" + strconv.FormatFloat(RewardTailFloorHMC, 'f', 6, 64) + ";" +
		"reward_per_difficulty_unit=" + strconv.FormatFloat(RewardPerDifficultyUnit, 'f', 6, 64) + ";" +
		"difficulty_min=" + strconv.Itoa(MinDifficultyScore) + ";" +
		"difficulty_max=" + strconv.Itoa(MaxDifficultyScore)
	sum := sha256.Sum256([]byte(wire))
	return hex.EncodeToString(sum[:])
}

func validateLockedPolicy() error {
	if DevFeeAddress == "" {
		return fmt.Errorf("chain policy invalid: empty DevFeeAddress")
	}
	if NetworkFeeBurnShare < 0 || NetworkFeeDevShare < 0 {
		return fmt.Errorf("chain policy invalid: negative network fee shares")
	}
	if OrderBurnRate < 0 || OrderPlatformFeeRate < 0 {
		return fmt.Errorf("chain policy invalid: negative order rates")
	}
	if !strings.HasPrefix(DevFeeAddress, "HMC-") || len(DevFeeAddress) != 20 {
		return fmt.Errorf("chain policy invalid: dev fee address must be HMC- + 16 hex")
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(DevFeeAddress, "HMC-")); err != nil {
		return fmt.Errorf("chain policy invalid: dev fee address hex malformed")
	}
	sum := NetworkFeeBurnShare + NetworkFeeDevShare
	if sum < 0.999999 || sum > 1.000001 {
		return fmt.Errorf("chain policy invalid: network fee shares must sum to 1, got %f", sum)
	}
	if MaxSupplyHMC <= 0 {
		return fmt.Errorf("chain policy invalid: max supply must be positive")
	}
	if RewardTailFloorHMC < 0 || InitialBaseRewardHMC < 0 {
		return fmt.Errorf("chain policy invalid: negative reward configuration")
	}
	if MinDifficultyScore <= 0 || MaxDifficultyScore < MinDifficultyScore {
		return fmt.Errorf("chain policy invalid: invalid difficulty bounds")
	}
	return nil
}

// BaseRewardForBlockIndex returns scheduled base PoH reward for given block index.
// Index 0 (genesis) is not a PoH block and returns 0.
func BaseRewardForBlockIndex(index uint64) float64 {
	if index == 0 {
		return 0
	}
	if RewardHalvingIntervalBlocks == 0 {
		return InitialBaseRewardHMC
	}
	epoch := float64((index - 1) / RewardHalvingIntervalBlocks)
	reward := InitialBaseRewardHMC / math.Pow(2, epoch)
	if reward < RewardTailFloorHMC {
		reward = RewardTailFloorHMC
	}
	return reward
}

// NextHalvingBlock returns the first block index of the next halving epoch.
func NextHalvingBlock(currentTipHeight uint64) uint64 {
	if RewardHalvingIntervalBlocks == 0 {
		return 0
	}
	curEpoch := currentTipHeight / RewardHalvingIntervalBlocks
	return (curEpoch+1)*RewardHalvingIntervalBlocks + 1
}

// ExpectedEmptyMiningHMCPerHour estimates base-reward-only earnings at target block time.
func ExpectedEmptyMiningHMCPerHour(currentTipHeight uint64) float64 {
	rewardNext := BaseRewardForBlockIndex(currentTipHeight + 1)
	blocksPerHour := 3600.0 / float64(PoHRetargetTargetSec)
	return rewardNext * blocksPerHour
}
