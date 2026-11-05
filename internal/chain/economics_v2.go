package chain

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Economics v2 (RFC_ECONOMICS_V2.md D9) — height-gated; unset/0 = never activate.
const (
	// V2InitialBaseRewardHMC is empty PoH reward after Economics v2 activation.
	V2InitialBaseRewardHMC = 0.1
	// V2RewardHalvingIntervalBlocks is ~4 years at 30s/block.
	V2RewardHalvingIntervalBlocks uint64 = 4_204_800
	// V2OrderBurnRate / V2OrderPlatformFeeRate: 10% upfront as 5%/5% (era 0).
	V2OrderBurnRate        = 0.05
	V2OrderPlatformFeeRate = 0.05
	// V2SUPInitialReward is SUP per empty PoH block after activation (no tail).
	V2SUPInitialReward = 0.002
	// V2SUPMaxSupply is the published hard cap for v2 SUP emission (RFC D4 locked).
	V2SUPMaxSupply = 21_000.0
	// V2ActivationBufferHMC is the public start-buffer size for the activation announce (D3 locked).
	V2ActivationBufferHMC = 2000.0
	// V2DigBurnSoftStartCalendarDays is Dig-only product soft-start after announce wall-clock (D6 locked).
	V2DigBurnSoftStartCalendarDays = 30
)

const (
	metaEconomicsV2ActivationHeight = "economics_v2_activation_height"
	metaEconomicsV2AnnounceUnix     = "economics_v2_announce_unix"
)

// ErrHuntSoftStart is returned while Economics v2 Dig-only soft-start is active.
var ErrHuntSoftStart = errors.New("chain: Hunt escrow blocked during Economics v2 Dig soft-start")

var (
	econV2HeightOnce   sync.Once
	econV2Height       uint64 // 0 = disabled (from env)
	econV2MetaMu       sync.RWMutex
	econV2MetaOverride uint64 // locked chain meta wins over env when ≥ 1

	econV2AnnounceOnce     sync.Once
	econV2AnnounceUnix     int64 // 0 = unset
	econV2AnnounceMetaMu   sync.RWMutex
	econV2AnnounceOverride int64 // locked meta wins when ≥ 1
)

// EconomicsV2ActivationHeight is the first block index that uses the v2 schedule.
// Consensus preference: locked chain meta (see lockEconomicsV2ActivationTx).
// Bootstrap / lab: HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT=<uint64> (must be ≥ 1).
// Unset or 0 ⇒ v1 forever (soft-announce / soak; no vanity default height).
func EconomicsV2ActivationHeight() uint64 {
	econV2MetaMu.RLock()
	meta := econV2MetaOverride
	econV2MetaMu.RUnlock()
	if meta >= 1 {
		return meta
	}
	econV2HeightOnce.Do(func() {
		econV2Height = DefaultEconomicsV2ActivationHeight
		if v2CompiledDefaultsOff {
			econV2Height = 0
		}
		if v := strings.TrimSpace(os.Getenv("HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT")); v != "" {
			if n, err := strconv.ParseUint(v, 10, 64); err == nil && n >= 1 {
				econV2Height = n
			}
		}
	})
	return econV2Height
}

// DefaultEconomicsV2ActivationHeight is the published activation height compiled into the
// announce release (0 = not announced). Followers and desktops have no v2 env: like the ASERT
// height (495 400), the switch ships in the binary so every updated node crosses it on its own.
// Preference order stays: locked chain meta > env > this default.
const DefaultEconomicsV2ActivationHeight uint64 = 540_000

// v2CompiledDefaultsOff makes the compiled announce defaults (height + authority key) read as
// unset, for tests of pre-announce behavior. Production never sets it.
var v2CompiledDefaultsOff bool

// SetV2CompiledDefaultsOffForTest toggles v2CompiledDefaultsOff (tests only) and clears caches.
func SetV2CompiledDefaultsOffForTest(off bool) {
	v2CompiledDefaultsOff = off
	ResetEconomicsV2ActivationHeightForTest()
}

