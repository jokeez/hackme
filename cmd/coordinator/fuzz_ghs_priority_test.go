package main

import (
	"testing"
	"time"
)

func TestAllowFuzzClaimByGHSDefersDigOnlyWhenHybridOnline(t *testing.T) {
	t.Setenv("HACKME_FUZZ_CLAIM_GHS_PRIORITY", "1")
	t.Setenv("HACKME_FUZZ_CLAIM_GHS_DIG_ONLY_PCT", "25")
	t.Setenv("HACKME_FUZZ_CLAIM_GHS_EXEMPT_PREFIXES", "") // disable bootstrap exempt for this test
	now := time.Now().Unix()
	wm := &workManager{worker: map[string]workerPayoutStat{
		"gpu-1": {
			LastHashrateGHS:  40,
			PeakHashrateGHS:  40,
			LastSeenUnix:     now,
			LastPoHSeenUnix:  now,
			LastFuzzSeenUnix: now,
		},
		"dig-1": {LastHashrateGHS: 0, LastSeenUnix: now, LastFuzzSeenUnix: now},
	}}
	if ok, _ := wm.allowFuzzClaimByGHS("gpu-1", now); !ok {
		t.Fatal("hybrid must always claim")
	}
	deferHits := 0
	admitHits := 0
	for w := int64(0); w < 40; w++ {
		ok, reason := wm.allowFuzzClaimByGHS("dig-1", now+w*10)
		if ok {
			admitHits++
		} else {
			if reason != "ghs_priority_defer" {
				t.Fatalf("reason=%q", reason)
			}
			deferHits++
		}
	}
	if deferHits == 0 {
		t.Fatal("expected some dig-only defers when hybrid online")
	}
	if admitHits == 0 {
		t.Fatal("dig-only must still get minority admit slots")
	}
}

func TestAllowFuzzClaimBootstrapFuzzExemptFromDefer(t *testing.T) {
	t.Setenv("HACKME_FUZZ_CLAIM_GHS_PRIORITY", "1")
	t.Setenv("HACKME_FUZZ_CLAIM_GHS_DIG_ONLY_PCT", "0") // hard defer everyone else
	t.Setenv("HACKME_FUZZ_CLAIM_GHS_EXEMPT_PREFIXES", "bootstrap-fuzz-")
	now := time.Now().Unix()
	wm := &workManager{worker: map[string]workerPayoutStat{
		"gpu-1": {
			LastHashrateGHS: 40, LastSeenUnix: now,
			LastPoHSeenUnix: now, LastFuzzSeenUnix: now,
		},
		"bootstrap-fuzz-01": {LastHashrateGHS: 0, LastSeenUnix: now, LastFuzzSeenUnix: now},
		"dig-cheap":         {LastHashrateGHS: 0, LastSeenUnix: now, LastFuzzSeenUnix: now},
	}}
	// Stay inside the 90s freshness window so hybrid capacity remains online.
	for w := int64(0); w < 8; w++ {
		ts := now + w*10
		if ok, reason := wm.allowFuzzClaimByGHS("bootstrap-fuzz-01", ts); !ok {
			t.Fatalf("bootstrap-fuzz must be exempt: %s", reason)
		}
		if ok, reason := wm.allowFuzzClaimByGHS("dig-cheap", ts); ok || reason != "ghs_priority_defer" {
			t.Fatalf("non-exempt dig-only must defer: ok=%v reason=%q", ok, reason)
		}
	}
}

func TestAllowFuzzClaimWorkerHuntExemptViaEnv(t *testing.T) {
	t.Setenv("HACKME_FUZZ_CLAIM_GHS_PRIORITY", "1")
	t.Setenv("HACKME_FUZZ_CLAIM_GHS_DIG_ONLY_PCT", "0")
	t.Setenv("HACKME_FUZZ_CLAIM_GHS_EXEMPT_PREFIXES", "worker-hunt-")
	now := time.Now().Unix()
	wm := &workManager{worker: map[string]workerPayoutStat{
		"gpu-1": {
			LastHashrateGHS: 40, LastSeenUnix: now,
			LastPoHSeenUnix: now, LastFuzzSeenUnix: now,
		},
		"worker-hunt-rx580": {LastHashrateGHS: 0, LastSeenUnix: now, LastFuzzSeenUnix: now},
		"dig-cheap":         {LastHashrateGHS: 0, LastSeenUnix: now, LastFuzzSeenUnix: now},
	}}
	if ok, reason := wm.allowFuzzClaimByGHS("worker-hunt-rx580", now); !ok {
		t.Fatalf("worker-hunt must be exempt via env: %s", reason)
	}
	if ok, reason := wm.allowFuzzClaimByGHS("dig-cheap", now); ok || reason != "ghs_priority_defer" {
		t.Fatalf("non-exempt dig-only must defer: ok=%v reason=%q", ok, reason)
	}
}

