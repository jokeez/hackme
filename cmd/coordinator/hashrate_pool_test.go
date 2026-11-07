package main

import (
	"math"
	"testing"
	"time"
)

func TestClampWorkerHashrateGHSAllowsRealGPUClass(t *testing.T) {
	// Real RTX-class OpenCL/CUDA calibrations land ~140–150 GH/s; the old 128 cap
	// silently clipped billing/retarget. Ceiling is 512 (500/512 family).
	cases := []struct {
		in, want float64
	}{
		{0, 0},
		{-1, 0},
		{math.NaN(), 0},
		{math.Inf(1), 0},
		{1.5, 1.5},
		{128, 128},
		{149.5, 149.5},
		{500, 500},
		{512, 512},
		{513, 512},
		{1000, 512},
	}
	for _, tc := range cases {
		got := clampWorkerHashrateGHS(tc.in)
		if math.IsNaN(tc.want) {
			if !math.IsNaN(got) {
				t.Fatalf("in=%v got %v want NaN", tc.in, got)
			}
			continue
		}
		if got != tc.want {
			t.Fatalf("in=%v got %v want %v", tc.in, got, tc.want)
		}
	}
	if maxWorkerHashrateGHS != 512 {
		t.Fatalf("maxWorkerHashrateGHS=%v want 512", maxWorkerHashrateGHS)
	}
}

func TestEffectiveWorkerHashrateGHSUsesPeakWhenEMACollapsed(t *testing.T) {
	st := workerPayoutStat{LastHashrateGHS: 0.002, PeakHashrateGHS: 36.4}
	got := effectiveWorkerHashrateGHS(st)
	want := 36.4 * 0.72
	if got < want*0.99 || got > want*1.01 {
		t.Fatalf("effective=%v want~%v", got, want)
	}
}

func TestEffectiveWorkerHashrateGHSKeepsLiveSample(t *testing.T) {
	st := workerPayoutStat{LastHashrateGHS: 28.5, PeakHashrateGHS: 36.0}
	if got := effectiveWorkerHashrateGHS(st); got != 28.5 {
		t.Fatalf("got %v want 28.5", got)
	}
}

func TestEffectiveWorkerHashrateGHSIgnoresBlake3CliffPeak(t *testing.T) {
	st := workerPayoutStat{LastHashrateGHS: 0.27, PeakHashrateGHS: 93.8}
	if got := effectiveWorkerHashrateGHS(st); got != 0.27 {
		t.Fatalf("got %v want 0.27 (no pre-Blake3 peak resurrect)", got)
	}
}

func TestWorkerRateLimitPerMinGPUMinFloor(t *testing.T) {
	wm := &workManager{
		claimPerMin: 6000,
		worker: map[string]workerPayoutStat{
			"gpu-rig": {LastHashrateGHS: 4.0, PeakHashrateGHS: 4.0},
		},
	}
	lim := wm.workerRateLimitPerMin("gpu-rig", wm.claimPerMin)
	if lim < 120 {
		t.Fatalf("live GPU (≥1 GH/s) must not be throttled below 120/min, got %d", lim)
	}
}

func TestWorkerRateLimitPerMinSubOneNotRaisedToFloor(t *testing.T) {
	wm := &workManager{
		claimPerMin: 6000,
		worker: map[string]workerPayoutStat{
			"cpu-spam": {LastHashrateGHS: 0.002, PeakHashrateGHS: 0.004},
		},
	}
	lim := wm.workerRateLimitPerMin("cpu-spam", wm.claimPerMin)
	if lim >= 120 {
		t.Fatalf("sub-1 GH/s must keep spam cap, got %d/min", lim)
	}
	if lim < 2 {
		t.Fatalf("sub-1 GH/s floor is 2/min, got %d", lim)
	}
}

func TestSmoothWorkerHashrateGHSRecoversFromStuckLow(t *testing.T) {
	got := smoothWorkerHashrateGHS(0.002, 35.0)
	if got < 20 {
		t.Fatalf("expected fast recovery toward sample, got %v", got)
	}
}

func TestSmoothWorkerHashrateGHSSnapsBlake3Cliff(t *testing.T) {
	// Pre-Blake3 calib EMA (~100 GH/s) must not bleed into a Blake3 wall sample.
	got := smoothWorkerHashrateGHS(100.0, 0.27)
	if got != 0.27 {
		t.Fatalf("cliff snap want 0.27, got %v", got)
	}
	// Modest drops still EMA (not a cliff).
	mixed := smoothWorkerHashrateGHS(30.0, 20.0)
	if mixed <= 20 || mixed >= 30 {
		t.Fatalf("non-cliff should blend, got %v", mixed)
	}
}

