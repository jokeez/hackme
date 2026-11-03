package main

import (
	"math"
	"path/filepath"
	"testing"

	"hackme/internal/chain"
	"hackme/internal/store"
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

// H2: withhold counters survive coordinator restart via dedupDB.
func TestFoundAuditPersistsAcrossRestart(t *testing.T) {
	t.Setenv("HACKME_COORDINATOR_FOUND_AUDIT", "on")
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "found-audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	wm := &workManager{}
	wm.attachDedupDB(db)
	key := foundAuditKey("HMC-persistpersist01", "w")
	for i := 0; i < 80; i++ {
		wm.noteFoundAuditWorkLocked(key, 32, 2048, 70_000)
	}
	st := wm.foundAudit[key]
	if st == nil || !st.Flagged || st.Expected < foundAuditMinExpected {
		t.Fatalf("setup not flagged: %+v", st)
	}
	wantExp, wantAct := st.Expected, st.Actual

	wm2 := &workManager{}
	wm2.attachDedupDB(db)
	st2 := wm2.foundAudit[key]
	if st2 == nil {
		t.Fatal("found_audit not restored after restart")
	}
	if st2.Expected != wantExp || st2.Actual != wantAct || !st2.Flagged {
		t.Fatalf("restored %+v want expected=%v actual=%v flagged=true", st2, wantExp, wantAct)
	}
	// Still rejects after reload without re-warming the window.
	if !wm2.noteFoundAuditWorkLocked(key, 32, 2048, 70_000) {
		t.Fatal("flagged identity must stay suspicious after restart")
	}
}

func TestFoundAuditUndoHit(t *testing.T) {
	wm := &workManager{}
	key := foundAuditKey("HMC-undo", "w")
	wm.noteFoundAuditHitLocked(key)
	if wm.foundAudit[key].Actual != 1 {
		t.Fatalf("Actual=%v", wm.foundAudit[key].Actual)
	}
	wm.undoFoundAuditHitLocked(key)
	if wm.foundAudit[key].Actual != 0 {
		t.Fatalf("undo left Actual=%v", wm.foundAudit[key].Actual)
	}
	wm.undoFoundAuditHitLocked(key) // no underflow
	if wm.foundAudit[key].Actual != 0 {
		t.Fatalf("underflow Actual=%v", wm.foundAudit[key].Actual)
	}
}

// Rejected founds (duplicate nonce) must not bump Actual — otherwise retries clear withhold flags.
func TestFoundAuditRejectedFoundDoesNotInflateActual(t *testing.T) {
	t.Setenv("HACKME_COORDINATOR_FOUND_AUDIT", "log")
	wm := secWM(t)
	wm.defaultBatch, wm.maxClaimBatch, wm.targetMod = 1<<12, 1<<12, 1
	addr := "HMC-cccccccccccccccc"
	l := secClaim(t, wm, "w-inf", addr)
	shares := honestShares(l, l.prev, l.addr, 64)
	var found uint64
	okHit := false
	for n := l.base; n < l.base+l.size; n++ {
		if chain.PohWorkBlake3(l.prev, l.addr, n)%l.mod == 0 {
			found, okHit = n, true
			break
		}
	}
	if !okHit {
		t.Fatal("no hit")
	}
	req := submitWorkRequest{
		WorkerID: "w-inf", BaseNonce: l.base, BatchSize: l.size, Attempts: l.size,
		HashrateGHS: 1, ShareNonces: shares,
		Found: true, FoundNonce: found, ResultHash: "rh-inf-1",
	}
	_, reason, payout, _, _ := wm.submit(req)
	if reason != "" || payout <= 0 {
		t.Fatalf("first accept failed: %q payout=%v", reason, payout)
	}
	key := foundAuditKey("", "w-inf")
	if st := wm.foundAudit[key]; st == nil || st.Actual != 1 {
		t.Fatalf("after accept: %+v", st)
	}
	// Same found again on a fresh lease: duplicate_found_nonce before accept credit.
	l2 := secClaim(t, wm, "w-inf", addr)
	shares2 := honestShares(l2, l2.prev, l2.addr, 64)
	req2 := submitWorkRequest{
		WorkerID: "w-inf", BaseNonce: l2.base, BatchSize: l2.size, Attempts: l2.size,
		HashrateGHS: 1, ShareNonces: shares2,
		Found: true, FoundNonce: found, ResultHash: "rh-inf-2",
	}
	_, reason2, payout2, _, _ := wm.submit(req2)
	if reason2 != "duplicate_found_nonce" || payout2 != 0 {
		t.Fatalf("want duplicate reject, got reason=%q payout=%v", reason2, payout2)
	}
	if st := wm.foundAudit[key]; st == nil || st.Actual != 1 {
		t.Fatalf("rejected found inflated Actual: %+v", st)
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
