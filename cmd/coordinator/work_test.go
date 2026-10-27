package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"hackme/internal/chain"
	"hackme/internal/store"
)

func TestWorkManagerClaimMonotonic(t *testing.T) {
	wm := &workManager{defaultBatch: 1000, targetMod: 1000000, leaseSec: 30, maxWorkers: 1000, maxActiveLeases: 1000, maxDedupEntries: 1000, active: make(map[workKey]leaseRecord), worker: make(map[string]workerPayoutStat)}
	b1, s1, _, _, _, ok1, reason1 := wm.claim("w1", 0)
	b2, s2, _, _, _, ok2, reason2 := wm.claim("w2", 2000)
	if !ok1 || reason1 != "" || !ok2 || reason2 != "" {
		t.Fatalf("claim failed: c1 ok=%v reason=%q c2 ok=%v reason=%q", ok1, reason1, ok2, reason2)
	}
	if b1 != 0 || s1 != 1000 {
		t.Fatalf("first claim got base=%d size=%d", b1, s1)
	}
	if b2 != 1000 || s2 != 2000 {
		t.Fatalf("second claim got base=%d size=%d", b2, s2)
	}
	st := wm.stats(false)
	if st["issued_ranges"].(uint64) != 2 {
		t.Fatalf("issued_ranges=%v", st["issued_ranges"])
	}
}

func TestWorkManagerReissuesExpiredRange(t *testing.T) {
	wm := &workManager{defaultBatch: 1000, targetMod: 1000000, leaseSec: 1, maxWorkers: 1000, maxActiveLeases: 1000, maxDedupEntries: 1000, active: make(map[workKey]leaseRecord), worker: make(map[string]workerPayoutStat)}
	base1, _, _, _, reused1, ok1, reason1 := wm.claim("w1", 0)
	if !ok1 || reason1 != "" {
		t.Fatalf("first claim failed: ok=%v reason=%q", ok1, reason1)
	}
	if reused1 {
		t.Fatal("first claim cannot be reissued")
	}
	time.Sleep(1200 * time.Millisecond)
	base2, _, _, _, reused2, ok2, reason2 := wm.claim("w2", 0)
	if !ok2 || reason2 != "" {
		t.Fatalf("second claim failed: ok=%v reason=%q", ok2, reason2)
	}
	if !reused2 {
		t.Fatal("expected reissued range after lease expiry")
	}
	if base2 != base1 {
		t.Fatalf("want same base on reissue, got %d vs %d", base2, base1)
	}
	st := wm.stats(false)
	if st["reissued_ranges"].(uint64) < 1 {
		t.Fatalf("reissued_ranges=%v", st["reissued_ranges"])
	}
}

func TestWorkManagerSubmitPayout(t *testing.T) {
	wm := withTestTip(&workManager{
		defaultBatch:         1000,
		targetMod:            997,
		leaseSec:             30,
		rewardPerM:           1.0,
		foundBonus:           0.25,
		maxWorkers:           1000,
		maxActiveLeases:      1000,
		maxDedupEntries:      1000,
		active:               make(map[workKey]leaseRecord),
		worker:               make(map[string]workerPayoutStat),
		acceptedResultHashes: make(map[string]struct{}),
		acceptedFoundNonces:  make(map[uint64]struct{}),
	})
	base, size, _, _, _, okClaim, reasonClaim := wm.claim("w1", 1000)
	if !okClaim || reasonClaim != "" {
		t.Fatalf("claim failed: ok=%v reason=%q", okClaim, reasonClaim)
	}
	// Find a valid PoH hit under v1 eval (7n+13) for this target_mod inside the leased range.
	foundNonce := uint64(0)
	for n := base; n < base+size; n++ {
		if chain.PohEval(n)%wm.targetMod == 0 {
			foundNonce = n
			break
		}
	}
	if foundNonce == 0 {
		t.Fatal("failed to find valid foundNonce in test range")
	}
	ok, reason, payout, _, _ := wm.submit(submitWorkRequest{
		WorkerID:    "w1",
		BaseNonce:   base,
		BatchSize:   size,
		WorkID:      buildWorkID("w1", base, size),
		Attempts:    500,
		Found:       true,
		FoundNonce:  foundNonce,
		ResultHash:  "abc123",
		HashrateGHS: 1.0,
	})
	if !ok || reason != "" {
		t.Fatalf("submit not accepted ok=%v reason=%q", ok, reason)
	}
	// attempts payout: 500/1_000_000*1.0 = 0.0005, plus found bonus 0.25
	if payout < 0.25049 || payout > 0.25051 {
		t.Fatalf("unexpected payout=%f", payout)
	}
	st := wm.stats(true)
	if st["total_payout_hmc"].(float64) < 0.25049 {
		t.Fatalf("total_payout_hmc=%v", st["total_payout_hmc"])
	}
	workers, _ := st["workers"].(map[string]workerPayoutStat)
	if workers["w1"].AcceptedAtt != 500 {
		t.Fatalf("worker attempts=%d", workers["w1"].AcceptedAtt)
	}
}

func TestWorkManagerRejectsWorkIDMismatch(t *testing.T) {
	wm := &workManager{
		defaultBatch:         1000,
		targetMod:            1000000,
		leaseSec:             30,
		maxWorkers:           1000,
		maxActiveLeases:      1000,
		maxDedupEntries:      1000,
		active:               make(map[workKey]leaseRecord),
		worker:               make(map[string]workerPayoutStat),
		acceptedResultHashes: make(map[string]struct{}),
		acceptedFoundNonces:  make(map[uint64]struct{}),
	}
	base, size, _, _, _, okClaim, reasonClaim := wm.claim("w1", 1000)
	if !okClaim || reasonClaim != "" {
		t.Fatalf("claim failed: ok=%v reason=%q", okClaim, reasonClaim)
	}
	ok, reason, _, _, _ := wm.submit(submitWorkRequest{
		WorkerID:  "w1",
		BaseNonce: base,
		BatchSize: size,
		WorkID:    "w1:bad",
	})
	if ok || reason != "work_id_mismatch" {
		t.Fatalf("want work_id_mismatch, got ok=%v reason=%q", ok, reason)
	}
}

func TestWorkManagerDedupResultHash(t *testing.T) {
	wm := withTestTip(&workManager{
		defaultBatch:         1000,
		targetMod:            997,
		leaseSec:             30,
		rewardPerM:           1.0,
		foundBonus:           0.01,
		maxWorkers:           1000,
		maxActiveLeases:      1000,
		maxDedupEntries:      1000,
		active:               make(map[workKey]leaseRecord),
		worker:               make(map[string]workerPayoutStat),
		acceptedResultHashes: make(map[string]struct{}),
		acceptedFoundNonces:  make(map[uint64]struct{}),
	})
	base1, size1, _, _, _, okClaim1, reasonClaim1 := wm.claim("w1", 1000)
	if !okClaim1 || reasonClaim1 != "" {
		t.Fatalf("first claim failed: ok=%v reason=%q", okClaim1, reasonClaim1)
	}
	found1 := uint64(0)
	for n := base1; n < base1+size1; n++ {
		if chain.PohEval(n)%wm.targetMod == 0 {
			found1 = n
			break
		}
	}
	if found1 == 0 {
		t.Fatal("failed to find valid foundNonce in test range")
	}
	ok1, reason1, _, _, _ := wm.submit(submitWorkRequest{
		WorkerID:   "w1",
		BaseNonce:  base1,
		BatchSize:  size1,
		WorkID:     buildWorkID("w1", base1, size1),
		Found:      true,
		FoundNonce: found1,
		ResultHash: "deadbeef",
	})
	if !ok1 || reason1 != "" {
		t.Fatalf("first submit failed: ok=%v reason=%q", ok1, reason1)
	}
	base2, size2, _, _, _, okClaim2, reasonClaim2 := wm.claim("w2", 1000)
	if !okClaim2 || reasonClaim2 != "" {
		t.Fatalf("second claim failed: ok=%v reason=%q", okClaim2, reasonClaim2)
	}
	found2 := uint64(0)
	for n := base2; n < base2+size2; n++ {
		if chain.PohEval(n)%wm.targetMod == 0 {
			found2 = n
			break
		}
	}
	if found2 == 0 {
		t.Fatal("failed to find valid foundNonce in test range")
	}
	ok2, reason2, _, _, _ := wm.submit(submitWorkRequest{
		WorkerID:   "w2",
		BaseNonce:  base2,
		BatchSize:  size2,
		WorkID:     buildWorkID("w2", base2, size2),
		Found:      true,
		FoundNonce: found2,
		ResultHash: "deadbeef",
	})
	if ok2 || reason2 != "duplicate_result_hash" {
		t.Fatalf("want duplicate_result_hash, got ok=%v reason=%q", ok2, reason2)
	}
}

