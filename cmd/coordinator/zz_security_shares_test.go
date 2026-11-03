package main

import (
	"testing"
	"time"

	"hackme/internal/chain"
)

// Adversarial share-PPS tests: every case is an attempt to get paid without doing the
// Blake3 work bound to this lease (tip, miner address, nonce range).

const secTip = "abababababababababababababababababababababababababababababababab"

func secWM(t *testing.T) *workManager {
	t.Helper()
	t.Setenv("HACKME_COORDINATOR_POOL_DAILY_BUDGET_HMC", "0")
	return &workManager{
		defaultBatch:         1 << 16,
		maxClaimBatch:        1 << 20,
		targetMod:            2_000_000,
		leaseSec:             60,
		leaseWorkSec:         3,
		rewardPerM:           1.0,
		baseRewardHMC:        0.01,
		shareDivisor:         1024,
		shareStrict:          true,
		pohFormulaVersion:    3,
		tipHash:              secTip,
		tipHeight:            495_000,
		maxWorkers:           100,
		maxActiveLeases:      100,
		maxDedupEntries:      1000,
		active:               make(map[workKey]leaseRecord),
		worker:               make(map[string]workerPayoutStat),
		acceptedResultHashes: make(map[string]struct{}),
		acceptedFoundNonces:  make(map[uint64]struct{}),
	}
}

type secLease struct {
	base, size, mod, sm uint64
	prev, addr          string
}

func secClaim(t *testing.T, wm *workManager, worker, addr string) secLease {
	t.Helper()
	wm.worker[worker] = workerPayoutStat{PayoutAddress: addr}
	base, size, _, mod, _, ok, reason := wm.claim(worker, wm.defaultBatch)
	if !ok {
		t.Fatalf("claim: %s", reason)
	}
	k := workKey{base: base, batch: size}
	rec := wm.active[k]
	rec.IssuedAtMs = time.Now().UnixMilli() - 2000
	rec.IssuedAt = time.Now().Unix() - 2
	wm.active[k] = rec
	sm := rec.ShareMod
	if sm == 0 {
		sm = shareModForLease(mod, wm.shareDivisor, size)
	}
	return secLease{base: base, size: size, mod: mod, sm: sm, prev: rec.PrevHash, addr: rec.MinerAddress}
}

// honestShares returns real Blake3 shares for (prev, addr) inside the lease range.
func honestShares(l secLease, prev, addr string, max int) []uint64 {
	var out []uint64
	for n := l.base; n < l.base+l.size && len(out) < max; n++ {
		if chain.PohWorkBlake3(prev, addr, n)%l.sm == 0 {
			out = append(out, n)
		}
	}
	return out
}

func secSubmit(wm *workManager, worker string, l secLease, shares []uint64) (string, float64) {
	_, reason, payout, _, _ := wm.submit(submitWorkRequest{
		WorkerID: worker, BaseNonce: l.base, BatchSize: l.size, Attempts: l.size,
		HashrateGHS: 1, ShareNonces: shares,
	})
	return reason, payout
}

func TestSecurityForgedSharesNotPaid(t *testing.T) {
	wm := secWM(t)
	l := secClaim(t, wm, "w-forge", "HMC-aaaaaaaaaaaaaaaa")
	var forged []uint64
	for n := l.base; n < l.base+l.size && len(forged) < 20; n++ {
		if chain.PohWorkBlake3(l.prev, l.addr, n)%l.sm != 0 {
			forged = append(forged, n)
		}
	}
	_, payout := secSubmit(wm, "w-forge", l, forged)
	if payout != 0 || wm.shareAccepts != 0 {
		t.Fatalf("forged shares paid %v (accepted %d)", payout, wm.shareAccepts)
	}
}

func TestSecurityOutOfRangeAndDuplicateShares(t *testing.T) {
	wm := secWM(t)
	l := secClaim(t, wm, "w-oor", "HMC-aaaaaaaaaaaaaaaa")
	good := honestShares(l, l.prev, l.addr, 2)
	if len(good) == 0 {
		t.Skip("no shares in lease")
	}
	// Valid work but outside this lease's range (another range's shares).
	outside := honestShares(secLease{base: l.base + l.size, size: l.size, sm: l.sm}, l.prev, l.addr, 3)
	shares := append([]uint64{good[0], good[0], good[0]}, outside...)
	_, payout := secSubmit(wm, "w-oor", l, shares)
	want := rewardPerShareAt(wm.baseRewardHMC, l.sm, l.mod)
	if payout > want*1.01 {
		t.Fatalf("duplicates/out-of-range paid %v, want at most one share %v", payout, want)
	}
}