// ResetEconomicsV2ActivationHeightForTest clears the cached env/meta height (tests only).
func ResetEconomicsV2ActivationHeightForTest() {
	econV2HeightOnce = sync.Once{}
	econV2Height = 0
	econV2MetaMu.Lock()
	econV2MetaOverride = 0
	econV2MetaMu.Unlock()
	econV2AnnounceOnce = sync.Once{}
	econV2AnnounceUnix = 0
	econV2AnnounceMetaMu.Lock()
	econV2AnnounceOverride = 0
	econV2AnnounceMetaMu.Unlock()
}

// EconomicsV2ActiveForIndex reports whether block index uses the v2 schedule.
func EconomicsV2ActiveForIndex(index uint64) bool {
	act := EconomicsV2ActivationHeight()
	return act >= 1 && index >= 1 && index >= act
}

// EconomicsV2AnnounceUnix is the activation-announce wall-clock (RFC D6 soft-start origin).
// Preference: locked chain meta, else HACKME_ECONOMICS_V2_ANNOUNCE_UNIX. Unset/0 ⇒ no soft-start gate.
func EconomicsV2AnnounceUnix() int64 {
	econV2AnnounceMetaMu.RLock()
	meta := econV2AnnounceOverride
	econV2AnnounceMetaMu.RUnlock()
	if meta >= 1 {
		return meta
	}
	econV2AnnounceOnce.Do(func() {
		econV2AnnounceUnix = 0
		if v := strings.TrimSpace(os.Getenv("HACKME_ECONOMICS_V2_ANNOUNCE_UNIX")); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 1 {
				econV2AnnounceUnix = n
			}
		}
	})
	return econV2AnnounceUnix
}

// DigSoftStartActiveAt reports whether Dig-only soft-start applies at wallClockUnix.
// Requires a published announce unix; inactive when unset (pre-announce / soak).
func DigSoftStartActiveAt(wallClockUnix int64) bool {
	ann := EconomicsV2AnnounceUnix()
	if ann < 1 || wallClockUnix < 1 {
		return false
	}
	end := ann + int64(V2DigBurnSoftStartCalendarDays)*24*3600
	return wallClockUnix >= ann && wallClockUnix < end
}

// DigSoftStartActiveNow is DigSoftStartActiveAt(time.Now().Unix()).
func DigSoftStartActiveNow() bool {
	return DigSoftStartActiveAt(time.Now().Unix())
}

// economicsV2ActivationHeightQ prefers immutable chain meta, then env bootstrap.
func (s *Service) economicsV2ActivationHeightQ(ctx context.Context, q queryRowContext) (uint64, error) {
	metaH, err := s.metaUint(ctx, q, metaEconomicsV2ActivationHeight, 0)
	if err != nil {
		return 0, err
	}
	if metaH >= 1 {
		econV2MetaMu.Lock()
		econV2MetaOverride = metaH
		econV2MetaMu.Unlock()
		return metaH, nil
	}
	return EconomicsV2ActivationHeight(), nil
}

// lockEconomicsV2ActivationTx persists the env bootstrap height into meta once and
// refuses a mismatched env after lock (followers must share the same activation).
func (s *Service) lockEconomicsV2ActivationTx(ctx context.Context, tx queryRowExecContext) error {
	metaH, err := s.metaUint(ctx, tx, metaEconomicsV2ActivationHeight, 0)
	if err != nil {
		return err
	}
	rawEnv := uint64(0)
	if v := strings.TrimSpace(os.Getenv("HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT")); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil && n >= 1 {
			rawEnv = n
		}
	}
	if metaH >= 1 {
		if rawEnv >= 1 && rawEnv != metaH {
			return fmt.Errorf("chain: HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT=%d disagrees with locked meta %d", rawEnv, metaH)
		}
		econV2MetaMu.Lock()
		econV2MetaOverride = metaH
		econV2MetaMu.Unlock()
	} else if rawEnv >= 1 {
		if err := s.upsertMetaUint(ctx, tx, metaEconomicsV2ActivationHeight, rawEnv); err != nil {
			return err
		}
		econV2MetaMu.Lock()
		econV2MetaOverride = rawEnv
		econV2MetaMu.Unlock()
	}
	if err := s.lockEconomicsV2AnnounceTx(ctx, tx); err != nil {
		return err
	}
	return s.lockAuthorityPubKeyTx(ctx, tx, false) // producer adds its own key in AppendPoHBlock
}

