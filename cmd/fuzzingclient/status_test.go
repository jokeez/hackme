package main

import (
	"strings"
	"testing"
)

func TestParsePulseProgressWithETA(t *testing.T) {
	raw := []byte(`{
		"ok": true,
		"status": "running",
		"progress": {
			"runs_done": 50,
			"budget_runs": 256,
			"progress_pct": 19.53125,
			"runs_per_sec": 0.5,
			"budget_seconds": 28800,
			"eta_sec_est": 412.0
		}
	}`)
	pct, done, budget, status, eta, err := parsePulseProgress(raw)
	if err != nil {
		t.Fatal(err)
	}
	if status != "running" || done != 50 || budget != 256 {
		t.Fatalf("got status=%s done=%d budget=%d", status, done, budget)
	}
	if pct < 19 || pct > 20 {
		t.Fatalf("pct=%v", pct)
	}
	if eta == nil || *eta != 412 {
		t.Fatalf("eta=%v", eta)
	}
}

func TestParsePulseProgressFallbackETA(t *testing.T) {
	raw := []byte(`{
		"status": "running",
		"progress": {
			"runs_done": 10,
			"budget_runs": 110,
			"progress_pct": 0,
			"runs_per_sec": 2.0
		}
	}`)
	pct, _, _, _, eta, err := parsePulseProgress(raw)
	if err != nil {
		t.Fatal(err)
	}
	if pct < 9 || pct > 10 {
		t.Fatalf("derived pct=%v", pct)
	}
	if eta == nil || *eta != 50 {
		t.Fatalf("fallback eta=%v want 50", eta)
	}
}

func TestParseEscrowSpendPrefersLockedBountyAndRefundPath(t *testing.T) {
	raw := []byte(`{
		"escrow": {
			"budget_hmc": 5,
			"runs_paid_hmc": 0.2,
			"bounty_paid_hmc": 0,
			"crash_bonus_paid_hmc": 0.01,
			"bounty_pool_hmc": 4,
			"locked_bounty_hmc": 3.99,
			"refundable_hmc": 4.79,
			"refund_path": "finalize_or_cancel_refunds_unused_runs_and_locked_bounty",
			"status": "open"
		}
	}`)
	esc, err := parseEscrowSpend(raw)
	if err != nil {
		t.Fatal(err)
	}
	if esc.LockedBountyHMC != 3.99 || esc.BountyLockedHMC != 3.99 {
		t.Fatalf("locked=%v (must prefer locked_bounty_hmc)", esc.LockedBountyHMC)
	}
	if esc.SpentHMC < 0.209 || esc.SpentHMC > 0.211 {
		t.Fatalf("spent=%v want ~0.21", esc.SpentHMC)
	}
	if esc.RefundPath == "" || esc.RefundableHMC != 4.79 {
		t.Fatalf("refund_path=%q refundable=%v", esc.RefundPath, esc.RefundableHMC)
	}
}

func TestParseEscrowSpend(t *testing.T) {
	raw := []byte(`{
		"ok": true,
		"escrow": {
			"campaign_id": "c1",
			"budget_hmc": 5.0,
			"runs_pool_hmc": 1.0,
			"bounty_pool_hmc": 4.0,
			"runs_paid_hmc": 0.25,
			"bounty_paid_hmc": 0,
			"refunded_bounty_hmc": 0,
			"refunded_runs_hmc": 0,
			"status": "open"
		}
	}`)
	esc, err := parseEscrowSpend(raw)
	if err != nil {
		t.Fatal(err)
	}
	if esc.SpentHMC != 0.25 {
		t.Fatalf("spent=%v", esc.SpentHMC)
	}
	if esc.BountyLockedHMC != 4.0 {
		t.Fatalf("locked=%v", esc.BountyLockedHMC)
	}
	if esc.Status != "open" {
		t.Fatalf("status=%q", esc.Status)
	}
}

func TestParseEscrowSpendWithRefund(t *testing.T) {
	raw := []byte(`{
		"escrow": {
			"budget_hmc": 5,
			"runs_paid_hmc": 1,
			"bounty_paid_hmc": 0,
			"bounty_pool_hmc": 4,
			"refunded_bounty_hmc": 4,
			"refunded_runs_hmc": 0.2,
			"status": "closed"
		}
	}`)
	esc, err := parseEscrowSpend(raw)
	if err != nil {
		t.Fatal(err)
	}
	if esc.RefundedBountyHMC != 4 || esc.RefundedRunsHMC != 0.2 {
		t.Fatalf("refunds bounty=%v runs=%v", esc.RefundedBountyHMC, esc.RefundedRunsHMC)
	}
}

func TestParseTaskPoH(t *testing.T) {
	raw := []byte(`{
		"id": "order-1",
		"status": "open",
		"progress_count": 1,
		"target_solves": 4,
		"progress_pct": 25
	}`)
	pct, count, target, status, err := parseTaskPoH(raw)
	if err != nil {
		t.Fatal(err)
	}
	if pct != 25 || count != 1 || target != 4 || status != "open" {
		t.Fatalf("pct=%v count=%d target=%d status=%s", pct, count, target, status)
	}
}

func TestBuildOrderStatusView(t *testing.T) {
	pulse := []byte(`{"status":"running","progress":{"runs_done":64,"budget_runs":256,"progress_pct":25,"eta_sec_est":100}}`)
	escrow := []byte(`{"escrow":{"budget_hmc":5,"runs_paid_hmc":0.1,"bounty_paid_hmc":0,"bounty_pool_hmc":4,"status":"open"}}`)
	task := []byte(`{"id":"ord","progress_count":0,"target_solves":1,"progress_pct":0,"status":"open"}`)
	v, err := buildOrderStatusView("http://127.0.0.1:8080", "camp-1", "ord", pulse, escrow, task)
	if err != nil {
		t.Fatal(err)
	}
	if v.PrimaryDeliverable != "gate" {
		t.Fatalf("deliverable=%q", v.PrimaryDeliverable)
	}
	if v.FuzzProgressPct != 25 || v.Escrow == nil || v.Escrow.SpentHMC != 0.1 {
		t.Fatalf("view=%+v escrow=%+v", v, v.Escrow)
	}
	if v.PoHProgressPct == nil || *v.PoHProgressPct != 0 {
		t.Fatalf("poh=%v", v.PoHProgressPct)
	}
	if v.EtaSecEst == nil || *v.EtaSecEst != 100 {
		t.Fatalf("eta=%v", v.EtaSecEst)
	}
	if !strings.Contains(v.GateURL, "/gate?") {
		t.Fatalf("gate url=%s", v.GateURL)
	}
}