func TestSecurityReplayNotPaidTwice(t *testing.T) {
	wm := secWM(t)
	l := secClaim(t, wm, "w-replay", "HMC-aaaaaaaaaaaaaaaa")
	shares := honestShares(l, l.prev, l.addr, 4)
	if len(shares) == 0 {
		t.Skip("no shares in lease")
	}
	_, first := secSubmit(wm, "w-replay", l, shares)
	reason, second := secSubmit(wm, "w-replay", l, shares)
	if first <= 0 {
		t.Fatalf("honest first submit not paid (%v)", first)
	}
	if second != 0 || reason == "" {
		t.Fatalf("replayed submit paid %v (reason %q)", second, reason)
	}
}

func TestSecuritySharesBoundToMinerAddress(t *testing.T) {
	wm := secWM(t)
	l := secClaim(t, wm, "w-addr", "HMC-aaaaaaaaaaaaaaaa")
	stolen := honestShares(l, l.prev, "HMC-bbbbbbbbbbbbbbbb", 10)
	if len(stolen) == 0 {
		t.Skip("no shares")
	}
	_, payout := secSubmit(wm, "w-addr", l, stolen)
	if payout != 0 {
		t.Fatalf("shares computed for another address paid %v", payout)
	}
}

func TestSecuritySharesBoundToTip(t *testing.T) {
	wm := secWM(t)
	l := secClaim(t, wm, "w-tip", "HMC-aaaaaaaaaaaaaaaa")
	pre := honestShares(l, "cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd", l.addr, 10)
	if len(pre) == 0 {
		t.Skip("no shares")
	}
	_, payout := secSubmit(wm, "w-tip", l, pre)
	if payout != 0 {
		t.Fatalf("precomputed shares for another tip paid %v", payout)
	}
}

func TestSecurityLeaseTheft(t *testing.T) {
	wm := secWM(t)
	l := secClaim(t, wm, "w-victim", "HMC-aaaaaaaaaaaaaaaa")
	shares := honestShares(l, l.prev, l.addr, 4)
	if len(shares) == 0 {
		t.Skip("no shares")
	}
	wm.worker["w-thief"] = workerPayoutStat{PayoutAddress: "HMC-cccccccccccccccc"}
	reason, payout := secSubmit(wm, "w-thief", l, shares)
	if payout != 0 {
		t.Fatalf("thief paid %v submitting victim's lease (reason %q)", payout, reason)
	}
	if st := wm.worker["w-thief"]; st.PayoutHMC != 0 {
		t.Fatalf("thief accrued %v", st.PayoutHMC)
	}
}

func TestSecurityBatchSizeTamper(t *testing.T) {
	wm := secWM(t)
	l := secClaim(t, wm, "w-batch", "HMC-aaaaaaaaaaaaaaaa")
	tampered := l
	tampered.size = l.size * 64 // try to change share difficulty / range
	shares := honestShares(l, l.prev, l.addr, 4)
	_, reason, payout, _, _ := wm.submit(submitWorkRequest{
		WorkerID: "w-batch", BaseNonce: tampered.base, BatchSize: tampered.size, Attempts: tampered.size,
		HashrateGHS: 1, ShareNonces: shares,
	})
	if payout != 0 {
		t.Fatalf("tampered batch_size paid %v (reason %q)", payout, reason)
	}
}

func TestSecurityShareFloodCapped(t *testing.T) {
	wm := secWM(t)
	l := secClaim(t, wm, "w-flood", "HMC-aaaaaaaaaaaaaaaa")
	shares := honestShares(l, l.prev, l.addr, maxSharesPerSubmit*3)
	_, payout := secSubmit(wm, "w-flood", l, shares)
	ceil := wm.maxSubmitHMCForBatch(l.size)
	if payout > ceil*1.0001 {
		t.Fatalf("flooded shares paid %v above per-lease ceiling %v", payout, ceil)
	}
}

