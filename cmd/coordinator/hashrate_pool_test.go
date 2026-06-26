package main

import "testing"

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

func TestWorkerRateLimitPerMinGPUMinFloor(t *testing.T) {
	wm := &workManager{
		claimPerMin: 6000,
		worker: map[string]workerPayoutStat{
			"gpu-rig": {LastHashrateGHS: 0.002, PeakHashrateGHS: 0.004},
		},
	}
	lim := wm.workerRateLimitPerMin("gpu-rig", wm.claimPerMin)
	if lim < 120 {
		t.Fatalf("GPU pool rig should not be throttled to %d/min", lim)
	}
}

func TestSmoothWorkerHashrateGHSRecoversFromStuckLow(t *testing.T) {
	got := smoothWorkerHashrateGHS(0.002, 35.0)
	if got < 20 {
		t.Fatalf("expected fast recovery toward sample, got %v", got)
	}
}

func TestSubmitPrefersReportedHashrateOverWallGH(t *testing.T) {
	wm := newWorkManagerFromEnv()
	now := int64(1_800_000_000)
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
		t.Fatalf("coordinator should record reported GH/s, got %v", st.LastHashrateGHS)
	}
	_ = now
}