func TestWorkManagerRejectsFoundWithoutResultHash(t *testing.T) {
	wm := &workManager{
		defaultBatch:         1000,
		targetMod:            1000000,
		leaseSec:             30,
		maxWorkers:           1000,
		maxActiveLeases:      1000,
		maxDedupEntries:      1000,
		active:               make(map[workKey]leaseRecord),
		worker:               make(map[string]workerPayoutStat),
		acceptedResultHashes: make(map[string]struct{}),
		acceptedFoundNonces:  make(map[uint64]struct{}),
	}
	base, size, _, _, _, okClaim, reasonClaim := wm.claim("w1", 1000)
	if !okClaim || reasonClaim != "" {
		t.Fatalf("claim failed: ok=%v reason=%q", okClaim, reasonClaim)
	}
	ok, reason, payout, _, _ := wm.submit(submitWorkRequest{
		WorkerID:   "w1",
		BaseNonce:  base,
		BatchSize:  size,
		WorkID:     buildWorkID("w1", base, size),
		Found:      true,
		FoundNonce: base + 11,
	})
	if ok || reason != "result_hash_required_for_found" || payout != 0 {
		t.Fatalf("want found hash required rejection, got ok=%v reason=%q payout=%f", ok, reason, payout)
	}
}

func TestWorkManagerRejectsFoundNonceOutOfRange(t *testing.T) {
	wm := &workManager{
		defaultBatch:         1000,
		targetMod:            1000000,
		leaseSec:             30,
		maxWorkers:           1000,
		maxActiveLeases:      1000,
		maxDedupEntries:      1000,
		active:               make(map[workKey]leaseRecord),
		worker:               make(map[string]workerPayoutStat),
		acceptedResultHashes: make(map[string]struct{}),
		acceptedFoundNonces:  make(map[uint64]struct{}),
	}
	base, size, _, _, _, okClaim, reasonClaim := wm.claim("w1", 1000)
	if !okClaim || reasonClaim != "" {
		t.Fatalf("claim failed: ok=%v reason=%q", okClaim, reasonClaim)
	}
	ok, reason, payout, _, _ := wm.submit(submitWorkRequest{
		WorkerID:   "w1",
		BaseNonce:  base,
		BatchSize:  size,
		WorkID:     buildWorkID("w1", base, size),
		Found:      true,
		FoundNonce: base + size + 10,
		ResultHash: "r1",
	})
	if ok || reason != "found_nonce_out_of_range" || payout != 0 {
		t.Fatalf("want found nonce range rejection, got ok=%v reason=%q payout=%f", ok, reason, payout)
	}
}

func TestWorkManagerRejectsDuplicateFoundNonce(t *testing.T) {
	wm := withTestTip(&workManager{
		defaultBatch:         1000,
		targetMod:            997,
		leaseSec:             30,
		maxWorkers:           1000,
		maxActiveLeases:      1000,
		maxDedupEntries:      1000,
		active:               make(map[workKey]leaseRecord),
		worker:               make(map[string]workerPayoutStat),
		acceptedResultHashes: make(map[string]struct{}),
		acceptedFoundNonces:  make(map[uint64]struct{}),
	})
	base1, size1, _, _, _, okClaim1, reasonClaim1 := wm.claim("w1", 1000)
	if !okClaim1 || reasonClaim1 != "" {
		t.Fatalf("first claim failed: ok=%v reason=%q", okClaim1, reasonClaim1)
	}
	nonce := uint64(0)
	for n := base1; n < base1+size1; n++ {
		if chain.PohEval(n)%wm.targetMod == 0 {
			nonce = n
			break
		}
	}
	if nonce == 0 {
		t.Fatal("failed to find valid foundNonce in test range")
	}
	ok1, reason1, _, _, _ := wm.submit(submitWorkRequest{
		WorkerID:   "w1",
		BaseNonce:  base1,
		BatchSize:  size1,
		WorkID:     buildWorkID("w1", base1, size1),
		Found:      true,
		FoundNonce: nonce,
		ResultHash: "h1",
	})
	if !ok1 || reason1 != "" {
		t.Fatalf("first found must pass: ok=%v reason=%q", ok1, reason1)
	}

	base2, size2, _, _, _, okClaim2, reasonClaim2 := wm.claim("w2", 1000)
	if !okClaim2 || reasonClaim2 != "" {
		t.Fatalf("second claim failed: ok=%v reason=%q", okClaim2, reasonClaim2)
	}
	ok2, reason2, _, _, _ := wm.submit(submitWorkRequest{
		WorkerID:   "w2",
		BaseNonce:  base2,
		BatchSize:  size2,
		WorkID:     buildWorkID("w2", base2, size2),
		Found:      true,
		FoundNonce: nonce,
		ResultHash: "h2",
	})
	if ok2 || reason2 != "duplicate_found_nonce" {
		t.Fatalf("want duplicate_found_nonce, got ok=%v reason=%q", ok2, reason2)
	}
}

func TestWorkManagerClaimRateLimit(t *testing.T) {
	wm := &workManager{
		defaultBatch:    1000,
		targetMod:       1000000,
		leaseSec:        30,
		claimPerMin:     2,
		submitPerMin:    10,
		banSec:          60,
		badStrikesToBan: 3,
		maxWorkers:      1000,
		maxActiveLeases: 1000,
		maxDedupEntries: 1000,
		active:          make(map[workKey]leaseRecord),
		worker:          make(map[string]workerPayoutStat),
		abuse:           make(map[string]workerAbuseState),
	}
	now := int64(1_700_000_000)
	if ok, _ := wm.allowClaim("w1", "", now); !ok {
		t.Fatal("first claim should pass")
	}
	if ok, _ := wm.allowClaim("w1", "", now); !ok {
		t.Fatal("second claim should pass")
	}
	if ok, reason := wm.allowClaim("w1", "", now); ok || reason != "claim_rate_limited" {
		t.Fatalf("expected claim_rate_limited, got ok=%v reason=%q", ok, reason)
	}
}

func TestWorkManagerBanAfterBadSubmits(t *testing.T) {
	wm := &workManager{
		defaultBatch:    1000,
		targetMod:       1000000,
		leaseSec:        30,
		claimPerMin:     100,
		submitPerMin:    1000,
		banSec:          60,
		badStrikesToBan: 2,
		maxWorkers:      1000,
		maxActiveLeases: 1000,
		maxDedupEntries: 1000,
		active:          make(map[workKey]leaseRecord),
		worker:          make(map[string]workerPayoutStat),
		abuse:           make(map[string]workerAbuseState),
		ipAbuse:         make(map[string]workerAbuseState),
	}
	now := int64(1_700_000_000)
	// Spoofable mismatch reasons must NOT ban the nominal worker_id (shared-token DoS).
	wm.markSubmitOutcome("w-abuse", "203.0.113.9", "work_id_mismatch", now)
	wm.markSubmitOutcome("w-abuse", "203.0.113.9", "range_leased_to_another_worker", now)
	if ok, reason := wm.allowSubmit("w-abuse", "", now+1); !ok {
		t.Fatalf("spoofable mismatch must not ban worker, got reason=%q", reason)
	}
	if ok, reason := wm.allowSubmit("w-abuse", "203.0.113.9", now+1); ok || reason != "worker_temporarily_banned" {
		t.Fatalf("expected attacker IP banned, got ok=%v reason=%q", ok, reason)
	}
	wm.markSubmitOutcome("w-replay", "", "replay", now)
	wm.markSubmitOutcome("w-replay", "", "replay", now)
	wm.markSubmitOutcome("w-replay", "", "replay", now)
	if ok, reason := wm.allowSubmit("w-replay", "", now+1); !ok {
		t.Fatalf("replay alone must not ban worker, got reason=%q", reason)
	}
	wm.markSubmitOutcome("w-stale", "", "unknown_or_already_closed_range", now)
	wm.markSubmitOutcome("w-stale", "", "unknown_or_already_closed_range", now)
	wm.markSubmitOutcome("w-stale", "", "lease_expired", now)
	if ok, reason := wm.allowSubmit("w-stale", "", now+1); !ok {
		t.Fatalf("stale/closed-range alone must not ban worker, got reason=%q", reason)
	}
	if ok, _ := wm.allowSubmit("w-abuse", "203.0.113.9", now+61); !ok {
		t.Fatal("ban should expire")
	}
}

