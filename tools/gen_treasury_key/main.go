// One-shot: generate a fresh Ed25519 treasury seed into a 0600 file (never print seed).
//
//	go run ./tools/gen_treasury_key
//	go run ./tools/gen_treasury_key -out /path/to/seed.hex
package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
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
	out := flag.String("out", filepath.Join(".secrets", "hackme_treasury_ed25519_seed.hex"),
		"path to write 64-hex seed (0600); seed is never printed")
	flag.Parse()

	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		panic(err)
	}
	seed := priv.Seed()
	addr := hmcFromSeed(seed)

	if err := os.MkdirAll(filepath.Dir(*out), 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, []byte(hex.EncodeToString(seed)+"\n"), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "write seed file: %v\n", err)
		os.Exit(1)
	}
	// Re-assert mode in case umask widened WriteFile perms.
	_ = os.Chmod(*out, 0o600)

	fmt.Println("NEW_DEV_FEE_ADDRESS", addr)
	fmt.Println("NEW_TREASURY_SEED_FILE", *out)
	fmt.Println("NEW_POLICY_HASH", policyHash(addr))
}