func TestBlake3ClassWallGHSCountsAsHybrid(t *testing.T) {
	// Live Blake3 desktop / named-fleet wall rates often ~0.02–0.4 GH/s.
	t.Setenv("HACKME_FUZZ_CLAIM_GHS_PRIORITY", "1")
	t.Setenv("HACKME_FUZZ_GHS_HYBRID_MIN", "") // default 0.002
	now := time.Now().Unix()
	wm := &workManager{worker: map[string]workerPayoutStat{
		"worker-kapa-pc": {
			LastHashrateGHS: 0.27, PeakHashrateGHS: 0.39,
			LastSeenUnix: now, LastPoHSeenUnix: now, LastFuzzSeenUnix: now,
		},
		"worker-shannon": {
			// SHARE_STRICT share-implied on CPU hybrid board is often ~0.002–0.02.
			LastHashrateGHS: 0.0035, PeakHashrateGHS: 0.01,
			LastSeenUnix: now, LastPoHSeenUnix: now, LastFuzzSeenUnix: now,
		},
		"dig-1": {LastHashrateGHS: 0, LastSeenUnix: now, LastFuzzSeenUnix: now},
	}}
	cap := wm.fuzzFleetCapacity(now)
	if cap.HybridWorkersOnline != 2 {
		t.Fatalf("blake3-class hybrid online=%d want 2: %+v", cap.HybridWorkersOnline, cap)
	}
	if cap.FleetHashrateGHS < 0.27 || cap.FleetHashrateGHS > 0.45 {
		t.Fatalf("fleet ghs=%v", cap.FleetHashrateGHS)
	}
	if ok, _ := wm.allowFuzzClaimByGHS("worker-kapa-pc", now); !ok {
		t.Fatal("blake3 hybrid must claim")
	}
	if ok, _ := wm.allowFuzzClaimByGHS("worker-shannon", now); !ok {
		t.Fatal("named-fleet blake3 share-implied GH must clear hybrid floor")
	}
}

func TestAllowFuzzClaimByGHSAllowsDigOnlyWhenNoHybrid(t *testing.T) {
	t.Setenv("HACKME_FUZZ_CLAIM_GHS_PRIORITY", "1")
	now := time.Now().Unix()
	wm := &workManager{worker: map[string]workerPayoutStat{
		"dig-a": {LastHashrateGHS: 0, LastSeenUnix: now, LastFuzzSeenUnix: now},
		"dig-b": {LastHashrateGHS: 0.1, LastSeenUnix: now, LastFuzzSeenUnix: now},
	}}
	if ok, reason := wm.allowFuzzClaimByGHS("dig-a", now); !ok {
		t.Fatalf("dig-only fleet must work without hybrid: %s", reason)
	}
}

func TestAllowFuzzClaimByGHSDisabled(t *testing.T) {
	t.Setenv("HACKME_FUZZ_CLAIM_GHS_PRIORITY", "0")
	now := time.Now().Unix()
	wm := &workManager{worker: map[string]workerPayoutStat{
		"gpu-1": {
			LastHashrateGHS: 40, LastSeenUnix: now,
			LastPoHSeenUnix: now, LastFuzzSeenUnix: now,
		},
		"dig-1": {LastHashrateGHS: 0, LastSeenUnix: now, LastFuzzSeenUnix: now},
	}}
	for w := int64(0); w < 20; w++ {
		if ok, _ := wm.allowFuzzClaimByGHS("dig-1", now+w*10); !ok {
			t.Fatal("priority off => dig-only always allowed")
		}
	}
}

