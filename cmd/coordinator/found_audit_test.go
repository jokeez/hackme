package main

import (
	"math"
	"testing"

	"hackme/internal/chain"
)

func TestPoissonLogCDF(t *testing.T) {
	// P(X ≤ 0 | λ) = e^-λ
	if got := poissonLogCDF(0, 30); math.Abs(got+30) > 1e-9 {
		t.Fatalf("ln P(0|30)=%v want -30", got)
	}
	// P(X ≤ 2 | 1) = e^-1 (1 + 1 + 1/2)
	want := math.Log(math.Exp(-1) * 2.5)
	if got := poissonLogCDF(2, 1); math.Abs(got-want) > 1e-9 {
		t.Fatalf("ln P(≤2|1)=%v want %v", got, want)
	}
	if got := poissonLogCDF(50, 10); math.Abs(got) > 1e-9 {
		t.Fatalf("far upper tail must be ≈0, got %v", got)
	}
}

func TestFoundRateSuspiciousThresholds(t *testing.T) {
	cases := []struct {
		exp, act float64
		want     bool
	}{
		{29, 0, false},   // not enough evidence yet
		{30, 0, true},    // e^-30 ≪ 1e-9
		{100, 30, false}, // 30% of expected: bad luck / stale, not withholding
		{100, 5, true},
		{200, 120, false},
	}
	for _, c := range cases {
		if got := foundRateSuspicious(c.exp, c.act); got != c.want {
			t.Fatalf("exp=%v act=%v got %v want %v", c.exp, c.act, got, c.want)
		}
	}
}

// Honest miners report at most one found per lease. With big leases (λ≫1 hits per lease) the
// audit must still clear them — counting λ instead of P(≥1) would flag every 5090.
func TestFoundAuditHonestBigLeasesNotFlagged(t *testing.T) {
	wm := &workManager{}
	key := foundAuditKey("HMC-aaaaaaaaaaaaaaaa", "w")
	const leaseMod, sm = 1_000_000, 100_000
	for i := 0; i < 5000; i++ {
		wm.noteFoundAuditHitLocked(key) // λ=3.2 ⇒ honest lease nearly always has a hit
		if wm.noteFoundAuditWorkLocked(key, 32, sm, leaseMod) {
			t.Fatalf("honest big-lease miner flagged at lease %d: %+v", i, *wm.foundAudit[key])
		}
	}
}

func TestFoundAuditWithholderFlaggedAndRecovers(t *testing.T) {
	wm := &workManager{}
	key := foundAuditKey("", "w-hide")
	flagged := false
	for i := 0; i < 100 && !flagged; i++ {
		flagged = wm.noteFoundAuditWorkLocked(key, 32, 2048, 70_000) // λ≈0.94, never reports
	}
	if !flagged {
		t.Fatalf("withholder never flagged: %+v", *wm.foundAudit[key])
	}
	// Fixed client reports its hits again: the decaying window clears it.
	cleared := false
	for i := 0; i < 2000 && !cleared; i++ {
		if i%3 != 0 {
			wm.noteFoundAuditHitLocked(key)
		}
		cleared = !wm.noteFoundAuditWorkLocked(key, 32, 2048, 70_000)
	}
	if !cleared {
		t.Fatalf("recovered miner still flagged: %+v", *wm.foundAudit[key])
	}
}

func TestFoundAuditModeOffAndLog(t *testing.T) {
	t.Setenv("HACKME_COORDINATOR_FOUND_AUDIT", "")
	if foundAuditMode() != "log" {
		t.Fatal("default must be log (observe before rejecting)")
	}
	t.Setenv("HACKME_COORDINATOR_FOUND_AUDIT", "off")
	wm := &workManager{}
	for i := 0; i < 200; i++ {
		if wm.noteFoundAuditWorkLocked("k", 32, 2048, 70_000) {
			t.Fatal("audit off must never flag")
		}
	}
	t.Setenv("HACKME_COORDINATOR_FOUND_AUDIT", "log")
	if foundAuditMode() != "log" {
		t.Fatal("log mode")
	}
}

// End to end through submit: a client that hashes full leases and returns valid shares but
// never reports its block hits stops being paid; an honest one keeps getting paid.
func TestSecurityBlockWithholdingStopsPay(t *testing.T) {
	t.Setenv("HACKME_COORDINATOR_FOUND_AUDIT", "on")
	run := func(reportFound bool) (paidLeases int, lastReason string) {
		wm := secWM(t)
		// Small leases and M keep the real Blake3 hashing cheap (λ≈2 hits per lease).
		wm.defaultBatch, wm.maxClaimBatch, wm.targetMod = 1<<12, 1<<12, 2_000
		addr := "HMC-bbbbbbbbbbbbbbbb"
		for i := 0; i < 80; i++ {
			l := secClaim(t, wm, "w-bwh", addr)
			shares := honestShares(l, l.prev, l.addr, 128)
			req := submitWorkRequest{
				WorkerID: "w-bwh", BaseNonce: l.base, BatchSize: l.size, Attempts: l.size,
				HashrateGHS: 1, ShareNonces: shares,
			}
			if reportFound {
				for n := l.base; n < l.base+l.size; n++ {
					if chain.PohWorkBlake3(l.prev, l.addr, n)%l.mod == 0 {
						req.Found, req.FoundNonce, req.ResultHash = true, n, "rh-"+string(rune('a'+i%26))+addr+string(rune(i))
						break
					}
				}
			}
			_, reason, payout, _, _ := wm.submit(req)
			lastReason = reason
			if payout > 0 {
				paidLeases++
			}
			wm.tipHash = secTip // claim path may refresh tip; keep leases on one tip
		}
		return
	}
	honestPaid, _ := run(true)
	if honestPaid < 75 {
		t.Fatalf("honest miner paid only %d/80 leases", honestPaid)
	}
	hidePaid, reason := run(false)
	if hidePaid > 40 || reason != "found_rate_too_low_update_miner" {
		t.Fatalf("withholder paid %d/80 leases (last reason %q)", hidePaid, reason)
	}
	// log mode: counted and journaled, never rejected.
	t.Setenv("HACKME_COORDINATOR_FOUND_AUDIT", "log")
	if logPaid, _ := run(false); logPaid < 75 {
		t.Fatalf("log mode must not cut pay, paid %d/80", logPaid)
	}
}