func TestSubmitRecordsWallHashrateWithoutTrustingReport(t *testing.T) {
	// Parent shells / desktop env must not force hybrid-strict on unit tests.
	t.Setenv("HACKME_POOL_HYBRID_SIGNER_ENABLED", "")
	t.Setenv("HACKME_POOL_HYBRID_SIGNER_STRICT", "")
	t.Setenv("HACKME_COORDINATOR_SHARE_STRICT", "0")
	wm := newWorkManagerFromEnv()
	wm.shareStrict = false
	wm.pohFormulaVersion = 1
	base, batch, _, _, _, ok, _ := wm.claim("w-gh", 0)
	if !ok {
		t.Fatal("claim failed")
	}
	req := submitWorkRequest{
		WorkerID:    "w-gh",
		BaseNonce:   base,
		BatchSize:   batch,
		Attempts:    batch,
		HashrateGHS: 32.5,
	}
	_, reason, _, _, _ := wm.submit(req)
	if reason != "" {
		t.Fatalf("submit failed: %s", reason)
	}
	st := wm.worker["w-gh"]
	if st.LastHashrateGHS < 10 {
		t.Fatalf("pre-Blake3 should record reported GH/s, got %v", st.LastHashrateGHS)
	}
}

func TestBlake3SubmitIgnoresFORCEReport(t *testing.T) {
	t.Setenv("HACKME_POOL_HYBRID_SIGNER_ENABLED", "")
	t.Setenv("HACKME_POOL_HYBRID_SIGNER_STRICT", "")
	wm := newAbuseTestWM()
	wm.attemptRecheckPct = 0
	wm.shareStrict = false // isolate GH path (no shares_required)
	wm.shareDivisor = 0
	wm.pohFormulaVersion = 3
	wm.tipHeight = 500_000
	wm.tipHash = testTipHash
	wm.worker["w-force"] = workerPayoutStat{
		LastHashrateGHS: 95,
		PeakHashrateGHS: 120,
	}
	base, size, _, _, _, ok, reason := wm.claim("w-force", wm.defaultBatch)
	if !ok {
		t.Fatalf("claim: %s", reason)
	}
	now := time.Now().Unix()
	k := workKey{base: base, batch: size}
	rec := wm.active[k]
	rec.FormulaVersion = 3
	rec.PrevHash = wm.tipHash
	rec.IssuedAt = now - 2
	rec.IssuedAtMs = (now - 2) * 1000
	rec.ExpiresAt = now + 60
	wm.active[k] = rec

	_, subReason, _, _, _ := wm.submit(submitWorkRequest{
		WorkerID:    "w-force",
		BaseNonce:   base,
		BatchSize:   size,
		Attempts:    size,
		Found:       false,
		HashrateGHS: 500, // FORCE_HASHRATE-style inflate — must be ignored
	})
	if subReason != "" {
		t.Fatalf("submit: %s", subReason)
	}
	st := wm.worker["w-force"]
	// Wall for defaultBatch/2s ≈ 0.002 GH/s; Blake3 must not keep FORCE/calib.
	if st.LastHashrateGHS > 1 {
		t.Fatalf("blake3 must ignore FORCE report, Last=%v", st.LastHashrateGHS)
	}
	if st.PeakHashrateGHS > st.LastHashrateGHS*1.01 && st.LastHashrateGHS > 0 {
		t.Fatalf("sticky pre-Blake3 peak must drop, peak=%v last=%v", st.PeakHashrateGHS, st.LastHashrateGHS)
	}
}

func TestSoftAttemptPayCappedByClaimedGHS(t *testing.T) {
	t.Setenv("HACKME_POOL_HYBRID_SIGNER_ENABLED", "")
	t.Setenv("HACKME_POOL_HYBRID_SIGNER_STRICT", "")
	wm := newAbuseTestWM()
	wm.attemptRecheckPct = 0
	wm.rewardPerM = 0.01
	base, size, _, _, _, ok, reason := wm.claim("w-soft", wm.defaultBatch)
	if !ok {
		t.Fatalf("claim: %s", reason)
	}
	now := time.Now().Unix()
	k := workKey{base: base, batch: size}
	rec := wm.active[k]
	rec.IssuedAt = now - 1
	rec.ExpiresAt = now + 60
	wm.active[k] = rec

	// Tiny claimed GH/s: maxCred = gh*1e9*elapsed*1.15 << defaultBatch.
	const claimGH = 0.001
	_, subReason, payout, _, _ := wm.submit(submitWorkRequest{
		WorkerID:    "w-soft",
		BaseNonce:   base,
		BatchSize:   size,
		Attempts:    size,
		Found:       false,
		HashrateGHS: claimGH,
	})
	if subReason != "" {
		t.Fatalf("submit: %s", subReason)
	}
	st := wm.worker["w-soft"]
	maxCred := uint64(claimGH * 1e9 * 1.0 * 1.15)
	if maxCred < 1 {
		t.Fatal("test setup: maxCred too small")
	}
	if st.AcceptedAtt > maxCred+1 {
		t.Fatalf("soft attempt-pay must cap by GH×elapsed: accepted=%d maxCred≈%d", st.AcceptedAtt, maxCred)
	}
	maxPay := (float64(maxCred) / 1_000_000.0) * wm.rewardPerM * 1.05
	if payout > maxPay+1e-9 {
		t.Fatalf("payout=%v exceeds soft GH cap max≈%v", payout, maxPay)
	}
	fullBatchPay := (float64(size) / 1_000_000.0) * wm.rewardPerM
	if payout >= fullBatchPay*0.99 {
		t.Fatalf("expected soft GH cap below full-batch pay, payout=%v full=%v", payout, fullBatchPay)
	}
}
