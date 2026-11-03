package main

import (
	"errors"
	"net/http"
	"strings"

	"hackme/internal/chain"
)

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
		writeAPIError(w, http.StatusPaymentRequired, "escrow_failed",
			"insufficient Wallet HMC for escrow — top up on-chain balance on #wallet, then Start (auto-funds spendable)", nil)
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