func TestClaimAsVictimRejectedWhenPayoutLocked(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	wm := &workManager{
		hybridSignerEnabled: true,
		claimRequirePubKey:  false, // even with require off…
		worker: map[string]workerPayoutStat{
			"victim-rig": {PayoutAddress: signerAddr(pub)},
		},
	}
	if ok, reason := wm.checkClaimMinerIdentity("victim-rig", "", ""); !ok {
		t.Fatalf("legacy empty claim must pass when require is off: ok=%v reason=%q", ok, reason)
	}
	pub2, _, _ := ed25519.GenerateKey(nil)
	if ok, reason := wm.checkClaimMinerIdentity("victim-rig", hex.EncodeToString(pub2), ""); ok || !strings.HasPrefix(reason, "payout_address_locked") {
		t.Fatalf("wrong key must fail: ok=%v reason=%q", ok, reason)
	}
	if ok, reason := wm.checkClaimMinerIdentity("victim-rig", hex.EncodeToString(pub), ""); !ok {
		t.Fatalf("matching key must pass: %s", reason)
	}
}

func TestSignatureRequiredDoesNotStrikeBoundWorker(t *testing.T) {
	wm := &workManager{
		banSec:          60,
		badStrikesToBan: 2,
		submitPerMin:    1000,
		claimPerMin:     1000,
		abuse:           make(map[string]workerAbuseState),
		ipAbuse:         make(map[string]workerAbuseState),
		worker: map[string]workerPayoutStat{
			"bound-miner": {PayoutAddress: "HMC-bound0000000000"},
		},
	}
	now := int64(1_700_000_100)
	wm.markSubmitOutcome("bound-miner", "198.51.100.7", "signature_required", now)
	wm.markSubmitOutcome("bound-miner", "198.51.100.7", "signature_required", now)
	if ok, reason := wm.allowSubmit("bound-miner", "", now+1); !ok {
		t.Fatalf("unsigned/forged sig must not ban bound worker, got %q", reason)
	}
	if ok, reason := wm.allowSubmit("bound-miner", "198.51.100.7", now+1); ok || reason != "worker_temporarily_banned" {
		t.Fatalf("expected attacker IP banned, got ok=%v reason=%q", ok, reason)
	}
}

func TestWorkManagerClaimRejectsNonceOverflow(t *testing.T) {
	wm := &workManager{
		defaultBatch:    10,
		targetMod:       1000000,
		leaseSec:        30,
		maxWorkers:      1000,
		maxActiveLeases: 1000,
		maxDedupEntries: 1000,
		active:          make(map[workKey]leaseRecord),
		worker:          make(map[string]workerPayoutStat),
	}
	wm.nextNonce.Store(math.MaxUint64 - 5)
	_, _, _, _, _, ok, reason := wm.claim("w1", 10)
	if ok || reason != "nonce_space_exhausted" {
		t.Fatalf("expected nonce_space_exhausted, got ok=%v reason=%q", ok, reason)
	}
}

func TestWorkManagerRejectsOverflowedSubmitRange(t *testing.T) {
	wm := &workManager{
		targetMod:            1000000,
		leaseSec:             30,
		active:               make(map[workKey]leaseRecord),
		worker:               make(map[string]workerPayoutStat),
		acceptedResultHashes: make(map[string]struct{}),
		acceptedFoundNonces:  make(map[uint64]struct{}),
	}
	base := uint64(math.MaxUint64 - 5)
	batch := uint64(10)
	wm.active[workKey{base: base, batch: batch}] = leaseRecord{
		WorkerID:  "w1",
		BaseNonce: base,
		BatchSize: batch,
		ExpiresAt: time.Now().Unix() + 30,
	}
	ok, reason, payout, _, _ := wm.submit(submitWorkRequest{
		WorkerID:   "w1",
		BaseNonce:  base,
		BatchSize:  batch,
		WorkID:     buildWorkID("w1", base, batch),
		Found:      true,
		FoundNonce: base,
		ResultHash: "hash1",
	})
	if ok || reason != "invalid_nonce_range" || payout != 0 {
		t.Fatalf("expected invalid_nonce_range, got ok=%v reason=%q payout=%f", ok, reason, payout)
	}
}

func TestWorkManagerPrunesOldAbuseState(t *testing.T) {
	wm := &workManager{
		claimPerMin:        2,
		submitPerMin:       10,
		banSec:             60,
		abuse:              make(map[string]workerAbuseState),
		ipAbuse:            make(map[string]workerAbuseState),
		lastAbusePruneUnix: 0,
	}
	now := int64(1_700_000_000)
	wm.abuse["old-worker"] = workerAbuseState{MinuteUnix: (now / 60) - 30}
	wm.ipAbuse["old-ip"] = workerAbuseState{MinuteUnix: (now / 60) - 30}
	if ok, _ := wm.allowClaim("fresh-worker", "", now); !ok {
		t.Fatal("claim must pass")
	}
	if _, ok := wm.abuse["old-worker"]; ok {
		t.Fatal("old worker abuse state must be pruned")
	}
	if _, ok := wm.ipAbuse["old-ip"]; ok {
		t.Fatal("old ip abuse state must be pruned")
	}
}

func signerAddr(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return "HMC-" + hex.EncodeToString(sum[:])[:16]
}

func TestWorkManagerHybridSignerAcceptsValidSignedSubmit(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	wm := &workManager{
		defaultBatch:           1000,
		targetMod:              1000000,
		leaseSec:               30,
		maxWorkers:             1000,
		maxActiveLeases:        1000,
		maxDedupEntries:        1000,
		hybridSignerEnabled:    true,
		active:                 make(map[workKey]leaseRecord),
		worker:                 make(map[string]workerPayoutStat),
		acceptedResultHashes:   make(map[string]struct{}),
		acceptedFoundNonces:    make(map[uint64]struct{}),
		acceptedSubmitNonces:   make(map[string]struct{}),
		acceptedSignedPayloads: make(map[string]struct{}),
		signedSubmitNonceMax:   make(map[string]uint64),
	}
	base, size, _, _, _, okClaim, reasonClaim := wm.claim("w1", 1000)
	if !okClaim || reasonClaim != "" {
		t.Fatalf("claim failed: ok=%v reason=%q", okClaim, reasonClaim)
	}
	req := submitWorkRequest{
		WorkerID:     "w1",
		BaseNonce:    base,
		BatchSize:    size,
		WorkID:       buildWorkID("w1", base, size),
		Attempts:     500,
		SubmitNonce:  1,
		MinerPubKey:  hex.EncodeToString(pub),
		MinerAddress: signerAddr(pub),
		MinerSigAlg:  "ed25519",
	}
	req.MinerSig = hex.EncodeToString(ed25519.Sign(priv, canonicalSubmitBytes(req)))
	_, reason, _, _, _ := wm.submit(req)
	if reason != "" {
		t.Fatalf("submit failed reason=%q", reason)
	}
	st := wm.stats(false)
	if st["signed_submits_accepted"].(uint64) != 1 {
		t.Fatalf("signed_submits_accepted=%v", st["signed_submits_accepted"])
	}
}

func TestWorkManagerHybridSignerRejectsInvalidSignature(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	wm := &workManager{
		defaultBatch:           1000,
		targetMod:              1000000,
		leaseSec:               30,
		maxWorkers:             1000,
		maxActiveLeases:        1000,
		maxDedupEntries:        1000,
		hybridSignerEnabled:    true,
		active:                 make(map[workKey]leaseRecord),
		worker:                 make(map[string]workerPayoutStat),
		acceptedResultHashes:   make(map[string]struct{}),
		acceptedFoundNonces:    make(map[uint64]struct{}),
		acceptedSubmitNonces:   make(map[string]struct{}),
		acceptedSignedPayloads: make(map[string]struct{}),
		signedSubmitNonceMax:   make(map[string]uint64),
	}
	base, size, _, _, _, okClaim, reasonClaim := wm.claim("w1", 1000)
	if !okClaim || reasonClaim != "" {
		t.Fatalf("claim failed: ok=%v reason=%q", okClaim, reasonClaim)
	}
	ok, reason, _, _, _ := wm.submit(submitWorkRequest{
		WorkerID:     "w1",
		BaseNonce:    base,
		BatchSize:    size,
		WorkID:       buildWorkID("w1", base, size),
		SubmitNonce:  1,
		MinerPubKey:  hex.EncodeToString(pub),
		MinerAddress: signerAddr(pub),
		MinerSigAlg:  "ed25519",
		MinerSig:     hex.EncodeToString(make([]byte, ed25519.SignatureSize)),
	})
	if ok || reason != "invalid_signature" {
		t.Fatalf("want invalid_signature, got ok=%v reason=%q", ok, reason)
	}
}

