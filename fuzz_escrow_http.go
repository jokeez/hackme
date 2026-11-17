package main

import (
	"errors"
	"net/http"
	"strings"

	"hackme/internal/chain"
)

// stampEscrowBudgetClaim writes the pool/fingerprint claim from the open escrow row.
// Under Economics v2 the row holds the post-fee miner budget while the customer paid
// priceHMC; pool sync refuses when config budget_hmc exceeds the row.
func stampEscrowBudgetClaim(cfg map[string]any, priceHMC float64, escrow *chain.FuzzEscrowRow) {
	if cfg == nil {
		return
	}
	if priceHMC > 0 {
		cfg["budget_hmc_price"] = priceHMC
	}
	if escrow != nil && escrow.BudgetHMC > 0 {
		cfg["budget_hmc"] = escrow.BudgetHMC
		return
	}
	if priceHMC > 0 {
		cfg["budget_hmc"] = priceHMC
	}
}

// poolRegisterClaimBudgetHMC is the prepaid budget published to the coordinator.
// Prefer the customer SKU price: Economics v2 locks a post-fee row (~90% of price),
// and coordinator package floors (hunt_lite ≥15, dig deep ≥25) are priced in customer
// HMC. Registering the post-fee row alone rejects legitimate min-SKU Hunt/Deep Dig.
func poolRegisterClaimBudgetHMC(priceHMC float64, escrow *chain.FuzzEscrowRow) float64 {
	if priceHMC > 0 {
		return priceHMC
	}
	if escrow != nil && escrow.BudgetHMC > 0 {
		return escrow.BudgetHMC
	}
	return 0
}

// writeOrdersAutoFundOrEscrowFailed maps auto-fund errors first, then escrow open failures.
func writeOrdersAutoFundOrEscrowFailed(w http.ResponseWriter, err error) {
	var af *ordersAutoFundError
	if errors.As(err, &af) && af != nil {
		writeOrdersAutoFundFailed(w, err)
		return
	}
	writeFuzzEscrowFailed(w, err)
}

// writeFuzzEscrowFailed maps escrow open errors to stable HTTP codes/messages.
// Never surface driver strings (e.g. sql.ErrNoRows) to clients.
func writeFuzzEscrowFailed(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, chain.ErrFuzzWalletMissing):
		writeAPIError(w, http.StatusPaymentRequired, "escrow_unavailable",
			"local wallet not initialized — run POST /api/genesis on this node, then Start Dig/Hunt (auto-funds from #wallet on-chain) or Fund Orders", nil)
	case errors.Is(err, chain.ErrFuzzInsufficientBalance):
		writeAPIError(w, http.StatusPaymentRequired, "insufficient_orders_spendable",
			"available to start (spendable) is below Dig/Hunt budget — on-chain alone is not enough until Start auto-funds or you Fund Orders; if HMC is in escrow, wait for finalize/cancel refund", nil)
	case errors.Is(err, chain.ErrSUPFeeInsufficent):
		writeAPIError(w, http.StatusPaymentRequired, "fee_sup_insufficient",
			"not enough SUP to pay the order fee — pay the fee in HMC (fee_asset=HMC) or add SUP", nil)
	case errors.Is(err, chain.ErrFeeAsset):
		writeAPIError(w, http.StatusBadRequest, "invalid_fee_asset", "fee_asset must be HMC or SUP", nil)
	default:
		if err != nil && strings.Contains(err.Error(), "fee_asset SUP requires Economics v2") {
			writeAPIError(w, http.StatusBadRequest, "fee_asset_unavailable", "paying the order fee in SUP starts with Economics v2", nil)
			return
		}
		// Surface stable validation messages (shards/budget floors) without sql driver leaks.
		msg := "escrow open failed"
		if err != nil {
			s := err.Error()
			switch {
			case strings.Contains(s, "budget_shards below minimum"),
				strings.Contains(s, "per-shard payout below minimum"),
				strings.Contains(s, "budget below minimum"),
				strings.Contains(s, "budget above maximum"),
				strings.Contains(s, "already exists"):
				msg = s
			}
		}
		writeAPIError(w, http.StatusPaymentRequired, "escrow_failed", msg, nil)
	}
}
