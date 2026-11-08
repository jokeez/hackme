package main

import (
	"testing"
	"time"

	"hackme/internal/chain"
)

func TestShareModForLeaseStarvesNotOnSolvencyM(t *testing.T) {
	// Live bug class: M ≈ Hs×30s (~5e11) made M/1024 ≫ batch → zero expected shares.
	const hugeM = uint64(500_000_000_000)
	const batch = uint64(4_194_304)
	sm := shareModForLease(hugeM, 1024, batch)
	leaseSM := batch / targetSharesPerLease
	if sm != leaseSM {
		t.Fatalf("shareMod=%d want lease scale %d (not M/div=%d)", sm, leaseSM, hugeM/1024)
	}
	if expectedSharesForMod(batch, sm) < 1 {
		t.Fatalf("expected shares %.4f must be ≥1", expectedSharesForMod(batch, sm))
	}
	// Normal M: still harden large leases above M/div when leaseSM is harder.
	mod := uint64(5_000_000)
	sm2 := shareModForLease(mod, 1024, batch)
	if sm2 < leaseSM {
		t.Fatalf("large lease should harden to ≥%d, got %d", leaseSM, sm2)
	}
}

func TestValidateShareNoncesV1(t *testing.T) {
	mod := uint64(1_000_003)
	div := uint64(1024)
	sm := shareModFor(mod, div)
	if sm == 0 {
		t.Fatal("share mod")
	}
	base := uint64(0)
	batch := uint64(50_000)
	var hit uint64
	for n := base; n < base+batch; n++ {
		if chain.PohEval(n)%sm == 0 {
			hit = n
			break
		}
	}
	if hit == 0 {
		t.Fatal("no share hit")
	}
	ok, rej := validateShareNonces([]uint64{hit, hit, base + batch + 1, hit + 1}, base, batch, sm, 1, "", "")
	if len(ok) != 1 || ok[0] != hit {
		t.Fatalf("accepted=%v rej=%+v", ok, rej)
	}
	if rej.Total() < 1 || rej.Dup < 1 || rej.OutOfRange < 1 || rej.BadPoH < 1 {
		t.Fatalf("expected dup+oor+bad_poh rejects, got %+v", rej)
	}
}

func TestValidateShareNoncesCapDoesNotInflateRejects(t *testing.T) {
	mod := uint64(997)
	sm := shareModFor(mod, 1024)
	base := uint64(0)
	batch := uint64(2_000_000)
	var hits []uint64
	for n := base; n < base+batch && len(hits) < maxSharesPerSubmit+40; n++ {
		if chain.PohEval(n)%sm == 0 {
			hits = append(hits, n)
		}
	}
	if len(hits) < maxSharesPerSubmit+10 {
		t.Fatalf("need many share hits, got %d", len(hits))
	}
	ok, rej := validateShareNonces(hits, base, batch, sm, 1, "", "")
	if len(ok) != maxSharesPerSubmit {
		t.Fatalf("accepted=%d want %d", len(ok), maxSharesPerSubmit)
	}
	if rej.Total() != 0 {
		t.Fatalf("cap drop must not count as PoH rejects, got rej=%+v", rej)
	}
}

