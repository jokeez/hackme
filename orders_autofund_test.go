package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"hackme/internal/chain"
)

func TestOrdersFundAmountForShortfall(t *testing.T) {
	if v, err := ordersFundAmountForShortfall(0); err != nil || v != 0 {
		t.Fatalf("zero: %v %v", v, err)
	}
	v, err := ordersFundAmountForShortfall(5)
	if err != nil || v != chain.OrdersFundMinHMC {
		t.Fatalf("round-up: got %v err=%v want min %v", v, err, chain.OrdersFundMinHMC)
	}
	v, err = ordersFundAmountForShortfall(40)
	if err != nil || v != 40 {
		t.Fatalf("exact: %v %v", v, err)
	}
	if _, err := ordersFundAmountForShortfall(chain.OrdersFundMaxHMC + 10); err == nil {
		t.Fatal("expected too-large error")
	}
}

func TestEnsureOrdersSpendableAlreadyEnough(t *testing.T) {
	a, _ := newWalletTestApp(t)
	ctx := context.Background()
	from := a.signer.Address()
	if _, err := a.chain.CreditOrdersFund(ctx, from, "aa00112233445566778899aabbccddee", chain.HMCToUnits(25)); err != nil {
		t.Fatal(err)
	}
	funded, err := a.ensureOrdersSpendableForBudget(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	if funded != nil {
		t.Fatalf("expected no fund when already enough, got %+v", funded)
	}
}

func TestEnsureOrdersSpendableInsufficientNoCanonical(t *testing.T) {
	a, _ := newWalletTestApp(t)
	t.Setenv("HACKME_DESKTOP_MODE", "")
	t.Setenv("HACKME_CANONICAL_CHAIN_URL", "")
	_, err := a.ensureOrdersSpendableForBudget(context.Background(), 20)
	if err == nil {
		t.Fatal("expected error")
	}
	var af *ordersAutoFundError
	if !errors.As(err, &af) {
		t.Fatalf("want ordersAutoFundError, got %T %v", err, err)
	}
	if af.Code != "insufficient_orders_spendable" && af.Code != "insufficient_on_chain" && af.Code != "escrow_failed" {
		t.Fatalf("code=%s msg=%s", af.Code, af.Msg)
	}
	if !strings.Contains(af.Msg, "available to start") {
		t.Fatalf("expected actionable spendable hint, got %q", af.Msg)
	}
}

func TestEnsureOrdersSpendableAutoFundThenEnough(t *testing.T) {
	a, _ := newWalletTestApp(t)
	from := a.signer.Address()
	t.Setenv("HACKME_DESKTOP_MODE", "1")
	t.Setenv("DESKTOP_PROFILE", "orders")

	var sends atomic.Int32
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/tx/send":
			sends.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":      true,
				"tx_hash": "autofund1234abcd5678ffff0000eeee99",
				"status":  "accepted",
			})
		case strings.HasPrefix(r.URL.Path, "/api/tx/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tx_hash":      "autofund1234abcd5678ffff0000eeee99",
				"status":       "included",
				"from":         from,
				"to":           chain.DevFeeAddress,
				"amount_units": chain.HMCToUnits(20),
				"memo":         chain.OrdersFundMemo(from),
			})
		case strings.HasPrefix(r.URL.Path, "/api/address/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"address":       from,
				"balance_units": chain.HMCToUnits(100),
				"next_nonce":    1,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer peer.Close()
	t.Setenv("HACKME_CANONICAL_CHAIN_URL", peer.URL)

	ctx := context.Background()
	_, bal0, _ := a.chain.Wallet(ctx)
	if bal0 >= 20 {
		t.Fatalf("precondition: expected low spendable, got %v", bal0)
	}

	funded, err := a.ensureOrdersSpendableForBudget(ctx, 20)
	if err != nil {
		t.Fatalf("auto-fund: %v", err)
	}
	if funded == nil || funded.AmountHMC < 19.9 {
		t.Fatalf("funded=%+v", funded)
	}
	if sends.Load() < 1 {
		t.Fatal("expected on-chain submit")
	}
	_, bal1, _ := a.chain.Wallet(ctx)
	if bal1 < 19.9 {
		t.Fatalf("spendable after=%v", bal1)
	}

	sends.Store(0)
	funded2, err := a.ensureOrdersSpendableForBudget(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	if funded2 != nil {
		t.Fatalf("expected skip fund, got %+v", funded2)
	}
	if sends.Load() != 0 {
		t.Fatalf("unexpected submit count %d", sends.Load())
	}
}

func TestEnsureOrdersSpendableInsufficientOnChain(t *testing.T) {
	a, _ := newWalletTestApp(t)
	from := a.signer.Address()
	t.Setenv("HACKME_DESKTOP_MODE", "1")
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/address/") {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"address":       from,
				"balance_units": chain.HMCToUnits(1),
				"next_nonce":    1,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer peer.Close()
	t.Setenv("HACKME_CANONICAL_CHAIN_URL", peer.URL)

	_, err := a.ensureOrdersSpendableForBudget(context.Background(), 20)
	if err == nil {
		t.Fatal("expected insufficient_on_chain")
	}
	var af *ordersAutoFundError
	if !errors.As(err, &af) || af.Code != "insufficient_on_chain" {
		t.Fatalf("got %v", err)
	}
}

func TestHuntEscrowAfterAutoFund(t *testing.T) {
	a, _ := newWalletTestApp(t)
	from := a.signer.Address()
	t.Setenv("HACKME_DESKTOP_MODE", "1")
	t.Setenv("DESKTOP_PROFILE", "orders")

	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/tx/send":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true, "tx_hash": "huntfund1234abcd5678ffff0000eeee11", "status": "accepted",
			})
		case strings.HasPrefix(r.URL.Path, "/api/tx/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tx_hash": "huntfund1234abcd5678ffff0000eeee11", "status": "included",
				"from": from, "to": chain.DevFeeAddress,
				"amount_units": chain.HMCToUnits(20), "memo": chain.OrdersFundMemo(from),
			})
		case strings.HasPrefix(r.URL.Path, "/api/address/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"address": from, "balance_units": chain.HMCToUnits(100), "next_nonce": 1,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer peer.Close()
	t.Setenv("HACKME_CANONICAL_CHAIN_URL", peer.URL)

	ctx := context.Background()
	funded, err := a.ensureOrdersSpendableForBudget(ctx, 20)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if funded == nil {
		t.Fatal("expected fund")
	}
	row, err := a.chain.OpenHuntEscrow(ctx, "hunt-autofund-e2e", 20, 100)
	if err != nil {
		t.Fatal(err)
	}
	if row.BudgetHMC < 19.9 {
		t.Fatalf("escrow=%+v", row)
	}
}