// lockEconomicsV2AnnounceTx persists announce wall-clock once (D6 soft-start origin).
func (s *Service) lockEconomicsV2AnnounceTx(ctx context.Context, tx queryRowExecContext) error {
	metaA, err := s.metaUint(ctx, tx, metaEconomicsV2AnnounceUnix, 0)
	if err != nil {
		return err
	}
	rawEnv := int64(0)
	if v := strings.TrimSpace(os.Getenv("HACKME_ECONOMICS_V2_ANNOUNCE_UNIX")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 1 {
			rawEnv = n
		}
	}
	if metaA >= 1 {
		if metaA > uint64(math.MaxInt64) {
			return fmt.Errorf("chain: economics_v2 announce unix meta %d overflows int64", metaA)
		}
		if rawEnv >= 1 && uint64(rawEnv) != metaA {
			return fmt.Errorf("chain: HACKME_ECONOMICS_V2_ANNOUNCE_UNIX=%d disagrees with locked meta %d", rawEnv, metaA)
		}
		econV2AnnounceMetaMu.Lock()
		econV2AnnounceOverride = int64(metaA)
		econV2AnnounceMetaMu.Unlock()
		return nil
	}
	if rawEnv >= 1 {
		if err := s.upsertMetaUint(ctx, tx, metaEconomicsV2AnnounceUnix, uint64(rawEnv)); err != nil {
			return err
		}
		econV2AnnounceMetaMu.Lock()
		econV2AnnounceOverride = rawEnv
		econV2AnnounceMetaMu.Unlock()
	}
	return nil
}

// v2RewardEpoch returns the HMC v2 halving epoch for block index (≥1).
func v2RewardEpoch(index uint64) uint64 {
	if index == 0 || V2RewardHalvingIntervalBlocks == 0 {
		return 0
	}
	return (index - 1) / V2RewardHalvingIntervalBlocks
}

// V2OrderBurnFreezeEpoch: the order burn halves with the first two HMC halvings
// (5% → 2.5% → 1.25%) and stays at 1.25% from epoch 2 on. Freezing only at the HMC tail
// (epoch 6, 0.078%) would make the burn decorative: offsetting the 5.76 HMC/day tail emission
// would need ~7 400 HMC/day of orders, versus ~460 HMC/day at 1.25%.
const V2OrderBurnFreezeEpoch uint64 = 2

// v2BurnFreezeEpoch is the epoch from which the order burn rate no longer halves.
func v2BurnFreezeEpoch() uint64 {
	return V2OrderBurnFreezeEpoch
}

