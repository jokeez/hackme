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
