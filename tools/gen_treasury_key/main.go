// One-shot: prints a fresh Ed25519 seed (hex) and DevFeeAddress derived like node HMC- ids.
// go run ./tools/gen_treasury_key
package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
)

const (
	orderBurnRate                  = 0.10
	orderPlatformFeeRate           = 0.05
	networkFeeBurnShare            = 0.30
	networkFeeDevShare             = 0.70
	maxSupplyHMC                   = 100_000_000.0
	initialBaseRewardHMC           = 0.01
	rewardHalvingInterval   uint64 = 2_102_400
	rewardTailFloorHMC             = 0.002
	rewardPerDifficultyUnit        = 0.0005
	minDifficultyScore             = 1
	maxDifficultyScore             = 100
)

func hmcFromSeed(seed []byte) string {
	pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	sum := sha256.Sum256(pub)
	return "HMC-" + hex.EncodeToString(sum[:])[:16]
}

func policyHash(dev string) string {
	wire := "order_burn_rate=" + strconv.FormatFloat(orderBurnRate, 'f', 6, 64) + ";" +
		"order_platform_fee_rate=" + strconv.FormatFloat(orderPlatformFeeRate, 'f', 6, 64) + ";" +
		"network_fee_burn_share=" + strconv.FormatFloat(networkFeeBurnShare, 'f', 6, 64) + ";" +
		"network_fee_dev_share=" + strconv.FormatFloat(networkFeeDevShare, 'f', 6, 64) + ";" +
		"dev_fee_address=" + dev + ";" +
		"max_supply_hmc=" + strconv.FormatFloat(maxSupplyHMC, 'f', 6, 64) + ";" +
		"initial_base_reward_hmc=" + strconv.FormatFloat(initialBaseRewardHMC, 'f', 6, 64) + ";" +
		"halving_interval_blocks=" + strconv.FormatUint(rewardHalvingInterval, 10) + ";" +
		"reward_tail_floor_hmc=" + strconv.FormatFloat(rewardTailFloorHMC, 'f', 6, 64) + ";" +
		"reward_per_difficulty_unit=" + strconv.FormatFloat(rewardPerDifficultyUnit, 'f', 6, 64) + ";" +
		"difficulty_min=" + strconv.Itoa(minDifficultyScore) + ";" +
		"difficulty_max=" + strconv.Itoa(maxDifficultyScore)
	sum := sha256.Sum256([]byte(wire))
	return hex.EncodeToString(sum[:])
}

func main() {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		panic(err)
	}
	seed := priv.Seed()
	addr := hmcFromSeed(seed)
	fmt.Println("NEW_DEV_FEE_ADDRESS", addr)
	fmt.Println("NEW_TREASURY_SEED_HEX", hex.EncodeToString(seed))
	fmt.Println("NEW_POLICY_HASH", policyHash(addr))
}
