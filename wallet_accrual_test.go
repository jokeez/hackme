package main

import "testing"

func TestWalletAccrualFromCoordinatorNoWorkersMap(t *testing.T) {
	ws := map[string]any{
		"workers_count":             uint64(2),
		"total_payout_hmc":          0.17,
		"last_signed_miner_address": "HMC-91fe007e4036c602",
		"submitted_items":           uint64(90000),
	}
	accrued, settled, unpaid, src := walletAccrualFromCoordinator(ws, nil, "HMC-91fe007e4036c602", "worker-kapa-pc", nil, false)
	if accrued != 0 || unpaid != 0 || src != "workers_omitted" {
		t.Fatalf("accrued=%v settled=%v unpaid=%v src=%s", accrued, settled, unpaid, src)
	}
}

func TestWalletAccrualSkipsAggregateWhenSignerMismatch(t *testing.T) {
	ws := map[string]any{
		"workers_count":             uint64(1),
		"total_payout_hmc":          0.17,
		"last_signed_miner_address": "HMC-381c0c5e2cfcc714",
	}
	ensureCoordinatorWorkersMap(ws)
	_, _, unpaid, src := walletAccrualFromCoordinator(ws, nil, "HMC-91fe007e4036c602", "worker-kapa-pc", nil, false)
	if unpaid > 1e-12 {
		t.Fatalf("expected 0 wallet unpaid, got %v src=%s", unpaid, src)
	}
}

func TestWalletAccrualPerWorkerRow(t *testing.T) {
	ws := map[string]any{
		"workers_count": uint64(2),
		"workers": map[string]any{
			"worker-kapa-pc": map[string]any{
				"payout_hmc":     0.0075,
				"payout_address": "HMC-91fe007e4036c602",
			},
			"vps-canary-01": map[string]any{
				"payout_hmc":     0.83,
				"payout_address": "HMC-381c0c5e2cfcc714",
			},
		},
	}
	accrued, _, unpaid, src := walletAccrualFromCoordinator(ws, nil, "HMC-91fe007e4036c602", "worker-kapa-pc", nil, false)
	if accrued < 0.0075-1e-9 || unpaid < 0.0075-1e-9 || src != "per_worker" {
		t.Fatalf("accrued=%v unpaid=%v src=%s", accrued, unpaid, src)
	}
}

func TestWalletAccrualPayoutMapWhenWorkersMissing(t *testing.T) {
	t.Setenv("HACKME_DESKTOP_MODE", "1")
	ws := map[string]any{
		"workers_count":             uint64(2),
		"total_payout_hmc":          0.17,
		"last_signed_miner_address": "HMC-381c0c5e2cfcc714",
	}
	payoutMap := map[string]string{"worker-kapa-pc": "HMC-91fe007e4036c602"}
	_, _, unpaid, src := walletAccrualFromCoordinator(ws, nil, "HMC-91fe007e4036c602", "worker-kapa-pc", payoutMap, false)
	if unpaid > 1e-12 || src != "workers_omitted" {
		t.Fatalf("expected no fleet-total attribution, got unpaid=%v src=%s", unpaid, src)
	}
}
