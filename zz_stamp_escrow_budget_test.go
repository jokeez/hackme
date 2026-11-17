package main

import (
	"testing"

	"hackme/internal/chain"
)

func TestStampEscrowBudgetClaimUsesPostFeeRow(t *testing.T) {
	cfg := map[string]any{}
	stampEscrowBudgetClaim(cfg, 20.0, &chain.FuzzEscrowRow{BudgetHMC: 18.0})
	if got, _ := cfg["budget_hmc"].(float64); got != 18.0 {
		t.Fatalf("budget_hmc=%v want 18 (post-fee escrow)", got)
	}
	if got, _ := cfg["budget_hmc_price"].(float64); got != 20.0 {
		t.Fatalf("budget_hmc_price=%v want 20 (customer price)", got)
	}
}

func TestStampEscrowBudgetClaimFallbackPrice(t *testing.T) {
	cfg := map[string]any{}
	stampEscrowBudgetClaim(cfg, 15.0, nil)
	if got, _ := cfg["budget_hmc"].(float64); got != 15.0 {
		t.Fatalf("budget_hmc=%v want 15", got)
	}
}

func TestPoolRegisterClaimBudgetPrefersCustomerPrice(t *testing.T) {
	got := poolRegisterClaimBudgetHMC(15.0, &chain.FuzzEscrowRow{BudgetHMC: 13.5})
	if got != 15.0 {
		t.Fatalf("claim=%v want customer price 15 (not post-fee 13.5)", got)
	}
	got = poolRegisterClaimBudgetHMC(0, &chain.FuzzEscrowRow{BudgetHMC: 13.5})
	if got != 13.5 {
		t.Fatalf("claim=%v want escrow fallback 13.5", got)
	}
}
