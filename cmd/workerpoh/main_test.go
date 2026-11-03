package main

import (
	"math"
	"testing"
	"time"

	"hackme/internal/gpupoh"
)

func TestEnvUint64(t *testing.T) {
	cases := []struct {
		name string
		set  string
		val  string
		want uint64
	}{
		{name: "unset returns fallback", set: "", want: 1 << 24},
		{name: "plain value", set: "GPU_CHUNK_TEST", val: "8388608", want: 8388608},
		{name: "whitespace trimmed", set: "GPU_CHUNK_TEST", val: " 16777216 ", want: 16777216},
		{name: "zero rejected", set: "GPU_CHUNK_TEST", val: "0", want: 1 << 24},
		{name: "negative rejected", set: "GPU_CHUNK_TEST", val: "-1", want: 1 << 24},
		{name: "garbage rejected", set: "GPU_CHUNK_TEST", val: "4M", want: 1 << 24},
		{name: "overflow rejected", set: "GPU_CHUNK_TEST", val: "99999999999999999999999", want: 1 << 24},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set != "" {
				t.Setenv(tc.set, tc.val)
			}
			if got := envUint64("GPU_CHUNK_TEST", 1<<24); got != tc.want {
				t.Fatalf("envUint64 = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestEnvIntMs(t *testing.T) {
	cases := []struct {
		name string
		set  string
		val  string
		want int
	}{
		{name: "unset returns fallback", set: "", want: 2500},
		{name: "plain value", set: "SEARCH_TIMEOUT_TEST", val: "12000", want: 12000},
		{name: "whitespace trimmed", set: "SEARCH_TIMEOUT_TEST", val: " 6000 ", want: 6000},
		{name: "zero allowed (cooldown semantics: no sleep)", set: "SEARCH_TIMEOUT_TEST", val: "0", want: 0},
		{name: "negative rejected", set: "SEARCH_TIMEOUT_TEST", val: "-5", want: 2500},
		{name: "garbage rejected", set: "SEARCH_TIMEOUT_TEST", val: "2.5s", want: 2500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set != "" {
				t.Setenv(tc.set, tc.val)
			}
			if got := envIntMs("SEARCH_TIMEOUT_TEST", 2500); got != tc.want {
				t.Fatalf("envIntMs = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestEnvIntPositive(t *testing.T) {
	cases := []struct {
		name string
		set  string
		val  string
		want int
	}{
		{name: "unset returns fallback", set: "", want: 2500},
		{name: "plain value", set: "SEARCH_TIMEOUT_TEST", val: "12000", want: 12000},
		{name: "whitespace trimmed", set: "SEARCH_TIMEOUT_TEST", val: " 6000 ", want: 6000},
		// Regression: a zero GPU search timeout is an already-expired context, so
		// every GPU Search would fail and silently fall back to CPU.
		{name: "zero rejected (expired context guard)", set: "SEARCH_TIMEOUT_TEST", val: "0", want: 2500},
		{name: "negative rejected", set: "SEARCH_TIMEOUT_TEST", val: "-5", want: 2500},
		{name: "garbage rejected", set: "SEARCH_TIMEOUT_TEST", val: "2.5s", want: 2500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set != "" {
				t.Setenv(tc.set, tc.val)
			}
			if got := envIntPositive("SEARCH_TIMEOUT_TEST", 2500); got != tc.want {
				t.Fatalf("envIntPositive = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestEffectiveClaimCooldownBlake3FastKernel(t *testing.T) {
	t.Setenv("HACKME_WORKER_CLAIM_COOLDOWN_MS", "100")
	t.Setenv("HACKME_WORKER_SUBGH_COOLDOWN_MS", "25000")
	// ghs<1 would previously force 25s; with no kernel timing, mild floor applies.
	got := effectiveClaimCooldownMS("gpu", 0.3)
	if got < 100 || got > 25000 {
		t.Fatalf("slow/missing kernel path: got %d", got)
	}
	// Explicit 0 disables sub-GH floor.
	t.Setenv("HACKME_WORKER_SUBGH_COOLDOWN_MS", "0")
	if got := effectiveClaimCooldownMS("gpu", 0.3); got != 100 {
		t.Fatalf("subgh disabled: got %d want 100", got)
	}
	// High reported GH/s never hits sub-GH floor.
	t.Setenv("HACKME_WORKER_SUBGH_COOLDOWN_MS", "25000")
	if got := effectiveClaimCooldownMS("gpu", 6.0); got != 100 {
		t.Fatalf("multi-GH/s: got %d want 100", got)
	}
}

func TestSubmitHashrateBlake3OmitsStickyCalibEMA(t *testing.T) {
	// Simulate pre-Blake3 FORCE/calib still sitting in EMA; fv≥3 must report wall only.
	prevEMA := submitHashrateEMA
	prevCal := gpuCalibratedGHS
	t.Cleanup(func() {
		submitHashrateEMA = prevEMA
		gpuCalibratedGHS = prevCal
	})
	submitHashrateEMA = 80
	gpuCalibratedGHS = 80
	t.Setenv("HASHRATE_GHS", "")
	t.Setenv("HACKME_WORKER_HASHRATE_GHS", "")
	t.Setenv("HACKME_WORKER_DECLARED_HASHRATE_GHS", "")
	t.Setenv("HACKME_GPU_HASHRATE_FLOOR_GHS", "")
	t.Setenv("HACKME_CUDA_CALIBRATE_GHS", "")

	const batch uint64 = 4_000_000
	got := submitHashrateGHSForFormula(batch, 2.0, "cpu", 3)
	// wall = 4e6/2/1e9 = 0.002 GH/s — must not resurrect 80 GH/s FORCE/calib EMA.
	if got > 0.01 || math.IsNaN(got) || got <= 0 {
		t.Fatalf("blake3 must omit FORCE/calib EMA and report wall, got %v", got)
	}
}

func TestCollectShareNoncesStrideCoversLargeLease(t *testing.T) {
	const shareMod = uint64(1_000_003)
	base := uint64(0)
	batch := uint64(16 << 20) // 16M
	// Prefix-only scan of 256K would miss shares past the window; stride must find some.
	out := collectShareNonces(base, batch, shareMod, 1, "", "", 8, 0, false, 256*1024)
	if len(out) == 0 {
		t.Fatal("stride harvest found no shares in 16M lease")
	}
	for _, n := range out {
		if n < base || n >= base+batch {
			t.Fatalf("nonce out of lease: %d", n)
		}
	}
}

// chunkSearcher returns scripted SearchPoHCollect results, one per chunk.
type chunkSearcher struct {
	results []gpupoh.SearchResult
	calls   int
}

func (s *chunkSearcher) Label() string { return "fake" }
func (s *chunkSearcher) Search(time.Duration, uint64, uint64, uint64) (bool, uint64, float64, error) {
	return false, 0, 0, nil
}
func (s *chunkSearcher) SearchPoH(time.Duration, gpupoh.SearchRequest) (bool, uint64, float64, error) {
	return false, 0, 0, nil
}
func (s *chunkSearcher) SearchPoHCollect(_ time.Duration, req gpupoh.SearchRequest) (gpupoh.SearchResult, float64, error) {
	var r gpupoh.SearchResult
	if s.calls < len(s.results) {
		r = s.results[s.calls]
	}
	s.calls++
	r.Scanned = req.Count
	return r, 0.001, nil
}

func seqNonces(from, n uint64) []uint64 {
	out := make([]uint64, n)
	for i := range out {
		out[i] = from + uint64(i)
	}
	return out
}

func runScanLeaseGPU(t *testing.T, strict bool, results []gpupoh.SearchResult) (gpuLeaseScan, *chunkSearcher) {
	t.Helper()
	srch := &chunkSearcher{results: results}
	cr := claimResp{BaseNonce: 0, BatchSize: 10 * 1024, TargetMod: 1_000_000, ShareMod: 1000, ShareStrict: strict}
	var shares []uint64
	req := func(base, count uint64) gpupoh.SearchRequest {
		return gpupoh.SearchRequest{Base: base, Count: count, Mod: cr.TargetMod, ShareMod: cr.ShareMod}
	}
	got := scanLeaseGPU(srch, cr, 1024, time.Second, req, 3, "HMC-test", "w1",
		func() bool { return false },
		func(more []uint64) { shares = append(shares, more...) },
		func() int { return len(shares) })
	return got, srch
}

func TestScanLeaseGPUKeepsHitFromChunkReachingShareThreshold(t *testing.T) {
	// Chunk 1: 31 shares. Chunk 2: the 32nd share and a block hit in the same pass.
	got, srch := runScanLeaseGPU(t, false, []gpupoh.SearchResult{
		{Shares: seqNonces(0, 31)},
		{Shares: seqNonces(1024, 1), Found: true, Nonce: 1500},
	})
	if !got.found || got.nonce != 1500 {
		t.Fatalf("block hit dropped: found=%v nonce=%d", got.found, got.nonce)
	}
	if srch.calls != 2 || got.searched != 2048 {
		t.Fatalf("want stop after chunk 2: calls=%d searched=%d", srch.calls, got.searched)
	}
}

func TestScanLeaseGPUStrictKeepsHitFromChunkReachingShareCap(t *testing.T) {
	// Strict PPS: no hit until the chunk that also reaches the 64-share cap.
	got, srch := runScanLeaseGPU(t, true, []gpupoh.SearchResult{
		{Shares: seqNonces(0, 40)},
		{Shares: seqNonces(1024, 24), Found: true, Nonce: 2000},
	})
	if !got.found || got.nonce != 2000 {
		t.Fatalf("block hit dropped: found=%v nonce=%d", got.found, got.nonce)
	}
	if srch.calls != 2 {
		t.Fatalf("want stop after chunk 2, calls=%d", srch.calls)
	}
}

func TestScanLeaseGPUStrictKeepsScanningWithoutHit(t *testing.T) {
	// Strict PPS with 32..63 shares and no hit: keep scanning for the block.
	got, srch := runScanLeaseGPU(t, true, []gpupoh.SearchResult{
		{Shares: seqNonces(0, 40)},
		{},
		{Found: true, Nonce: 2100},
	})
	if !got.found || got.nonce != 2100 || srch.calls != 3 {
		t.Fatalf("found=%v nonce=%d calls=%d", got.found, got.nonce, srch.calls)
	}
}