// Fail-open: a coordinator that does not know the tip yet (fresh restart, node unreachable)
// must not hand out leases whose shares are checked with the algebraic v1 formula (7n+13),
// which needs no work at all.
func TestSecurityNoAlgebraicLeasesWithoutTip(t *testing.T) {
	wm := secWM(t)
	wm.tipHash = ""
	wm.tipHeight = 0
	wm.pohFormulaVersion = 0
	wm.worker["w-open"] = workerPayoutStat{PayoutAddress: "HMC-aaaaaaaaaaaaaaaa"}
	base, size, _, mod, _, ok, _ := wm.claim("w-open", 1<<16)
	if !ok {
		return // refusing to lease without a tip is the safe outcome
	}
	rec := wm.active[workKey{base: base, batch: size}]
	sm := rec.ShareMod
	if sm == 0 {
		sm = shareModForLease(mod, wm.shareDivisor, size)
	}
	var free []uint64
	for n := base; n < base+size && len(free) < 8; n++ {
		if chain.PohEval(n)%sm == 0 { // 7n+13: zero work
			free = append(free, n)
		}
	}
	// Tip arrives afterwards (normal refresh) — the old lease must still not pay algebraic shares.
	wm.tipHash, wm.tipHeight, wm.pohFormulaVersion = secTip, 495_000, 3
	_, _, payout, _, _ := wm.submit(submitWorkRequest{
		WorkerID: "w-open", BaseNonce: base, BatchSize: size, Attempts: size, HashrateGHS: 1, ShareNonces: free,
	})
	if payout != 0 {
		t.Fatalf("algebraic (no-work) shares paid %v on a lease issued without tip (fv=%d)", payout, rec.FormulaVersion)
	}
}

// Regression (2026-10-28 outage): the tip_unavailable early return in claim leaked m.mu, so the
// first claim after a coordinator restart (before the tip refresh) deadlocked the whole pool.
func TestSecurityClaimWithoutTipDoesNotDeadlock(t *testing.T) {
	wm := secWM(t)
	wm.tipHash, wm.tipHeight = "", 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 3; i++ {
			if _, _, _, _, _, ok, reason := wm.claim("w-notip", 1<<16); ok || reason != "tip_unavailable" {
				t.Errorf("claim %d without tip: ok=%v reason=%q", i, ok, reason)
			}
		}
		_ = wm.stats(false) // takes m.mu as well
		wm.tipHash, wm.tipHeight = secTip, 495_000
		if _, _, _, _, _, ok, reason := wm.claim("w-notip", 1<<16); !ok {
			t.Errorf("claim after tip arrives: %s", reason)
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("claim without tip deadlocked the work manager (m.mu leaked)")
	}
}

// Regression (2026-10-28): SUP accrued from client-reported attempts × the dead attempt-pay
// rate (~59 SUP per real HMC with 2^36 leases). SUP must follow verified share pay only.
func TestSUPFollowsVerifiedSharePayNotClaimedAttempts(t *testing.T) {
	wm := secWM(t)
	wm.rewardPerM = 0.00005 // live legacy rate that used to drive SUP
	wm.supPolicy = supPolicy{Enabled: true, RateOfHMC: 0.08, DailyCapRatio: 0.12, StreakSec: 72 * 3600, StreakBonus: 0.25, StaleRateMax: 0.05, ReducedMult: 0.25}
	wm.supMeta = make(map[string]workerSupMeta)
	wm.abuse = make(map[string]workerAbuseState)
	l := secClaim(t, wm, "w-sup", "HMC-aaaaaaaaaaaaaaaa")
	shares := honestShares(l, l.prev, l.addr, 6)
	if len(shares) == 0 {
		t.Skip("no shares")
	}
	_, reason, payout, _, _ := wm.submit(submitWorkRequest{
		WorkerID: "w-sup", BaseNonce: l.base, BatchSize: l.size, Attempts: 1 << 62, // absurd claim
		HashrateGHS: 1, ShareNonces: shares,
	})
	if reason != "" || payout <= 0 {
		t.Fatalf("submit: reason=%q payout=%v", reason, payout)
	}
	sup := wm.worker["w-sup"].PayoutSUP
	if sup <= 0 || sup > payout*0.08*1.25*1.0001 {
		t.Fatalf("SUP %v for share pay %v: want ≤ 8%% (×streak 1.25), independent of claimed attempts", sup, payout)
	}
	// The SUP basis is exactly the credited share pay (the old basis was attempts × legacy rate).
	if wm.hmcAccruedDay < payout*0.9999 || wm.hmcAccruedDay > payout*1.0001 {
		t.Fatalf("SUP daily basis %v must equal credited share pay %v", wm.hmcAccruedDay, payout)
	}
	if wm.supAccruedDay > wm.hmcAccruedDay*0.12+1e-12 {
		t.Fatalf("SUP day %v above 12%% of real HMC %v", wm.supAccruedDay, wm.hmcAccruedDay)
	}
}