func TestWorkManagerHybridSignerRejectsReplay(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	wm := &workManager{
		defaultBatch:           1000,
		targetMod:              1000000,
		leaseSec:               30,
		maxWorkers:             1000,
		maxActiveLeases:        1000,
		maxDedupEntries:        1000,
		hybridSignerEnabled:    true,
		active:                 make(map[workKey]leaseRecord),
		worker:                 make(map[string]workerPayoutStat),
		acceptedResultHashes:   make(map[string]struct{}),
		acceptedFoundNonces:    make(map[uint64]struct{}),
		acceptedSubmitNonces:   make(map[string]struct{}),
		acceptedSignedPayloads: make(map[string]struct{}),
		signedSubmitNonceMax:   make(map[string]uint64),
	}
	base1, size1, _, _, _, okClaim1, reasonClaim1 := wm.claim("w1", 1000)
	if !okClaim1 || reasonClaim1 != "" {
		t.Fatalf("first claim failed: ok=%v reason=%q", okClaim1, reasonClaim1)
	}
	req1 := submitWorkRequest{
		WorkerID:     "w1",
		BaseNonce:    base1,
		BatchSize:    size1,
		WorkID:       buildWorkID("w1", base1, size1),
		SubmitNonce:  9,
		MinerPubKey:  hex.EncodeToString(pub),
		MinerAddress: signerAddr(pub),
		MinerSigAlg:  "ed25519",
	}
	req1.MinerSig = hex.EncodeToString(ed25519.Sign(priv, canonicalSubmitBytes(req1)))
	_, reason1, _, _, _ := wm.submit(req1)
	if reason1 != "" {
		t.Fatalf("first submit failed: reason=%q", reason1)
	}
	base2, size2, _, _, _, okClaim2, reasonClaim2 := wm.claim("w1", 1000)
	if !okClaim2 || reasonClaim2 != "" {
		t.Fatalf("second claim failed: ok=%v reason=%q", okClaim2, reasonClaim2)
	}
	req2 := submitWorkRequest{
		WorkerID:     "w1",
		BaseNonce:    base2,
		BatchSize:    size2,
		WorkID:       buildWorkID("w1", base2, size2),
		SubmitNonce:  9,
		MinerPubKey:  hex.EncodeToString(pub),
		MinerAddress: signerAddr(pub),
		MinerSigAlg:  "ed25519",
	}
	req2.MinerSig = hex.EncodeToString(ed25519.Sign(priv, canonicalSubmitBytes(req2)))
	ok2, reason2, _, _, _ := wm.submit(req2)
	if ok2 || reason2 != "replay" {
		t.Fatalf("want replay, got ok=%v reason=%q", ok2, reason2)
	}
}

func TestWorkManagerSharedMinerKeyDifferentWorkers(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	wm := &workManager{
		defaultBatch:           1000,
		targetMod:              1000000,
		leaseSec:               30,
		maxWorkers:             1000,
		maxActiveLeases:        1000,
		maxDedupEntries:        1000,
		hybridSignerEnabled:    true,
		active:                 make(map[workKey]leaseRecord),
		worker:                 make(map[string]workerPayoutStat),
		acceptedResultHashes:   make(map[string]struct{}),
		acceptedFoundNonces:    make(map[uint64]struct{}),
		acceptedSubmitNonces:   make(map[string]struct{}),
		acceptedSignedPayloads: make(map[string]struct{}),
		signedSubmitNonceMax:   make(map[string]uint64),
	}
	base1, size1, _, _, _, ok1, _ := wm.claim("worker-a", 1000)
	if !ok1 {
		t.Fatal("claim a failed")
	}
	reqA := submitWorkRequest{
		WorkerID: "worker-a", BaseNonce: base1, BatchSize: size1,
		WorkID: buildWorkID("worker-a", base1, size1), SubmitNonce: 42,
		MinerPubKey: hex.EncodeToString(pub), MinerAddress: signerAddr(pub), MinerSigAlg: "ed25519",
	}
	reqA.MinerSig = hex.EncodeToString(ed25519.Sign(priv, canonicalSubmitBytes(reqA)))
	if _, reason, _, _, _ := wm.submit(reqA); reason != "" {
		t.Fatalf("worker-a submit: %q", reason)
	}
	base2, size2, _, _, _, ok2, _ := wm.claim("worker-b", 1000)
	if !ok2 {
		t.Fatal("claim b failed")
	}
	reqB := submitWorkRequest{
		WorkerID: "worker-b", BaseNonce: base2, BatchSize: size2,
		WorkID: buildWorkID("worker-b", base2, size2), SubmitNonce: 42, // same nonce, shared key
		MinerPubKey: hex.EncodeToString(pub), MinerAddress: signerAddr(pub), MinerSigAlg: "ed25519",
	}
	reqB.MinerSig = hex.EncodeToString(ed25519.Sign(priv, canonicalSubmitBytes(reqB)))
	if _, reason, _, _, _ := wm.submit(reqB); reason != "" {
		t.Fatalf("shared-key different worker must accept, reason=%q", reason)
	}
}

func TestWorkManagerPayoutAddressLocked(t *testing.T) {
	pub1, priv1, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub2, priv2, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	wm := &workManager{
		defaultBatch:           1000,
		targetMod:              1000000,
		leaseSec:               30,
		maxWorkers:             1000,
		maxActiveLeases:        1000,
		maxDedupEntries:        1000,
		hybridSignerEnabled:    true,
		active:                 make(map[workKey]leaseRecord),
		worker:                 make(map[string]workerPayoutStat),
		acceptedResultHashes:   make(map[string]struct{}),
		acceptedFoundNonces:    make(map[uint64]struct{}),
		acceptedSubmitNonces:   make(map[string]struct{}),
		acceptedSignedPayloads: make(map[string]struct{}),
		signedSubmitNonceMax:   make(map[string]uint64),
	}
	base, size, _, _, _, ok, _ := wm.claim("rig-lock", 1000)
	if !ok {
		t.Fatal("claim failed")
	}
	req1 := submitWorkRequest{
		WorkerID: "rig-lock", BaseNonce: base, BatchSize: size,
		WorkID: buildWorkID("rig-lock", base, size), SubmitNonce: 1,
		MinerPubKey: hex.EncodeToString(pub1), MinerAddress: signerAddr(pub1), MinerSigAlg: "ed25519",
	}
	req1.MinerSig = hex.EncodeToString(ed25519.Sign(priv1, canonicalSubmitBytes(req1)))
	if _, reason, _, _, _ := wm.submit(req1); reason != "" {
		t.Fatalf("first signed submit: %q", reason)
	}
	if got := wm.worker["rig-lock"].PayoutAddress; !strings.EqualFold(got, signerAddr(pub1)) {
		t.Fatalf("locked payout=%q want %q", got, signerAddr(pub1))
	}
	base2, size2, _, _, _, ok2, _ := wm.claim("rig-lock", 1000)
	if !ok2 {
		t.Fatal("second claim failed")
	}
	req2 := submitWorkRequest{
		WorkerID: "rig-lock", BaseNonce: base2, BatchSize: size2,
		WorkID: buildWorkID("rig-lock", base2, size2), SubmitNonce: 2,
		MinerPubKey: hex.EncodeToString(pub2), MinerAddress: signerAddr(pub2), MinerSigAlg: "ed25519",
	}
	req2.MinerSig = hex.EncodeToString(ed25519.Sign(priv2, canonicalSubmitBytes(req2)))
	_, reason, _, _, _ := wm.submit(req2)
	if reason != "payout_address_locked" && !strings.HasPrefix(reason, "payout_address_locked:") {
		t.Fatalf("want payout_address_locked, got %q", reason)
	}
}

func TestWorkManagerHybridStrictRequiresSignature(t *testing.T) {
	wm := &workManager{
		defaultBatch:           1000,
		targetMod:              1000000,
		leaseSec:               30,
		maxWorkers:             1000,
		maxActiveLeases:        1000,
		maxDedupEntries:        1000,
		hybridSignerEnabled:    true,
		hybridSignerStrict:     true,
		hybridRequireFoundSig:  true,
		active:                 make(map[workKey]leaseRecord),
		worker:                 make(map[string]workerPayoutStat),
		acceptedResultHashes:   make(map[string]struct{}),
		acceptedFoundNonces:    make(map[uint64]struct{}),
		acceptedSubmitNonces:   make(map[string]struct{}),
		acceptedSignedPayloads: make(map[string]struct{}),
		signedSubmitNonceMax:   make(map[string]uint64),
	}
	base, size, _, _, _, okClaim, reasonClaim := wm.claim("w1", 1000)
	if !okClaim || reasonClaim != "" {
		t.Fatalf("claim failed: ok=%v reason=%q", okClaim, reasonClaim)
	}
	ok, reason, _, _, _ := wm.submit(submitWorkRequest{
		WorkerID:  "w1",
		BaseNonce: base,
		BatchSize: size,
		WorkID:    buildWorkID("w1", base, size),
		Attempts:  size,
	})
	if ok || reason != "signature_required" {
		t.Fatalf("want signature_required, got ok=%v reason=%q", ok, reason)
	}
}