// OrderBurnRateAt returns the order burn fraction for orders opened at tipHeight
// (uses tip+1 so the next block's schedule gates the open).
// After v2: starts at 5%, halves with the first two HMC halvings, then stays at 1.25% (RFC §5.2).
func OrderBurnRateAt(tipHeight uint64) float64 {
	idx := tipHeight + 1
	if !EconomicsV2ActiveForIndex(idx) {
		return OrderBurnRate
	}
	epoch := v2RewardEpoch(idx)
	if freeze := v2BurnFreezeEpoch(); epoch > freeze {
		epoch = freeze
	}
	rate := V2OrderBurnRate / math.Pow(2, float64(epoch))
	if rate < 1e-12 {
		rate = 0
	}
	return rate
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

// Activation guard (producer side). Locking the height into chain meta is irreversible, so
// the producing node locks only when the operator confirms the same height in
// HACKME_ECONOMICS_V2_ACTIVATION_CONFIRM and the height is at least
// HACKME_ECONOMICS_V2_MIN_LEAD_BLOCKS ahead of the tip (default 20 160 ≈ 7 days, RFC D8).
// Otherwise block production stops with an explicit error (deploy gate / watchdog alert)
// instead of silently locking a typo. Followers importing blocks are not gated.
const defaultV2MinLeadBlocks = uint64(7 * 2880)

// v2ActivationGuardOff is flipped by package tests that exercise v2 from tiny heights.
var v2ActivationGuardOff bool

func v2MinLeadBlocks() uint64 {
	if v := strings.TrimSpace(os.Getenv("HACKME_ECONOMICS_V2_MIN_LEAD_BLOCKS")); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			return n
		}
	}
	return defaultV2MinLeadBlocks
}

// ErrV2ActivationGuard: refused to lock an unconfirmed or too-close activation height.
var ErrV2ActivationGuard = errors.New("chain: economics v2 activation height not locked")

// lockEconomicsV2ActivationForImportTx syncs locked meta / confirmed env, but never writes
// height meta from HEIGHT alone (CONFIRM required). Unconfirmed followers still validate
// headers via EconomicsV2ActivationHeight() env without poisoning local meta.
func (s *Service) lockEconomicsV2ActivationForImportTx(ctx context.Context, tx queryRowExecContext) error {
	metaH, err := s.metaUint(ctx, tx, metaEconomicsV2ActivationHeight, 0)
	if err != nil {
		return err
	}
	if metaH >= 1 {
		return s.lockEconomicsV2ActivationTx(ctx, tx)
	}
	raw := strings.TrimSpace(os.Getenv("HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT"))
	h, perr := strconv.ParseUint(raw, 10, 64)
	if raw == "" || perr != nil || h < 1 {
		return s.lockAuthorityPubKeyTx(ctx, tx, false)
	}
	if !v2ActivationGuardOff {
		if c := strings.TrimSpace(os.Getenv("HACKME_ECONOMICS_V2_ACTIVATION_CONFIRM")); c != raw {
			// Env height present but unconfirmed — do not lock; still pin authority if needed.
			return s.lockAuthorityPubKeyTx(ctx, tx, false)
		}
	}
	return s.lockEconomicsV2ActivationTx(ctx, tx)
}

// checkV2ActivationGuardTx runs before lockEconomicsV2ActivationTx on the producing path.
func (s *Service) checkV2ActivationGuardTx(ctx context.Context, tx queryRowExecContext, nextIdx uint64) error {
	if v2ActivationGuardOff {
		return nil
	}
	metaH, err := s.metaUint(ctx, tx, metaEconomicsV2ActivationHeight, 0)
	if err != nil || metaH >= 1 {
		return err // already locked (env mismatch is checked by the lock itself)
	}
	raw := strings.TrimSpace(os.Getenv("HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT"))
	h, perr := strconv.ParseUint(raw, 10, 64)
	if raw == "" || perr != nil || h < 1 {
		return nil // not configured: nothing to lock
	}
	if c := strings.TrimSpace(os.Getenv("HACKME_ECONOMICS_V2_ACTIVATION_CONFIRM")); c != raw {
		return fmt.Errorf("%w: set HACKME_ECONOMICS_V2_ACTIVATION_CONFIRM=%d to confirm (got %q)", ErrV2ActivationGuard, h, c)
	}
	if lead := v2MinLeadBlocks(); h < nextIdx || h-nextIdx < lead {
		// Keep the lead distance in uint64 — avoid CodeQL incorrect-integer-conversion on ParseUint→int64.
		var after uint64
		if h >= nextIdx {
			after = h - nextIdx
		}
		return fmt.Errorf("%w: height %d is only %d blocks after next block %d (minimum lead %d)",
			ErrV2ActivationGuard, h, after, nextIdx, lead)
	}
	return nil
}