func TestSoftShareKeepsAttemptPay(t *testing.T) {
	wm := withTestTip(&workManager{
		defaultBatch:         10_000,
		targetMod:            10_000_000, // ≥ lease so M economic cap does not bind attempt-pay
		leaseSec:             60,
		rewardPerM:           1.0,
		baseRewardHMC:        0.01,
		foundBonus:           0.0,
		shareDivisor:         1024,
		shareStrict:          false,
		shareRateMin:         0.30,
		shareRateMinExpected: 8.0,
		shareRateReject:      false,
		maxWorkers:           1000,
		maxActiveLeases:      1000,
		maxDedupEntries:      1000,
		active:               make(map[workKey]leaseRecord),
		worker:               make(map[string]workerPayoutStat),
		acceptedResultHashes: make(map[string]struct{}),
		acceptedFoundNonces:  make(map[uint64]struct{}),
	})
	base, size, _, mod, _, ok, reason := wm.claim("w-share-soft", 10_000)
	if !ok {
		t.Fatalf("claim: %s", reason)
	}
	sm := shareModFor(mod, wm.shareDivisor)
	var shares []uint64
	for n := base; n < base+size && len(shares) < 3; n++ {
		if chain.PohEval(n)%sm == 0 {
			shares = append(shares, n)
		}
	}
	if len(shares) == 0 {
		t.Skip("no shares in lease")
	}
	_, reasonSub, payout, _, _ := wm.submit(submitWorkRequest{
		WorkerID:    "w-share-soft",
		BaseNonce:   base,
		BatchSize:   size,
		WorkID:      buildWorkID("w-share-soft", base, size),
		Attempts:    size,
		Found:       false,
		HashrateGHS: 1.0,
		ShareNonces: shares,
	})
	if reasonSub != "" {
		t.Fatalf("submit failed: %s", reasonSub)
	}
	// Soft: attempt-pay, not share-pay (audit: share-pay underpays honest CPU harvest).
	wantAttempt := (float64(size) / 1_000_000.0) * 1.0
	sharePay := float64(len(shares)) * rewardPerShare(0.01, 1024)
	if payout < wantAttempt*0.99 || payout > wantAttempt*1.01 {
		t.Fatalf("payout=%v want attempt~%v (share would be %v)", payout, wantAttempt, sharePay)
	}
	if wm.sharePaidSubmits != 0 {
		t.Fatalf("soft must not share-pay, paid_submits=%d", wm.sharePaidSubmits)
	}
	if wm.shareAccepts < uint64(len(shares)) {
		t.Fatalf("share accepts=%d want>=%d", wm.shareAccepts, len(shares))
	}
	st := wm.worker["w-share-soft"]
	if st.ShareAccepted < uint64(len(shares)) || st.ShareExpected <= 0 {
		t.Fatalf("share stats accepted=%d expected=%v", st.ShareAccepted, st.ShareExpected)
	}
}

func TestStrictSharePayoutPath(t *testing.T) {
	wm := withTestTip(&workManager{
		defaultBatch:         10_000,
		targetMod:            997,
		leaseSec:             60,
		rewardPerM:           1.0,
		baseRewardHMC:        0.01,
		foundBonus:           0.0,
		shareDivisor:         1024,
		shareStrict:          true,
		maxWorkers:           1000,
		maxActiveLeases:      1000,
		maxDedupEntries:      1000,
		active:               make(map[workKey]leaseRecord),
		worker:               make(map[string]workerPayoutStat),
		acceptedResultHashes: make(map[string]struct{}),
		acceptedFoundNonces:  make(map[uint64]struct{}),
	})
	base, size, _, mod, _, ok, reason := wm.claim("w-share-strict", 10_000)
	if !ok {
		t.Fatalf("claim: %s", reason)
	}
	sm := shareModForLease(mod, wm.shareDivisor, size)
	var shares []uint64
	for n := base; n < base+size && len(shares) < 3; n++ {
		if chain.PohEval(n)%sm == 0 {
			shares = append(shares, n)
		}
	}
	if len(shares) == 0 {
		t.Skip("no shares in lease")
	}
	_, reasonSub, payout, _, _ := wm.submit(submitWorkRequest{
		WorkerID:    "w-share-strict",
		BaseNonce:   base,
		BatchSize:   size,
		WorkID:      buildWorkID("w-share-strict", base, size),
		Attempts:    size,
		Found:       false,
		HashrateGHS: 1.0,
		ShareNonces: shares,
	})
	if reasonSub != "" {
		t.Fatalf("submit failed: %s", reasonSub)
	}
	want := float64(len(shares)) * rewardPerShareAt(0.01, sm, mod)
	if payout < want*0.99 || payout > want*1.01 {
		t.Fatalf("payout=%v want~%v", payout, want)
	}
	if wm.sharePaidSubmits < 1 {
		t.Fatalf("strict should share-pay, paid=%d", wm.sharePaidSubmits)
	}
}