func TestWorkManagerHybridFoundRequiresSignature(t *testing.T) {
	wm := &workManager{
		defaultBatch:           1000,
		targetMod:              1000000,
		leaseSec:               30,
		maxWorkers:             1000,
		maxActiveLeases:        1000,
		maxDedupEntries:        1000,
		hybridSignerEnabled:    true,
		hybridSignerStrict:     false,
		hybridRequireFoundSig:  true,
		active:                 make(map[workKey]leaseRecord),
		worker:                 make(map[string]workerPayoutStat),
		acceptedResultHashes:   make(map[string]struct{}),
		acceptedFoundNonces:    make(map[uint64]struct{}),
		acceptedSubmitNonces:   make(map[string]struct{}),
		acceptedSignedPayloads: make(map[string]struct{}),
		signedSubmitNonceMax:   make(map[string]uint64),
	}
	base, size, _, _, _, okClaim, reasonClaim := wm.claim("w1", 1000)
	if !okClaim || reasonClaim != "" {
		t.Fatalf("claim failed: ok=%v reason=%q", okClaim, reasonClaim)
	}
	ok, reason, _, _, _ := wm.submit(submitWorkRequest{
		WorkerID:   "w1",
		BaseNonce:  base,
		BatchSize:  size,
		WorkID:     buildWorkID("w1", base, size),
		Attempts:   size,
		Found:      true,
		FoundNonce: base + 1,
		ResultHash: "abc",
	})
	if ok || reason != "found_signature_required" {
		t.Fatalf("want found_signature_required, got ok=%v reason=%q", ok, reason)
	}
}

func TestWorkManagerHybridSignerRejectsTamperedSignatureByte(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	wm := newHybridTestWorkManager(true, true)
	base, size, _, _, _, okClaim, reasonClaim := wm.claim("w-tamper", 1000)
	if !okClaim || reasonClaim != "" {
		t.Fatalf("claim failed: %v %q", okClaim, reasonClaim)
	}
	req := submitWorkRequest{
		WorkerID:     "w-tamper",
		BaseNonce:    base,
		BatchSize:    size,
		WorkID:       buildWorkID("w-tamper", base, size),
		Attempts:     800,
		SubmitNonce:  42,
		MinerPubKey:  hex.EncodeToString(pub),
		MinerAddress: signerAddr(pub),
		MinerSigAlg:  "ed25519",
	}
	req.MinerSig = hex.EncodeToString(ed25519.Sign(priv, canonicalSubmitBytes(req)))
	sig, _ := hex.DecodeString(req.MinerSig)
	sig[0] ^= 0xFF
	req.MinerSig = hex.EncodeToString(sig)
	ok, reason, payout, _, _ := wm.submit(req)
	if ok || reason != "invalid_signature" {
		t.Fatalf("want invalid_signature, got ok=%v reason=%q payout=%v", ok, reason, payout)
	}
	if payout != 0 {
		t.Fatalf("tampered submit must not accrue payout, got %v", payout)
	}
}

func TestWorkManagerHybridSignerRejectsTamperedPayloadField(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	wm := newHybridTestWorkManager(true, true)
	base, size, _, _, _, okClaim, reasonClaim := wm.claim("w-tamper2", 1000)
	if !okClaim || reasonClaim != "" {
		t.Fatalf("claim failed: %v %q", okClaim, reasonClaim)
	}
	req := submitWorkRequest{
		WorkerID:     "w-tamper2",
		BaseNonce:    base,
		BatchSize:    size,
		WorkID:       buildWorkID("w-tamper2", base, size),
		Attempts:     500,
		SubmitNonce:  7,
		MinerPubKey:  hex.EncodeToString(pub),
		MinerAddress: signerAddr(pub),
		MinerSigAlg:  "ed25519",
	}
	req.MinerSig = hex.EncodeToString(ed25519.Sign(priv, canonicalSubmitBytes(req)))
	req.Attempts = 501 // one byte of logical payload changed after sign
	ok, reason, payout, _, _ := wm.submit(req)
	if ok || reason != "invalid_signature" {
		t.Fatalf("want invalid_signature, got ok=%v reason=%q payout=%v", ok, reason, payout)
	}
	if payout != 0 {
		t.Fatalf("tampered attempts must not accrue, got %v", payout)
	}
}

func newHybridTestWorkManager(strict, requireFound bool) *workManager {
	return &workManager{
		defaultBatch:           1000,
		targetMod:              1_000_000,
		leaseSec:               30,
		maxWorkers:             1000,
		maxActiveLeases:        1000,
		maxDedupEntries:        1000,
		hybridSignerEnabled:    true,
		hybridSignerStrict:     strict,
		hybridRequireFoundSig:  requireFound,
		rewardPerM:             1.0,
		payoutFoundOnly:        false,
		active:                 make(map[workKey]leaseRecord),
		worker:                 make(map[string]workerPayoutStat),
		abuse:                  make(map[string]workerAbuseState),
		ipAbuse:                make(map[string]workerAbuseState),
		dropReasonCount:        make(map[string]uint64),
		acceptedResultHashes:   make(map[string]struct{}),
		acceptedFoundNonces:    make(map[uint64]struct{}),
		acceptedSubmitNonces:   make(map[string]struct{}),
		acceptedSignedPayloads: make(map[string]struct{}),
		signedSubmitNonceMax:   make(map[string]uint64),
	}
}

func TestMaybeRetargetPoolModIncreasesOnFastHits(t *testing.T) {
	wm := &workManager{
		targetMod:          1_000_000,
		poolRetarget:       true,
		poolRetargetMinSec: 15,
		rewardAuto:         true,
		baseRewardHMC:      0.01,
		rewardPerM:         0.01,
	}
	now := time.Now().Unix()
	wm.maybeRetargetPoolMod(now)
	if wm.targetMod != 1_000_000 {
		t.Fatalf("first hit should not retarget without prior interval, got %d", wm.targetMod)
	}
	wm.maybeRetargetPoolMod(now + 1)
	if wm.targetMod != 1_000_000 {
		t.Fatalf("second hit inside min window should not retarget, got %d", wm.targetMod)
	}
	wm.targetModUpdatedUnix = now - 20
	wm.lastPoolRetargetUnix = now - 20
	wm.maybeRetargetPoolMod(now + 20)
	if wm.targetMod <= 1_000_000 {
		t.Fatalf("fast hit after min window should increase target_mod, got %d", wm.targetMod)
	}
	if wm.rewardPerM >= 0.01 {
		t.Fatalf("rewardPerM should drop when target_mod rises, got %f", wm.rewardPerM)
	}
}

func TestRefreshTargetModDoesNotDowngradePoolRetargetM(t *testing.T) {
	wm := &workManager{
		targetMod:     1_120_000,
		poolRetarget:  true,
		targetEvery:   1,
		targetLastAt:  0,
		rewardAuto:    true,
		baseRewardHMC: 0.01,
	}
	// Simulate chain metrics with solo-floor mod; pool retarget must keep its M.
	wm.targetMod = 1_120_000
	wm.applyChainTargetMod(251, time.Now().Unix())
	if wm.targetMod != 1_120_000 {
		t.Fatalf("pool retarget must not be overwritten by chain mod, got %d", wm.targetMod)
	}
	wm.poolRetarget = false
	wm.applyChainTargetMod(251, time.Now().Unix())
	if wm.targetMod != poolTargetModMin {
		t.Fatalf("without pool retarget chain mod clamps to pool floor, got %d", wm.targetMod)
	}
}

func TestApplyChainTargetModTracksChainAfterBlake3(t *testing.T) {
	t.Setenv("HACKME_COORDINATOR_TARGET_MOD_FILE", filepath.Join(t.TempDir(), "coordinator_target_mod.txt"))
	// v3 + poolRetarget: chain tip is a raise-only solvability floor.
	// Load retarget owns M; a lower chain tip must not smash pool M down.
	wm := &workManager{
		targetMod:           900_000_000,
		poolRetarget:        true,
		pohFormulaVersion:   3,
		tipHeight:           485_000,
		pohActivationHeight: 485_000,
		rewardAuto:          true,
		baseRewardHMC:       0.01,
		targetModMin:        poolTargetModMin,
		targetModMax:        poolTargetModMax,
	}
	wm.applyChainTargetMod(23_000_000, time.Now().Unix())
	if wm.targetMod != 900_000_000 {
		t.Fatalf("v3+poolRetarget: lower chain tip must not lower pool M, got %d", wm.targetMod)
	}
	wm.applyChainTargetMod(950_000_000, time.Now().Unix())
	if wm.targetMod != 950_000_000 {
		t.Fatalf("v3+poolRetarget: higher chain tip must raise pool M floor, got %d", wm.targetMod)
	}
}

