package chain

import (
	"context"
	"errors"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
)

// Economics v2 (RFC_ECONOMICS_V2.md D9) — height-gated; unset/0 = never activate.
const (
	// V2InitialBaseRewardHMC is empty PoH reward after Economics v2 activation.
	V2InitialBaseRewardHMC = 0.1
	// V2RewardHalvingIntervalBlocks is ~4 years at 30s/block.
	V2RewardHalvingIntervalBlocks uint64 = 4_204_800
	// V2OrderBurnRate / V2OrderPlatformFeeRate: 10% upfront as 5%/5%.
	V2OrderBurnRate        = 0.05
	V2OrderPlatformFeeRate = 0.05
	// V2SUPInitialReward is SUP per empty PoH block after activation (no tail).
	V2SUPInitialReward = 0.002
	// V2SUPMaxSupply is the published hard cap for v2 SUP emission (RFC D4 locked).
	V2SUPMaxSupply = 21_000.0
	// V2ActivationBufferHMC is the public start-buffer size for the activation announce (D3 locked).
	V2ActivationBufferHMC = 2000.0
	// V2DigBurnSoftStartCalendarDays is Dig-only burn soft-start after activation wall-clock (D6 locked).
	V2DigBurnSoftStartCalendarDays = 30
)

var (
	econV2HeightOnce sync.Once
	econV2Height     uint64 // 0 = disabled
)

// EconomicsV2ActivationHeight is the first block index that uses the v2 schedule.
// Lab/ops: HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT=<uint64> (must be ≥ 1).
// Unset or 0 ⇒ v1 forever (soft-announce / soak; no vanity default height).
func EconomicsV2ActivationHeight() uint64 {
	econV2HeightOnce.Do(func() {
		econV2Height = 0
		if v := strings.TrimSpace(os.Getenv("HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT")); v != "" {
			if n, err := strconv.ParseUint(v, 10, 64); err == nil && n >= 1 {
				econV2Height = n
			}
		}
	})
	return econV2Height
}

// ResetEconomicsV2ActivationHeightForTest clears the cached env height (tests only).
func ResetEconomicsV2ActivationHeightForTest() {
	econV2HeightOnce = sync.Once{}
	econV2Height = 0
}

// EconomicsV2ActiveForIndex reports whether block index uses the v2 schedule.
func EconomicsV2ActiveForIndex(index uint64) bool {
	act := EconomicsV2ActivationHeight()
	return act >= 1 && index >= 1 && index >= act
}

// OrderBurnRateAt returns the order burn fraction for orders opened at tipHeight
// (uses tip+1 so the next block's schedule gates the open).
func OrderBurnRateAt(tipHeight uint64) float64 {
	if EconomicsV2ActiveForIndex(tipHeight + 1) {
		return V2OrderBurnRate
	}
	return OrderBurnRate
}

// OrderPlatformFeeRateAt returns the platform fee fraction for orders at tipHeight.
func OrderPlatformFeeRateAt(tipHeight uint64) float64 {
	if EconomicsV2ActiveForIndex(tipHeight + 1) {
		return V2OrderPlatformFeeRate
	}
	return OrderPlatformFeeRate
}

// baseRewardV1 is the pre-v2 schedule (0.01, ~2y halvings).
func baseRewardV1(index uint64) float64 {
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

// baseRewardV2 is the post-activation schedule (0.1, ~4y halvings, 0.002 floor).
func baseRewardV2(index uint64) float64 {
	if V2RewardHalvingIntervalBlocks == 0 {
		return V2InitialBaseRewardHMC
	}
	epoch := float64((index - 1) / V2RewardHalvingIntervalBlocks)
	reward := V2InitialBaseRewardHMC / math.Pow(2, epoch)
	if reward < RewardTailFloorHMC {
		reward = RewardTailFloorHMC
	}
	return reward
}

// SUPRewardForBlockIndex returns SUP minted for an empty PoH block at index.
// Zero before v2 activation, for genesis, and for order-linked blocks (caller gates).
func SUPRewardForBlockIndex(index uint64) float64 {
	if !EconomicsV2ActiveForIndex(index) || index == 0 {
		return 0
	}
	if V2RewardHalvingIntervalBlocks == 0 {
		return V2SUPInitialReward
	}
	epoch := float64((index - 1) / V2RewardHalvingIntervalBlocks)
	reward := V2SUPInitialReward / math.Pow(2, epoch)
	// No SUP tail — emission stops when it would round to zero units.
	if reward < 1e-8 {
		return 0
	}
	return reward
}

// mintEmptyBlockSUPTx credits Economics v2 empty-block SUP to toAddr inside tx.
// No-op before activation, for order-linked blocks, zero schedule reward, or when the
// v2 (and ledger) caps are exhausted. Consensus emission — not gated by admin mint flags.
func (s *Service) mintEmptyBlockSUPTx(ctx context.Context, tx queryRowExecContext, blockIndex uint64, orderTaskID, toAddr string) error {
	if strings.TrimSpace(orderTaskID) != "" || !EconomicsV2ActiveForIndex(blockIndex) {
		return nil
	}
	reward := SUPRewardForBlockIndex(blockIndex)
	if reward <= 0 {
		return nil
	}
	amount := SUPToUnits(reward)
	if amount == 0 {
		return nil
	}
	toAddr = strings.TrimSpace(toAddr)
	if toAddr == "" || !strings.HasPrefix(toAddr, "HMC-") {
		return errors.New("chain: valid address required for empty-block SUP mint")
	}
	mintedU, err := s.supTotalMintedUnits(ctx, tx)
	if err != nil {
		return err
	}
	v2Cap := SUPToUnits(V2SUPMaxSupply)
	ledgerCap, err := s.supMaxSupplyUnits(ctx, tx)
	if err != nil {
		return err
	}
	capU := v2Cap
	if ledgerCap < capU {
		capU = ledgerCap
	}
	if mintedU >= capU {
		return nil
	}
	remain := capU - mintedU
	if amount > remain {
		amount = remain
	}
	if amount == 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO accounts (address, balance_units, balance_sup_units, next_nonce, sup_next_nonce, updated_at)
		 VALUES (?, 0, ?, 0, 0, strftime('%s','now'))
		 ON CONFLICT(address) DO UPDATE SET balance_sup_units=accounts.balance_sup_units+excluded.balance_sup_units, updated_at=excluded.updated_at`,
		toAddr, amount); err != nil {
		return err
	}
	return s.upsertMetaUint(ctx, tx, metaSUPTotalMintedUnits, mintedU+amount)
}