func TestShareRateReject(t *testing.T) {
	wm := withTestTip(&workManager{
		defaultBatch:         10_000,
		targetMod:            997,
		leaseSec:             60,
		rewardPerM:           1.0,
		baseRewardHMC:        0.01,
		shareDivisor:         1024,
		shareStrict:          false,
		shareRateMin:         0.30,
		shareRateMinExpected: 2.0,
		shareRateReject:      true,
		maxWorkers:           1000,
		maxActiveLeases:      1000,
		maxDedupEntries:      1000,
		active:               make(map[workKey]leaseRecord),
		worker:               make(map[string]workerPayoutStat),
		acceptedResultHashes: make(map[string]struct{}),
		acceptedFoundNonces:  make(map[uint64]struct{}),
	})
	// Seed low share rate without rejecting yet.
	wm.worker["w-rate"] = workerPayoutStat{ShareExpected: 1.5, ShareAccepted: 0}
	base, size, _, _, _, ok, reason := wm.claim("w-rate", 10_000)
	if !ok {
		t.Fatalf("claim: %s", reason)
	}
	// expectedShares(10000, ~997, 1024) ≈ 10k*1024/997 ≫ 2; no shares → reject
	_, reasonSub, _, _, _ := wm.submit(submitWorkRequest{
		WorkerID:    "w-rate",
		BaseNonce:   base,
		BatchSize:   size,
		WorkID:      buildWorkID("w-rate", base, size),
		Attempts:    size,
		Found:       false,
		HashrateGHS: 1.0,
		ShareNonces: nil,
	})
	if reasonSub != "share_rate_low" {
		t.Fatalf("want share_rate_low, got %q", reasonSub)
	}
	if wm.shareRateLowRejects < 1 {
		t.Fatalf("rejects=%d", wm.shareRateLowRejects)
	}
}

func TestExpectedShares(t *testing.T) {
	// attempts * 1024 / M
	got := expectedShares(4_194_304, 972_000_000, 1024)
	if got < 4.4 || got > 4.5 {
		t.Fatalf("expected~4.42 got %v", got)
	}
	if expectedShares(0, 997, 1024) != 0 || expectedShares(1000, 0, 1024) != 0 {
		t.Fatal("zero cases")
	}
}

func TestSoftShareScanAttempts(t *testing.T) {
	if softShareScanAttempts(4_194_304, false) != shareHarvestScanCap {
		t.Fatalf("soft must cap to %d", shareHarvestScanCap)
	}
	if softShareScanAttempts(4_194_304, true) != shareHashBudget {
		t.Fatalf("strict must cap to shareHashBudget=%d", shareHashBudget)
	}
	if softShareScanAttempts(1000, false) != 1000 {
		t.Fatal("under cap unchanged")
	}
}

func TestShareModForLeaseScalesWithBatch(t *testing.T) {
	mod := uint64(2_000_000)
	div := uint64(1024)
	easy := shareModFor(mod, div)
	big := shareModForLease(mod, div, 64<<30) // 64G lease
	if big <= easy {
		t.Fatalf("large lease shareMod=%d should exceed M/div=%d", big, easy)
	}
	want := (64 << 30) / targetSharesPerLease
	if big != want {
		t.Fatalf("shareMod=%d want batch/targetShares=%d", big, want)
	}
	small := shareModForLease(mod, div, 10_000)
	if small != easy && small != 10_000/targetSharesPerLease {
		t.Fatalf("small lease unexpected shareMod=%d easy=%d", small, easy)
	}
}