func TestFuzzFleetCapacityIgnoresGhostAndPoHOnly(t *testing.T) {
	now := time.Now().Unix()
	wm := &workManager{worker: map[string]workerPayoutStat{
		"hybrid": {
			LastHashrateGHS: 30, PeakHashrateGHS: 30,
			LastSeenUnix: now, LastPoHSeenUnix: now, LastFuzzSeenUnix: now,
		},
		"ghost": { // stopped mining; fuzz heartbeat only
			LastHashrateGHS: 50, PeakHashrateGHS: 50,
			LastSeenUnix: now, LastPoHSeenUnix: now - 10_000, LastFuzzSeenUnix: now,
		},
		"poh-only": { // mines but hybrid fuzz off
			LastHashrateGHS: 40, PeakHashrateGHS: 40,
			LastSeenUnix: now, LastPoHSeenUnix: now, LastFuzzSeenUnix: 0,
		},
		"dig-1": {LastHashrateGHS: 0, LastSeenUnix: now, LastFuzzSeenUnix: now},
		"old":   {LastHashrateGHS: 50, LastSeenUnix: now - 10_000},
	}}
	cap := wm.fuzzFleetCapacity(now)
	if cap.HybridWorkersOnline != 1 {
		t.Fatalf("hybrid online=%d want 1 (ghost+poh-only excluded): %+v", cap.HybridWorkersOnline, cap)
	}
	if cap.DigOnlyWorkersOnline < 1 {
		t.Fatalf("dig-only=%d: %+v", cap.DigOnlyWorkersOnline, cap)
	}
	if cap.FleetHashrateGHS < 29 || cap.FleetHashrateGHS > 31 {
		t.Fatalf("fleet ghs=%v (must not include ghost/poh-only)", cap.FleetHashrateGHS)
	}
	// With only ghost+poh-only (no true hybrid), dig-only unrestricted.
	wm2 := &workManager{worker: map[string]workerPayoutStat{
		"ghost":    wm.worker["ghost"],
		"poh-only": wm.worker["poh-only"],
		"dig-1":    wm.worker["dig-1"],
	}}
	for w := int64(0); w < 20; w++ {
		if ok, reason := wm2.allowFuzzClaimByGHS("dig-1", now+w*10); !ok {
			t.Fatalf("no true hybrid => dig-only always: %s", reason)
		}
	}
}

func TestAllowFuzzClaimGhostLosesHybridPriority(t *testing.T) {
	t.Setenv("HACKME_FUZZ_CLAIM_GHS_PRIORITY", "1")
	t.Setenv("HACKME_FUZZ_CLAIM_GHS_DIG_ONLY_PCT", "0") // hard defer dig-only when hybrid present
	t.Setenv("HACKME_FUZZ_CLAIM_GHS_EXEMPT_PREFIXES", "")
	now := time.Now().Unix()
	wm := &workManager{worker: map[string]workerPayoutStat{
		"hybrid": {
			LastHashrateGHS: 30, LastSeenUnix: now,
			LastPoHSeenUnix: now, LastFuzzSeenUnix: now,
		},
		"ghost": {
			LastHashrateGHS: 50, LastSeenUnix: now,
			LastPoHSeenUnix: now - 10_000, LastFuzzSeenUnix: now,
		},
	}}
	if ok, _ := wm.allowFuzzClaimByGHS("hybrid", now); !ok {
		t.Fatal("live hybrid")
	}
	// Ghost has stale PoH — treated as dig-only, deferred while true hybrid online.
	if ok, reason := wm.allowFuzzClaimByGHS("ghost", now); ok || reason != "ghs_priority_defer" {
		t.Fatalf("ghost must defer: ok=%v reason=%q", ok, reason)
	}
}

func TestFuzzFleetCapacityAndETA(t *testing.T) {
	now := time.Now().Unix()
	wm := &workManager{worker: map[string]workerPayoutStat{
		"gpu-1": {
			LastHashrateGHS: 30, PeakHashrateGHS: 30,
			LastSeenUnix: now, LastPoHSeenUnix: now, LastFuzzSeenUnix: now,
		},
		"gpu-2": {
			LastHashrateGHS: 20, PeakHashrateGHS: 20,
			LastSeenUnix: now, LastPoHSeenUnix: now, LastFuzzSeenUnix: now,
		},
		"dig-1": {LastHashrateGHS: 0, LastSeenUnix: now, LastFuzzSeenUnix: now},
		"old":   {LastHashrateGHS: 50, LastSeenUnix: now - 10_000},
	}}
	cap := wm.fuzzFleetCapacity(now)
	if cap.HybridWorkersOnline != 2 || cap.DigOnlyWorkersOnline != 1 {
		t.Fatalf("cap=%+v", cap)
	}
	if cap.FleetHashrateGHS < 49 || cap.FleetHashrateGHS > 51 {
		t.Fatalf("fleet ghs=%v", cap.FleetHashrateGHS)
	}
	if cap.EstShardsPerHour < 1 {
		t.Fatalf("est shards=%v", cap.EstShardsPerHour)
	}
	eta := estimateETASeconds(360, cap.EstShardsPerHour)
	if eta <= 0 {
		t.Fatalf("eta=%d", eta)
	}
	if estimateETASeconds(0, cap.EstShardsPerHour) != 0 {
		t.Fatal("complete eta")
	}
	if estimateETASeconds(10, 0) != -1 {
		t.Fatal("unknown eta")
	}
}