func TestWorkerHashrateGHSForSubmitFormulaBlake3ClampsToWall(t *testing.T) {
	now := int64(1_000_000)
	issued := now - 2
	// Reported 80 GH/s but wall for 4e6 batch / 2s ≈ 0.002 GH/s — must trust wall on v3.
	got := workerHashrateGHSForSubmitFormula(80.0, 4_000_000, issued, now, 0, 3)
	if got > 0.01 {
		t.Fatalf("blake3 must clamp to wall GH/s, got %v", got)
	}
	// Pre-v3 still allows fast-cycle calib trust when wall < 1s.
	gotV1 := workerHashrateGHSForSubmitFormula(80.0, 4_000_000, now, now, 0, 1)
	if gotV1 < 50 {
		t.Fatalf("v1 fast cycle may keep reported calib, got %v", gotV1)
	}
}

func TestPoolMinerCountBoostScalesManyMiners(t *testing.T) {
	if poolMinerCountBoost(1) != 1.0 {
		t.Fatalf("single miner boost want 1.0")
	}
	b12 := poolMinerCountBoost(12)
	b100k := poolMinerCountBoost(100_000)
	if b100k <= b12 {
		t.Fatalf("100k miners should boost more than 12: b12=%v b100k=%v", b12, b100k)
	}
	if b100k > 1.35 {
		t.Fatalf("boost cap 1.35, got %v", b100k)
	}
}

func TestPayoutFoundOnlyDisabledAccruesAttempts(t *testing.T) {
	wm := newTestWorkManagerForPayout(1.0, false)
	base, size, _, _, _, okClaim, _ := wm.claim("w-pay0", 0)
	if !okClaim {
		t.Fatal("claim failed")
	}
	_, reason, payout, _, _ := wm.submit(submitWorkRequest{
		WorkerID: "w-pay0", BaseNonce: base, BatchSize: size,
		WorkID: buildWorkID("w-pay0", base, size), Attempts: size, Found: false,
		ResultHash: "no-hit-hash", HashrateGHS: 5.0,
	})
	if reason != "" {
		t.Fatalf("submit rejected reason=%q", reason)
	}
	want := (float64(size) / 1_000_000.0) * 1.0
	if math.Abs(payout-want) > 1e-12 {
		t.Fatalf("want %f HMC for %d attempts at 1 HMC/M, got %f", want, size, payout)
	}
}

func TestPayoutFoundOnlySkipsAttemptAccrual(t *testing.T) {
	wm := newTestWorkManagerForPayout(1.0, true)
	base, size, _, _, _, okClaim, _ := wm.claim("w-pay1", 0)
	if !okClaim {
		t.Fatal("claim failed")
	}
	_, reason, payout, _, _ := wm.submit(submitWorkRequest{
		WorkerID: "w-pay1", BaseNonce: base, BatchSize: size,
		WorkID: buildWorkID("w-pay1", base, size), Attempts: size, Found: false,
		ResultHash: "no-hit-hash", HashrateGHS: 5.0,
	})
	if reason != "" {
		t.Fatalf("submit rejected reason=%q", reason)
	}
	if payout != 0 {
		t.Fatalf("payout_found_only=1 should skip attempt payout, got %f", payout)
	}
}

func TestPayoutSharesNoSystematicLoss(t *testing.T) {
	wm := newTestWorkManagerForPayout(0.001, false)
	var sum float64
	for i := 0; i < 7; i++ {
		wid := "w-loss-" + strconv.Itoa(i)
		base, size, _, _, _, okClaim, _ := wm.claim(wid, uint64(i)*2000)
		if !okClaim {
			t.Fatalf("claim %s failed", wid)
		}
		_, reason, payout, _, _ := wm.submit(submitWorkRequest{
			WorkerID: wid, BaseNonce: base, BatchSize: size,
			WorkID: buildWorkID(wid, base, size), Attempts: size, Found: false,
			ResultHash: "h-" + wid, HashrateGHS: 2.5,
		})
		if reason != "" {
			t.Fatalf("submit %s reason=%q", wid, reason)
		}
		sum += payout
	}
	st := wm.stats(true)
	total, _ := st["total_payout_hmc"].(float64)
	if math.Abs(sum-total) > 1e-12 {
		t.Fatalf("worker ledger sum %f != pool total_payout_hmc %f", sum, total)
	}
	if sum <= 0 {
		t.Fatal("expected positive pooled payout")
	}
}

func TestPoolDifficultyStableAtSteadyHashrate(t *testing.T) {
	t.Setenv("HACKME_COORDINATOR_POOL_BLOCK_SEC", "30")
	t.Setenv("HACKME_COORDINATOR_POOL_LOAD_SOLVENCY", "1")
	wm := &workManager{
		targetMod:          4_000_000,
		targetModMin:       poolTargetModMin,
		targetModMax:       poolTargetModMax,
		poolRetarget:       true,
		poolRetargetMinSec: 1,
		shareStrict:        true,
	}
	now := time.Now().Unix()
	const poolGH = 12.5
	const miners = 4
	want := poolLoadTargetM(poolGH, miners, poolTargetModMin, true)
	converged := false
	// Solvency-scale M (~3.75e11) needs many 1.35× ramp ticks from 4e6.
	for i := 0; i < 120; i++ {
		prev := wm.targetMod
		wm.maybeRetargetPoolLoadLocked(now+int64(i*30), poolGH, miners)
		if i > 16 && wm.targetMod == prev {
			converged = true
			break
		}
	}
	if !converged {
		t.Fatalf("steady GH/s did not converge within 120 steps, target_mod=%d", wm.targetMod)
	}
	if wm.targetMod < want/2 || wm.targetMod > want*2 {
		t.Fatalf("converged M=%d want ~%d (solvency Hs×block_sec)", wm.targetMod, want)
	}
	if wm.targetMod < 1_000_000_000 {
		t.Fatalf("solvency M too small for 12.5 GH/s: %d", wm.targetMod)
	}
}

func TestPoolLoadLabScaleWhenSolvencyOff(t *testing.T) {
	t.Setenv("HACKME_COORDINATOR_POOL_LOAD_SOLVENCY", "0")
	want := poolLoadTargetM(15, 4, poolTargetModMin, true)
	if want > 100_000_000 {
		t.Fatalf("lab scale should be GH×floor class, got %d", want)
	}
	t.Setenv("HACKME_COORDINATOR_POOL_LOAD_SOLVENCY", "1")
	solv := poolLoadTargetM(15, 4, poolTargetModMin, true)
	if solv < 100_000_000_000 {
		t.Fatalf("default solvency should be Hs×30s class, got %d", solv)
	}
}

func TestPoolDifficultyRisesOnMinerSurge(t *testing.T) {
	wm := &workManager{
		targetMod:          2_000_000,
		targetModMin:       poolTargetModMin,
		targetModMax:       poolTargetModMax,
		poolRetarget:       true,
		poolRetargetMinSec: 1,
	}
	now := time.Now().Unix()
	wm.maybeRetargetPoolLoadLocked(now, 0.5, 1)
	low := wm.targetMod
	wm.maybeRetargetPoolLoadLocked(now+30, 45.0, 18)
	high := wm.targetMod
	if high <= low {
		t.Fatalf("surge should raise target_mod: low=%d high=%d", low, high)
	}
	if poolMinerCountBoost(18) <= poolMinerCountBoost(1) {
		t.Fatal("miner boost should increase with fleet size")
	}
}

func newTestWorkManagerForPayout(rewardPerM float64, payoutFoundOnly bool) *workManager {
	return &workManager{
		defaultBatch:         1000,
		targetMod:            10_000_000, // ≥ batch so attempt-pay economic cap does not bind
		leaseSec:             30,
		rewardPerM:           rewardPerM,
		payoutFoundOnly:      payoutFoundOnly,
		maxWorkers:           1000,
		maxActiveLeases:      1000,
		maxDedupEntries:      1000,
		active:               make(map[workKey]leaseRecord),
		worker:               make(map[string]workerPayoutStat),
		abuse:                make(map[string]workerAbuseState),
		ipAbuse:              make(map[string]workerAbuseState),
		dropReasonCount:      make(map[string]uint64),
		acceptedResultHashes: make(map[string]struct{}),
		acceptedFoundNonces:  make(map[uint64]struct{}),
	}
}