func TestSoftShareExpectedUsesScanCap(t *testing.T) {
	wm := withTestTip(&workManager{
		defaultBatch:         4_194_304,
		maxClaimBatch:        4_194_304,
		targetMod:            972_000_000,
		leaseSec:             60,
		rewardPerM:           1.0,
		baseRewardHMC:        0.01,
		shareDivisor:         1024,
		shareStrict:          false,
		shareRateMin:         0.30,
		shareRateMinExpected: 100, // do not flag in this unit test
		shareRateReject:      false,
		maxWorkers:           1000,
		maxActiveLeases:      1000,
		maxDedupEntries:      1000,
		active:               make(map[workKey]leaseRecord),
		worker:               make(map[string]workerPayoutStat),
		acceptedResultHashes: make(map[string]struct{}),
		acceptedFoundNonces:  make(map[uint64]struct{}),
	})
	base, size, _, mod, _, ok, reason := wm.claim("w-cap", 4_194_304)
	if !ok {
		t.Fatalf("claim: %s", reason)
	}
	_, reasonSub, _, _, _ := wm.submit(submitWorkRequest{
		WorkerID:    "w-cap",
		BaseNonce:   base,
		BatchSize:   size,
		WorkID:      buildWorkID("w-cap", base, size),
		Attempts:    size,
		Found:       false,
		HashrateGHS: 50.0,
		ShareNonces: nil,
	})
	if reasonSub != "" {
		t.Fatalf("submit: %s", reasonSub)
	}
	st := wm.worker["w-cap"]
	want := expectedShares(shareHarvestScanCap, mod, 1024)
	full := expectedShares(size, mod, 1024)
	if st.ShareExpected < want*0.99 || st.ShareExpected > want*1.01 {
		t.Fatalf("expected=%v want capped~%v (full would be %v)", st.ShareExpected, want, full)
	}
	if st.ShareExpected > full*0.2 {
		t.Fatalf("soft expected must be far below full-lease mean")
	}
}

func TestRewardPerShare(t *testing.T) {
	if rewardPerShare(0.01, 1024) <= 0 {
		t.Fatal("zero")
	}
	if rewardPerShare(0.01, 1) != 0 {
		t.Fatal("divisor<2")
	}
}

func TestBlake3StrictPPSIgnoresReportedGHS(t *testing.T) {
	wm := withTestTip(&workManager{
		defaultBatch:         1 << 20,
		maxClaimBatch:        1 << 26,
		targetMod:            2_000_000,
		leaseSec:             60,
		leaseWorkSec:         3,
		rewardPerM:           1.0,
		baseRewardHMC:        0.01,
		foundBonus:           0,
		shareDivisor:         1024,
		shareStrict:          true,
		pohFormulaVersion:    1, // PohEval shares in this unit test
		maxWorkers:           100,
		maxActiveLeases:      100,
		maxDedupEntries:      1000,
		active:               make(map[workKey]leaseRecord),
		worker:               make(map[string]workerPayoutStat),
		acceptedResultHashes: make(map[string]struct{}),
		acceptedFoundNonces:  make(map[uint64]struct{}),
	})
	// Seed credited GH so adaptClaimBatch grows past default (simulates prior honest wall).
	wm.worker["w-pps"] = workerPayoutStat{LastHashrateGHS: 20}
	base, size, _, mod, _, ok, reason := wm.claim("w-pps", 1<<22)
	if !ok {
		t.Fatalf("claim: %s", reason)
	}
	if size < 1<<22 {
		t.Fatalf("adaptive lease too small: %d", size)
	}
	sm := shareModForLease(mod, wm.shareDivisor, size)
	var shares []uint64
	for n := base; n < base+size && len(shares) < 8; n++ {
		if chain.PohEval(n)%sm == 0 {
			shares = append(shares, n)
		}
	}
	if len(shares) == 0 {
		t.Skip("no shares in lease")
	}
	k := workKey{base: base, batch: size}
	rec := wm.active[k]
	rec.IssuedAtMs = time.Now().UnixMilli() - 2500 // 2.5s wall → share GH credible
	rec.IssuedAt = time.Now().Unix() - 3
	wm.active[k] = rec

	_, reasonSub, payout, _, _ := wm.submit(submitWorkRequest{
		WorkerID:    "w-pps",
		BaseNonce:   base,
		BatchSize:   size,
		Attempts:    size,
		Found:       false,
		HashrateGHS: 500, // FORCE — must not drive pay or LastHashrate
		ShareNonces: shares,
	})
	if reasonSub != "" {
		t.Fatalf("submit: %s", reasonSub)
	}
	want := float64(len(shares)) * rewardPerShareAt(0.01, sm, mod)
	if payout < want*0.99 || payout > want*1.01 {
		t.Fatalf("PPS payout=%v want~%v", payout, want)
	}
	st := wm.worker["w-pps"]
	if st.LastHashrateGHS >= 100 {
		t.Fatalf("must not credit FORCE report, Last=%v", st.LastHashrateGHS)
	}
	// EMA may retain part of seeded 20 GH/s; raw must come from shares/wall not 500.
	if st.LastHashrateGHS <= 0 {
		t.Fatalf("expected share/wall GH credit, Last=%v", st.LastHashrateGHS)
	}
}

