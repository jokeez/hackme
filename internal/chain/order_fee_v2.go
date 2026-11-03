package chain

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"math/bits"
	"strings"
)

// Economics v2 order fee (RFC §5.2 / §4.5). Customer price = miner escrow + fee; the fee is
// burn (5% in era 0, halving with HMC eras, frozen in the tail) + platform (5%) of the price.
// The escrow is always HMC and identical whichever asset pays the fee:
//
//	fee_asset=HMC: price HMC debited; burn destroyed for real, platform credited to DevFeeAddress.
//	fee_asset=SUP: escrow HMC debited; the whole fee (burn+platform) burned in SUP at
//	               SUPFeeRateToHMCFee, through the one SUP spend path (product hmc_order).
//
// Before activation, order opens keep the v1 behaviour.

const (
	FeeAssetHMC = "HMC"
	FeeAssetSUP = "SUP"
)

var ErrFeeAsset = errors.New("chain: fee_asset must be HMC or SUP")

// OrderFeeV2 is one order's fee legs in units.
type OrderFeeV2 struct {
	Asset         string `json:"fee_asset"`
	BurnUnits     uint64 `json:"fee_burn_units"`     // HMC-equivalent burn leg
	PlatformUnits uint64 `json:"fee_platform_units"` // HMC-equivalent platform leg
	SUPUnits      uint64 `json:"fee_sup_units"`      // SUP burned when Asset == SUP
}

// HMCUnits is the HMC the payer spends on the fee (0 when paid in SUP).
func (f OrderFeeV2) HMCUnits() uint64 {
	if f.Asset == FeeAssetSUP {
		return 0
	}
	return f.BurnUnits + f.PlatformUnits
}

// NormalizeFeeAsset maps API input to HMC|SUP (empty ⇒ HMC).
func NormalizeFeeAsset(v string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "", FeeAssetHMC:
		return FeeAssetHMC, nil
	case FeeAssetSUP:
		return FeeAssetSUP, nil
	}
	return "", ErrFeeAsset
}

func ratePPM(rate float64) uint64 {
	if rate <= 0 || math.IsNaN(rate) {
		return 0
	}
	return uint64(math.Round(rate * 1e6))
}

// mulDivPPM returns v·ppm/1e6 without overflow.
func mulDivPPM(v, ppm uint64) uint64 {
	hi, lo := bits.Mul64(v, ppm)
	q, _ := bits.Div64(hi, lo, 1_000_000)
	return q
}

func orderFeeV2Rates(tipHeight uint64) (burnPPM, platformPPM uint64) {
	return ratePPM(OrderBurnRateAt(tipHeight)), ratePPM(OrderPlatformFeeRateAt(tipHeight))
}

func newOrderFeeV2(asset string, burn, platform uint64) OrderFeeV2 {
	f := OrderFeeV2{Asset: asset, BurnUnits: burn, PlatformUnits: platform}
	if asset == FeeAssetSUP {
		f.SUPUnits = (burn + platform) * SUPFeeRateToHMCFee
	}
	return f
}

// OrderFeeV2FromPrice splits a customer price (Dig/Hunt budget) into fee legs and escrow.
func OrderFeeV2FromPrice(tipHeight, priceUnits uint64, asset string) (OrderFeeV2, uint64) {
	bp, pp := orderFeeV2Rates(tipHeight)
	burn, platform := mulDivPPM(priceUnits, bp), mulDivPPM(priceUnits, pp)
	return newOrderFeeV2(asset, burn, platform), priceUnits - burn - platform
}

// OrderFeeV2ForEscrow prices a fixed escrow (PoH order reward × solves): price = escrow/(1−r).
func OrderFeeV2ForEscrow(tipHeight, escrowUnits uint64, asset string) OrderFeeV2 {
	bp, pp := orderFeeV2Rates(tipHeight)
	if bp+pp == 0 || bp+pp >= 1_000_000 {
		return newOrderFeeV2(asset, 0, 0)
	}
	hi, lo := bits.Mul64(escrowUnits, 1_000_000)
	price, _ := bits.Div64(hi, lo, 1_000_000-bp-pp)
	return newOrderFeeV2(asset, mulDivPPM(price, bp), mulDivPPM(price, pp))
}

// applyOrderFeeV2Tx books the fee inside the order-open tx. The caller has already debited
// escrow + f.HMCUnits() from the primary wallet (dual ledger). Caller holds s.mu.
func (s *Service) applyOrderFeeV2Tx(ctx context.Context, tx *sql.Tx, walletAddr string, f OrderFeeV2, ref string) (string, error) {
	if f.Asset == FeeAssetSUP {
		if f.SUPUnits == 0 {
			return "", nil
		}
		return s.burnSUPForFeeTx(ctx, tx, walletAddr, f.SUPUnits, SUPFeeProductHMCOrder, ref)
	}
	if f.PlatformUnits > 0 {
		if err := s.creditUnits(ctx, tx, DevFeeAddress, f.PlatformUnits); err != nil {
			return "internal_error", err
		}
	}
	if f.BurnUnits > 0 {
		// Real burn: the units already left the payer's balance; only the counter moves here.
		mintedUnits, burnedUnits, err := s.econTotalsUnits(ctx, tx)
		if err != nil {
			return "internal_error", err
		}
		if err := s.upsertMetaUint(ctx, tx, metaTotalBurnedUnits, burnedUnits+f.BurnUnits); err != nil {
			return "internal_error", err
		}
		if err := s.upsertMetaFloat(ctx, tx, metaTotalBurnedHMC, UnitsToHMC(burnedUnits+f.BurnUnits)); err != nil {
			return "internal_error", err
		}
		if err := s.upsertMetaFloat(ctx, tx, metaTotalMintedHMC, UnitsToHMC(mintedUnits)); err != nil {
			return "internal_error", err
		}
	}
	return "", nil
}
