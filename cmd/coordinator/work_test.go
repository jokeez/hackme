package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"testing"
	"time"

	"hackme/internal/chain"
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
	wm := &workManager{
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
	}
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
		WorkerID:   "w1",
		BaseNonce:  base,
		BatchSize:  size,
		WorkID:     buildWorkID("w1", base, size),
		Attempts:   500,
		Found:      true,
		FoundNonce: foundNonce,
		ResultHash: "abc123",
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
	wm := &workManager{
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
	}
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
	wm := &workManager{
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
	}
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
		submitPerMin:    100,
		banSec:          60,
		badStrikesToBan: 2,
		maxWorkers:      1000,
		maxActiveLeases: 1000,
		maxDedupEntries: 1000,
		active:          make(map[workKey]leaseRecord),
		worker:          make(map[string]workerPayoutStat),
		abuse:           make(map[string]workerAbuseState),
	}
	now := int64(1_700_000_000)
	wm.markSubmitOutcome("w-abuse", "work_id_mismatch", now)
	wm.markSubmitOutcome("w-abuse", "unknown_or_already_closed_range", now)
	if ok, reason := wm.allowSubmit("w-abuse", "", now+1); ok || reason != "worker_temporarily_banned" {
		t.Fatalf("expected worker_temporarily_banned, got ok=%v reason=%q", ok, reason)
	}
	if ok, _ := wm.allowSubmit("w-abuse", "", now+61); !ok {
		t.Fatal("ban should expire")
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