func TestPoolLoadCatchUpAfterRestart(t *testing.T) {
	t.Setenv("HACKME_COORDINATOR_POOL_LOAD_SOLVENCY", "0") // lab scale for this catch-up shape
	wm := &workManager{
		targetMod:          5_000_000, // default after restart
		targetModMin:       poolTargetModMin,
		targetModMax:       poolTargetModMax,
		poolRetarget:       true,
		poolRetargetMinSec: 1,
		poolGHSmoothed:     100,
		shareStrict:        true,
	}
	now := time.Now().Unix()
	// With ~100 GH/s lab scale, load hint ≈ 200M; catch-up should climb much faster than +6%/tick.
	for i := 0; i < 20; i++ {
		wm.maybeRetargetPoolLoadLocked(now+int64(i*20), 100.0, 10)
	}
	if wm.targetMod < 40_000_000 {
		t.Fatalf("expected fast catch-up toward ~200M load hint, got M=%d", wm.targetMod)
	}
}

func TestPoolLoadRetargetRunsUnderBlake3V3(t *testing.T) {
	// Regression: tracksChainTargetMod used to no-op load retarget while UI
	// still advertised target_mod_load_hint ≫ target_mod_min.
	wm := &workManager{
		targetMod:          poolTargetModMin,
		targetModMin:       poolTargetModMin,
		targetModMax:       poolTargetModMax,
		poolRetarget:       true,
		poolRetargetMinSec: 1,
		pohFormulaVersion:  3,
		poolGHSmoothed:     30,
	}
	now := time.Now().Unix()
	if !wm.tracksChainTargetModLocked() {
		t.Fatal("fixture must track chain (v3)")
	}
	for i := 0; i < 24; i++ {
		wm.maybeRetargetPoolLoadLocked(now+int64(i*20), 30.0, 25)
	}
	if wm.targetMod <= poolTargetModMin*2 {
		t.Fatalf("v3 load retarget must raise M off the floor, got %d", wm.targetMod)
	}
	// Chain snap at min must not smash load-raised M back down.
	raised := wm.targetMod
	wm.applyChainTargetMod(poolTargetModMin, now+10_000)
	if wm.targetMod != raised {
		t.Fatalf("chain floor must not lower load-owned M: before=%d after=%d", raised, wm.targetMod)
	}
}

func TestPersistPoolTargetModRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HACKME_COORDINATOR_TARGET_MOD_FILE", filepath.Join(dir, "m.txt"))
	persistPoolTargetMod(123456789)
	got := loadPersistedPoolTargetMod()
	if got != 123456789 {
		t.Fatalf("got %d", got)
	}
}

func TestPoolDifficultyLowGHNoHitRunaway(t *testing.T) {
	wm := &workManager{
		targetMod:          4_000_000,
		targetModMin:       poolTargetModMin,
		targetModMax:       poolTargetModMax,
		poolRetarget:       true,
		poolRetargetMinSec: 1,
		poolGHSmoothed:     0.15,
	}
	now := time.Now().Unix()
	const poolGH = 0.15
	for i := 0; i < 40; i++ {
		wm.maybeRetargetPoolLoadLocked(now+int64(i*30), poolGH, 1)
	}
	loadStable := wm.targetMod
	for i := 0; i < 24; i++ {
		wm.lastFoundHitUnix = now + int64(i*5)
		wm.maybeRetargetPoolMod(now + int64(i*20))
	}
	if wm.targetMod > loadStable*3 {
		t.Fatalf("low reported GH/s + fast hits should not explode M: load=%d after_hits=%d", loadStable, wm.targetMod)
	}
}

func TestPoolLoadHintUnclampedLargeFleet(t *testing.T) {
	t.Setenv("HACKME_COORDINATOR_POOL_LOAD_SOLVENCY", "0")
	// Lab scale: 30 GH/s × 2M → ~60M hint.
	hint := poolLoadHintUnclamped(30.0, 3, poolTargetModMin)
	if hint < 50_000_000 || hint > 100_000_000 {
		t.Fatalf("30 GH/s lab hint want ~60M, got %d", hint)
	}
	hint100k := poolLoadHintUnclamped(30.0, 100_000, poolTargetModMin)
	if hint100k <= hint {
		t.Fatalf("100k miners should raise hint above small fleet: %d vs %d", hint100k, hint)
	}
	t.Setenv("HACKME_COORDINATOR_POOL_LOAD_SOLVENCY", "1")
	solv := poolLoadHintUnclamped(30.0, 3, poolTargetModMin)
	if solv < 100_000_000_000 {
		t.Fatalf("solvency hint for 30 GH/s should be Hs×30s class, got %d", solv)
	}
}

func TestWorkManagerStatsMarksStaleWorkersOffline(t *testing.T) {
	now := time.Now().Unix()
	wm := &workManager{
		defaultBatch:    1000,
		targetMod:       1_000_000,
		leaseSec:        30,
		maxWorkers:      1000,
		maxActiveLeases: 1000,
		maxDedupEntries: 1000,
		active:          make(map[workKey]leaseRecord),
		worker: map[string]workerPayoutStat{
			"live":  {LastSeenUnix: now - 10, LastHashrateGHS: 25.5, PayoutHMC: 1},
			"stale": {LastSeenUnix: now - 600, LastHashrateGHS: 99.0, PayoutHMC: 2},
		},
	}
	st := wm.stats(false)
	workers, _ := st["workers"].(map[string]workerPayoutStat)
	live := workers[publicWorkerKey("live")]
	stale := workers[publicWorkerKey("stale")]
	if !live.Online || live.LastHashrateGHS != 25.5 {
		t.Fatalf("live: %+v", live)
	}
	if stale.Online || stale.LastHashrateGHS != 0 {
		t.Fatalf("stale: %+v", stale)
	}
}

func TestWorkManagerWorkersByPayoutAddress(t *testing.T) {
	wm := &workManager{
		worker: map[string]workerPayoutStat{
			"worker-a": {PayoutAddress: "HMC-abc123", PayoutHMC: 1.5, LastClientIP: "1.2.3.4"},
			"worker-b": {PayoutAddress: "HMC-other", PayoutHMC: 0.1},
			"worker-c": {PayoutAddress: "hmc-abc123", PayoutHMC: 0.2, LastClientIP: "5.6.7.8"},
		},
	}
	got := wm.workersByPayoutAddress("HMC-abc123")
	if len(got) != 2 {
		t.Fatalf("want 2 workers for wallet, got %d", len(got))
	}
	if _, ok := got["worker-a"]; !ok || got["worker-a"].PayoutHMC != 1.5 {
		t.Fatalf("worker-a: %+v", got["worker-a"])
	}
	if _, ok := got["worker-c"]; !ok {
		t.Fatalf("case-insensitive match missing worker-c")
	}
	if got["worker-a"].LastClientIP != "" || got["worker-c"].LastClientIP != "" {
		t.Fatalf("public by-wallet must redact last_client_ip: a=%q c=%q", got["worker-a"].LastClientIP, got["worker-c"].LastClientIP)
	}
}

func TestPublicStatsRedactsClientIP(t *testing.T) {
	now := time.Now().Unix()
	wm := &workManager{
		worker: map[string]workerPayoutStat{
			"rig-1": {LastSeenUnix: now - 5, LastHashrateGHS: 10, PayoutHMC: 1, LastClientIP: "203.0.113.9", PayoutAddress: "HMC-pub"},
		},
	}
	pub := wm.stats(false)
	workers, _ := pub["workers"].(map[string]workerPayoutStat)
	pseudo := publicWorkerKey("rig-1")
	if _, ok := workers["rig-1"]; ok {
		t.Fatal("public stats must not expose raw worker id keys")
	}
	st, ok := workers[pseudo]
	if !ok {
		t.Fatalf("missing pseudonym key %s in %#v", pseudo, workers)
	}
	if st.LastClientIP != "" {
		t.Fatalf("public stats leaked last_client_ip: %+v", st)
	}
	if st.PayoutAddress != "HMC-pub" || st.PayoutHMC != 1 {
		t.Fatalf("public payout fields should remain: %+v", st)
	}
	if pub["workers_keys"] != "pseudonym_v1" {
		t.Fatalf("workers_keys=%v", pub["workers_keys"])
	}
	priv := wm.stats(true)
	workers, _ = priv["workers"].(map[string]workerPayoutStat)
	if workers["rig-1"].LastClientIP != "203.0.113.9" {
		t.Fatalf("details=1 should keep last_client_ip for admin: %+v", workers["rig-1"])
	}
}