func TestHonestWallHashrateCapsEarlyFoundBatch(t *testing.T) {
	// 64G lease submitted after ~2s with prev ~7 GH/s must not credit ~32 GH/s.
	got := honestWallHashrateGHS(1<<36, 2.0, 7.0)
	if got > 10 {
		t.Fatalf("early-found wall GH inflated: got %v want ≤~8.75", got)
	}
	if got < 5 {
		t.Fatalf("should credit near prev ceiling, got %v", got)
	}
	raw := honestWallHashrateGHS(1<<22, 2.0, 7.0) // 4M / 2s ≈ 0.002
	if raw > 1 {
		t.Fatalf("small lease wall unexpected %v", raw)
	}
}

func TestAdaptClaimBatchBootstrapsBlake3(t *testing.T) {
	wm := &workManager{
		defaultBatch:      1 << 22,
		maxClaimBatch:     1 << 36,
		leaseWorkSec:      3,
		pohFormulaVersion: 3,
		worker:            map[string]workerPayoutStat{},
	}
	got := wm.adaptClaimBatch("new-gpu", 1<<26)
	if got < blake3BootstrapBatch {
		t.Fatalf("bootstrap want>=%d got=%d", blake3BootstrapBatch, got)
	}
	if got > blake3BootstrapBatch*2 {
		t.Fatalf("cold bootstrap too large got=%d (CPU would expire lease)", got)
	}
	wm.worker["hot"] = workerPayoutStat{LastHashrateGHS: 30}
	got = wm.adaptClaimBatch("hot", 1<<26)
	want := uint64(30 * 1e9 * 3)
	if want > wm.maxClaimBatch {
		want = wm.maxClaimBatch
	}
	if got != want {
		t.Fatalf("adaptive from GH: got=%d want=%d", got, want)
	}
	// Slow CPU: size to ~leaseWorkSec of measured GH, not sticky 8G.
	wm.worker["cpu"] = workerPayoutStat{LastHashrateGHS: 0.1}
	got = wm.adaptClaimBatch("cpu", 1<<22)
	wantCPU := uint64(0.1 * 1e9 * 3)
	if got != wantCPU {
		t.Fatalf("slow CPU adaptive: got=%d want=%d", got, wantCPU)
	}
}

func TestShareImpliedGHSIgnoresSingleHugeShare(t *testing.T) {
	sm := uint64(2 << 30) // 2G — old stride harvest noise
	if g := shareImpliedGHS(1, sm, 3.0); g != 0 {
		t.Fatalf("single huge-share sample must be ignored, got %v", g)
	}
	if g := shareImpliedGHS(8, sm, 3.0); g <= 0 {
		t.Fatalf("multi-share sample should credit GH, got %v", g)
	}
}

func TestWarmLowShareImpliedGHS(t *testing.T) {
	if g := warmLowShareImpliedGHS(0.012, 1); g < 0.1 || g > 2 {
		t.Fatalf("low sample should warm into 0.1–2 band, got %v", g)
	}
	if g := warmLowShareImpliedGHS(0.2, 4); g != 0.2 {
		t.Fatalf("already-visible sample must pass through, got %v", g)
	}
	if g := warmLowShareImpliedGHS(0, 1); g != 0 {
		t.Fatalf("zero stays zero, got %v", g)
	}
}
