package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"hackme/internal/chain"
)

// ordersAutoFundError is a stable HTTP-mappable failure from Fund / auto-fund-on-Start.
type ordersAutoFundError struct {
	HTTPStatus int
	Code       string
	Msg        string
	Extra      map[string]any
}

func (e *ordersAutoFundError) Error() string {
	if e == nil {
		return "orders auto-fund error"
	}
	return e.Msg
}

func writeOrdersAutoFundFailed(w http.ResponseWriter, err error) {
	var af *ordersAutoFundError
	if errors.As(err, &af) && af != nil {
		st := af.HTTPStatus
		if st == 0 {
			st = http.StatusBadRequest
		}
		writeAPIError(w, st, af.Code, af.Msg, af.Extra)
		return
	}
	writeAPIError(w, http.StatusInternalServerError, "fund_failed", err.Error(), nil)
}

// ordersFundAmountForShortfall rounds a spendable shortfall up to fund bounds.
func ordersFundAmountForShortfall(shortfallHMC float64) (float64, error) {
	if shortfallHMC <= 1e-12 {
		return 0, nil
	}
	amt := shortfallHMC
	if amt < chain.OrdersFundMinHMC {
		amt = chain.OrdersFundMinHMC
	}
	if amt > chain.OrdersFundMaxHMC+1e-12 {
		return 0, &ordersAutoFundError{
			HTTPStatus: http.StatusPaymentRequired,
			Code:       "fund_amount_too_large",
			Msg: fmt.Sprintf("need %.2f HMC more spendable but max single fund is %.0f HMC — Fund Orders in chunks or lower budget",
				shortfallHMC, chain.OrdersFundMaxHMC),
			Extra: map[string]any{"shortfall_hmc": shortfallHMC, "max_fund_hmc": chain.OrdersFundMaxHMC},
		}
	}
	return amt, nil
}

// ordersSpendableShortfallHint builds an actionable Start/Dig message: fund vs wait for escrow refund.
func (a *app) ordersSpendableShortfallHint(ctx context.Context, spendable, needHMC, onChainHint float64) (msg string, extra map[string]any) {
	shortfall := needHMC - spendable
	if shortfall < 0 {
		shortfall = 0
	}
	lockedHMC, _, _ := a.chain.OrdersEscrowLockedHMC(ctx)
	extra = map[string]any{
		"spendable_hmc":      spendable,
		"need_hmc":           needHMC,
		"shortfall_hmc":      shortfall,
		"escrow_locked_hmc":  lockedHMC,
		"on_chain_hmc":       onChainHint,
		"available_to_start": spendable,
	}
	switch {
	case onChainHint+1e-9 >= needHMC && spendable+1e-9 < needHMC:
		msg = fmt.Sprintf(
			"available to start %.4f HMC < need %.4f (on-chain %.4f is enough — retry Start to auto-fund, or Fund Orders). Escrow-locked %.4f HMC is not spendable until finalize/cancel refund",
			spendable, needHMC, onChainHint, lockedHMC)
		extra["action"] = "retry_autofund_or_fund_orders"
	case lockedHMC > 0.0001 && onChainHint+spendable+1e-9 < needHMC:
		msg = fmt.Sprintf(
			"available to start %.4f HMC < need %.4f (on-chain %.4f). %.4f HMC is in escrow — wait for Dig/Hunt finalize/cancel refund, or top up #wallet",
			spendable, needHMC, onChainHint, lockedHMC)
		extra["action"] = "wait_escrow_refund_or_fund_wallet"
	default:
		msg = fmt.Sprintf(
			"available to start %.4f HMC < need %.4f (on-chain %.4f, in escrow %.4f). Top up on-chain HMC on #wallet, then Start (auto-funds spendable)",
			spendable, needHMC, onChainHint, lockedHMC)
		extra["action"] = "fund_wallet_on_chain"
	}
	return msg, extra
}

// ensureOrdersSpendableForBudget tops up local spendable from on-chain Wallet when short.
// No-op when local balance already covers needHMC. On non-follower nodes (no canonical fund path),
// returns a clear Wallet insufficient error instead of attempting Fund.
func (a *app) ensureOrdersSpendableForBudget(ctx context.Context, needHMC float64) (*chain.OrdersFundResult, error) {
	if a == nil || a.chain == nil {
		return nil, &ordersAutoFundError{
			HTTPStatus: http.StatusServiceUnavailable,
			Code:       "chain_unavailable",
			Msg:        "chain unavailable",
		}
	}
	if needHMC <= 1e-12 {
		return nil, nil
	}
	_, bal, err := a.chain.Wallet(ctx)
	if err != nil {
		return nil, err
	}
	if bal+1e-9 >= needHMC {
		return nil, nil
	}
	shortfall := needHMC - bal

	onChainHint := 0.0
	if a.signer != nil {
		addrCtx, addrCancel := context.WithTimeout(ctx, 3*time.Second)
		if units, _, ok := a.fetchCanonicalAddressState(addrCtx, a.signer.Address()); ok {
			onChainHint = chain.UnitsToHMC(units)
		}
		addrCancel()
	}

	if _, cerr := a.ordersFundCanonicalBase(); cerr != nil {
		msg, extra := a.ordersSpendableShortfallHint(ctx, bal, needHMC, onChainHint)
		return nil, &ordersAutoFundError{
			HTTPStatus: http.StatusPaymentRequired,
			Code:       "insufficient_orders_spendable",
			Msg:        msg,
			Extra:      extra,
		}
	}

	fundAmt, ferr := ordersFundAmountForShortfall(shortfall)
	if ferr != nil {
		return nil, ferr
	}
	if fundAmt <= 1e-12 {
		return nil, nil
	}

	out, err := a.fundOrdersSpendable(ctx, fundAmt)
	if err != nil {
		var af *ordersAutoFundError
		if errors.As(err, &af) && af != nil && af.Code == "insufficient_on_chain" {
			msg, extra := a.ordersSpendableShortfallHint(ctx, bal, needHMC, onChainHint)
			af.Msg = msg
			if af.Extra == nil {
				af.Extra = extra
			} else {
				for k, v := range extra {
					if _, ok := af.Extra[k]; !ok {
						af.Extra[k] = v
					}
				}
				af.Extra["action"] = extra["action"]
			}
			return nil, af
		}
		return nil, err
	}
	if out != nil && out.Pending {
		return nil, &ordersAutoFundError{
			HTTPStatus: http.StatusConflict,
			Code:       "fund_pending_confirm",
			Msg:        "on-chain fund submitted but not yet included — retry Start in a few seconds, or POST /api/orders/fund/confirm",
			Extra: map[string]any{
				"tx_hash":      out.TxHash,
				"amount_hmc":   out.AmountHMC,
				"amount_units": out.AmountUnits,
				"action":       "retry_start_or_confirm_fund",
			},
		}
	}
	_, balAfter, _ := a.chain.Wallet(ctx)
	if balAfter+1e-9 < needHMC {
		msg, extra := a.ordersSpendableShortfallHint(ctx, balAfter, needHMC, onChainHint)
		extra["funded"] = out.Funded
		return nil, &ordersAutoFundError{
			HTTPStatus: http.StatusPaymentRequired,
			Code:       "insufficient_orders_spendable",
			Msg:        msg,
			Extra:      extra,
		}
	}
	if out != nil {
		return out.Funded, nil
	}
	return nil, nil
}