func TestPayoutAddressLockedAfterFirstSignedSubmit(t *testing.T) {
	pub1, priv1, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	pub2, priv2, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	wm := &workManager{
		defaultBatch:           1000,
		targetMod:              1_000_000,
		leaseSec:               30,
		maxWorkers:             1000,
		maxActiveLeases:        1000,
		maxDedupEntries:        1000,
		hybridSignerEnabled:    true,
		hybridSignerStrict:     true,
		hybridRequireFoundSig:  true,
		active:                 make(map[workKey]leaseRecord),
		worker:                 make(map[string]workerPayoutStat),
		acceptedResultHashes:   make(map[string]struct{}),
		acceptedFoundNonces:    make(map[uint64]struct{}),
		acceptedSubmitNonces:   make(map[string]struct{}),
		acceptedSignedPayloads: make(map[string]struct{}),
		signedSubmitNonceMax:   make(map[string]uint64),
	}
	base, size, _, _, _, okClaim, _ := wm.claim("w-lock", 1000)
	if !okClaim {
		t.Fatal("claim failed")
	}
	req1 := submitWorkRequest{
		WorkerID: "w-lock", BaseNonce: base, BatchSize: size, WorkID: buildWorkID("w-lock", base, size),
		Attempts: size, MinerPubKey: hex.EncodeToString(pub1), MinerAddress: signerAddr(pub1), SubmitNonce: 1,
	}
	req1.MinerSig = hex.EncodeToString(ed25519.Sign(priv1, canonicalSubmitBytes(req1)))
	_, reason, _, _, _ := wm.submit(req1)
	if reason != "" {
		t.Fatalf("first signed submit: reason=%q", reason)
	}
	base2, size2, _, _, _, okClaim, _ := wm.claim("w-lock", 1000)
	if !okClaim {
		t.Fatal("second claim failed")
	}
	req2 := submitWorkRequest{
		WorkerID: "w-lock", BaseNonce: base2, BatchSize: size2, WorkID: buildWorkID("w-lock", base2, size2),
		Attempts: size2, MinerPubKey: hex.EncodeToString(pub2), MinerAddress: signerAddr(pub2), SubmitNonce: 2,
	}
	req2.MinerSig = hex.EncodeToString(ed25519.Sign(priv2, canonicalSubmitBytes(req2)))
	_, reason, _, _, _ = wm.submit(req2)
	if !strings.HasPrefix(reason, "payout_address_locked") {
		t.Fatalf("want payout_address_locked, got reason=%q", reason)
	}
}

func TestClaimMinerIdentityLocked(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	wm := &workManager{
		hybridSignerEnabled: true,
		claimRequirePubKey:  true,
		worker: map[string]workerPayoutStat{
			"w1": {PayoutAddress: signerAddr(pub)},
		},
	}
	if ok, reason := wm.checkClaimMinerIdentity("w1", "", ""); ok || reason != "claim_pubkey_required" {
		t.Fatalf("require pubkey: ok=%v reason=%q", ok, reason)
	}
	// Address-only must not establish (or satisfy) a lock without proving the key.
	if ok, reason := wm.checkClaimMinerIdentity("unlocked-rig", "", "HMC-aaaaaaaaaaaaaaaa"); ok || reason != "claim_pubkey_required" {
		t.Fatalf("address-only under require must fail: ok=%v reason=%q", ok, reason)
	}
	// Even with require off, address-only must not write a sticky lock.
	wmOff := &workManager{hybridSignerEnabled: true, claimRequirePubKey: false, worker: map[string]workerPayoutStat{}}
	if ok, reason := wmOff.checkClaimMinerIdentity("legacy-rig", "", "HMC-bbbbbbbbbbbbbbbb"); !ok {
		t.Fatalf("legacy address-only claim should pass without locking: %s", reason)
	}
	if got := wmOff.lockedPayoutAddress("legacy-rig"); got != "" {
		t.Fatalf("address-only must not set payout lock, got %q", got)
	}
	pub2, _, _ := ed25519.GenerateKey(nil)
	if ok, reason := wm.checkClaimMinerIdentity("w1", hex.EncodeToString(pub2), ""); ok || !strings.HasPrefix(reason, "payout_address_locked") {
		t.Fatalf("locked mismatch: ok=%v reason=%q", ok, reason)
	}
	if ok, reason := wm.checkClaimMinerIdentity("w1", hex.EncodeToString(pub), ""); !ok {
		t.Fatalf("matching pubkey should pass: %s", reason)
	}
}

func TestMergeWorkerStatAddressConflictDoesNotStealPayout(t *testing.T) {
	victim := workerPayoutStat{PayoutAddress: "HMC-victim000000000", PayoutHMC: 10, PayoutSUP: 1, LastHashrateGHS: 100}
	attacker := workerPayoutStat{PayoutAddress: "HMC-attacker0000000", PayoutHMC: 0.1, PayoutSUP: 0.01, LastHashrateGHS: 1}
	merged, conflict := mergeWorkerStat(attacker, victim)
	if !conflict || !merged.AddressConflict {
		t.Fatalf("expected conflict flag, conflict=%v addrConflict=%v", conflict, merged.AddressConflict)
	}
	if merged.PayoutAddress != "" {
		t.Fatalf("conflict must clear payout address, got %q", merged.PayoutAddress)
	}
	if merged.PayoutHMC < 10.0 {
		t.Fatalf("accrual must remain visible for ops (not zeroed), got hmc=%v", merged.PayoutHMC)
	}
	merged2, conflict2 := mergeWorkerStat(victim, attacker)
	if !conflict2 || merged2.PayoutAddress != "" {
		t.Fatalf("conflict2 must clear address, got addr=%q conflict=%v", merged2.PayoutAddress, conflict2)
	}
	richAttacker := workerPayoutStat{PayoutAddress: "HMC-attacker0000000", PayoutHMC: 99, PayoutSUP: 9, LastHashrateGHS: 1}
	merged3, conflict3 := mergeWorkerStat(victim, richAttacker)
	if !conflict3 {
		t.Fatal("expected address conflict")
	}
	if merged3.PayoutAddress != "" {
		t.Fatalf("conflict must blank address so settle cannot pay wrong target, got %q", merged3.PayoutAddress)
	}
	if merged3.PayoutHMC < 10 {
		t.Fatalf("victim accrual must remain visible: %v", merged3.PayoutHMC)
	}
	// Report #17: after the address is blanked, the next fleet row must not
	// reinstall the attacker's payout.
	again, _ := mergeWorkerStat(merged3, richAttacker)
	if again.PayoutAddress != "" || !again.AddressConflict {
		t.Fatalf("conflict must stay sticky, got addr=%q conflict=%v", again.PayoutAddress, again.AddressConflict)
	}
}

func TestPayoutLockSurvivesRestartAndPrune(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "locks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	wm := newWorkManagerFromEnv()
	wm.attachDedupDB(db)
	const victim = "HMC-victim000000000"
	wm.persistPayoutLock("rig1", victim)
	wm2 := newWorkManagerFromEnv()
	wm2.attachDedupDB(db)
	pub, _, _ := ed25519.GenerateKey(nil)
	ok, reason := wm2.checkClaimMinerIdentity("rig1", hex.EncodeToString(pub), "")
	if ok || !strings.HasPrefix(reason, "payout_address_locked") {
		t.Fatalf("restart must keep lock: ok=%v reason=%q", ok, reason)
	}
	wm2.mu.Lock()
	delete(wm2.worker, "rig1")
	wm2.mu.Unlock()
	ok, reason = wm2.checkClaimMinerIdentity("rig1", hex.EncodeToString(pub), "")
	if ok || !strings.HasPrefix(reason, "payout_address_locked") {
		t.Fatalf("prune must not free the durable lock: ok=%v reason=%q", ok, reason)
	}
}

func TestStatsWorkersCountMatchesMergedMap(t *testing.T) {
	wm := newTestWorkManagerForPayout(1e-5, false)
	now := time.Now().Unix()
	wm.mu.Lock()
	wm.worker["rig-a"] = workerPayoutStat{LastSeenUnix: now, LastHashrateGHS: 10, PayoutAddress: "HMC-aaa"}
	wm.worker["rig-a-gpu0"] = workerPayoutStat{LastSeenUnix: now, LastHashrateGHS: 11, PayoutAddress: "HMC-aaa"}
	wm.worker["rig-b"] = workerPayoutStat{LastSeenUnix: now, LastHashrateGHS: 5, PayoutAddress: "HMC-bbb"}
	wm.mu.Unlock()
	st := wm.stats(false)
	workers, _ := st["workers"].(map[string]workerPayoutStat)
	if len(workers) != 2 {
		t.Fatalf("merged workers want 2 got %d keys=%v", len(workers), workers)
	}
	if got := st["workers_count"]; got != 2 {
		t.Fatalf("workers_count want 2 (merged), got %v (tracked raw=%v)", got, st["workers_tracked"])
	}
	if got := st["workers_tracked"]; got != 3 {
		t.Fatalf("workers_tracked want 3 raw entries, got %v", got)
	}
}
