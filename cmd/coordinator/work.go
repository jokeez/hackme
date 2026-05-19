package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"hackme/internal/chain"
	"hackme/internal/lanpool"
	"hackme/internal/worksubmit"
)

const maxCoordinatorJSONBodyBytes = 1 << 20 // 1 MiB

type workManager struct {
	nextNonce atomic.Uint64

	defaultBatch    uint64
	targetMod       uint64
	targetURL       string
	targetEvery     int64
	targetLastAt    int64
	leaseSec        int64
	rewardPerM      float64 // HMC per 1,000,000 accepted attempts
	foundBonus      float64 // HMC bonus when found=true on accepted submit
	baseRewardHMC   float64
	rewardAuto      bool
	payoutFoundOnly bool

	mu sync.Mutex

	// active holds outstanding leases by (base,batch).
	active map[workKey]leaseRecord
	// worker keeps reward/work counters per worker_id.
	worker map[string]workerPayoutStat
	// acceptedResultHashes deduplicates useful submits globally.
	acceptedResultHashes   map[string]struct{}
	acceptedFoundNonces    map[uint64]struct{}
	acceptedSubmitNonces   map[string]struct{}
	acceptedSignedPayloads map[string]struct{}
	signedSubmitNonceMax   map[string]uint64
	lastSignedMiner        string

	issuedRanges    uint64
	reissuedRanges  uint64
	submittedItems  uint64
	foundHits       uint64
	expiredLeases   uint64
	unknownSubmits  uint64
	staleSubmits    uint64
	rejectedSubmits uint64
	totalAttempts   uint64
	totalPayoutHMC  float64
	dedupSubmits    uint64
	dedupFoundNonce uint64
	signedAccepts   uint64
	signedRejects   uint64

	claimPerMin     int
	submitPerMin    int
	banSec          int64
	badStrikesToBan int
	abuse           map[string]workerAbuseState
	ipAbuse         map[string]workerAbuseState

	maxWorkers               int
	maxActiveLeases          int
	maxActiveLeasesPerWorker int
	maxDedupEntries          int

	dropReasonCount   map[string]uint64
	ingressInFlight   atomic.Int64
	ackLatencyMsSum   atomic.Uint64
	ackLatencySamples atomic.Uint64

	schedulerMu          sync.Mutex
	schedulerMode        string
	schedulerTransitions uint64
	ordersPriority       bool
	ordersProbeURL       string
	ordersProbeEverySec  int64
	lastOrdersProbeUnix  int64
	lastOrdersActive     bool
	probeInFlight        bool

	lastAbusePruneUnix    int64
	hybridSignerEnabled   bool
	hybridSignerStrict    bool
	hybridRequireFoundSig bool
}

type workKey struct {
	base  uint64
	batch uint64
}

type leaseRecord struct {
	WorkerID  string `json:"worker_id"`
	BaseNonce uint64 `json:"base_nonce"`
	BatchSize uint64 `json:"batch_size"`
	ExpiresAt int64  `json:"expires_at"`
	Reissues  uint64 `json:"reissues"`
}

type workerPayoutStat struct {
	AcceptedRanges uint64  `json:"accepted_ranges"`
	AcceptedHits   uint64  `json:"accepted_hits"`
	AcceptedAtt    uint64  `json:"accepted_attempts"`
	PayoutHMC      float64 `json:"payout_hmc"`
	PayoutAddress  string  `json:"payout_address,omitempty"`
	SignedSubmits  uint64  `json:"signed_submits,omitempty"`
}

type workerAbuseState struct {
	MinuteUnix  int64 `json:"minute_unix"`
	ClaimCount  int   `json:"claim_count"`
	SubmitCount int   `json:"submit_count"`
	BadStrikes  int   `json:"bad_strikes"`
	BannedUntil int64 `json:"banned_until,omitempty"`
}

type claimWorkRequest struct {
	WorkerID  string `json:"worker_id"`
	BatchSize uint64 `json:"batch_size,omitempty"`
}

type submitWorkRequest struct {
	WorkerID     string  `json:"worker_id"`
	BaseNonce    uint64  `json:"base_nonce"`
	BatchSize    uint64  `json:"batch_size"`
	WorkID       string  `json:"work_id,omitempty"`
	Attempts     uint64  `json:"attempts,omitempty"`
	Found        bool    `json:"found,omitempty"`
	FoundNonce   uint64  `json:"found_nonce,omitempty"`
	ResultHash   string  `json:"result_hash,omitempty"` // unique proof/result key for useful workloads
	ProofHash    string  `json:"proof_hash,omitempty"`  // optional hash of repro artifact/log bundle
	HashrateGHS  float64 `json:"hashrate_gh_s,omitempty"`
	MinerAddress string  `json:"miner_address,omitempty"`
	MinerPubKey  string  `json:"miner_pubkey_ed25519,omitempty"`
	MinerSig     string  `json:"miner_sig_ed25519,omitempty"`
	MinerSigAlg  string  `json:"miner_sig_alg,omitempty"`
	SubmitNonce  uint64  `json:"submit_nonce,omitempty"`
}

func newWorkManagerFromEnv() *workManager {
	batch := uint64(1 << 22)
	if v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_WORK_BATCH")); v != "" {
		if x, err := strconv.ParseUint(v, 10, 64); err == nil && x >= 1000 {
			batch = x
		}
	}
	mod := uint64(1_000_000)
	if v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_TARGET_MOD")); v != "" {
		if x, err := strconv.ParseUint(v, 10, 64); err == nil && x > 0 {
			mod = x
		}
	}
	targetURL := strings.TrimRight(strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_TARGET_SOURCE_URL")), "/")
	targetEvery := int64(3)
	if v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_TARGET_REFRESH_SEC")); v != "" {
		if x, err := strconv.ParseInt(v, 10, 64); err == nil && x >= 1 {
			targetEvery = x
		}
	}
	lease := int64(30)
	if v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_LEASE_SEC")); v != "" {
		if x, err := strconv.ParseInt(v, 10, 64); err == nil && x > 0 {
			lease = x
		}
	}
	rewardPerM := 0.001
	if v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_REWARD_PER_M_ATTEMPTS")); v != "" {
		if x, err := strconv.ParseFloat(v, 64); err == nil && x >= 0 {
			rewardPerM = x
		}
	}
	foundBonus := 0.01
	rewardAuto := true
	if v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_COORDINATOR_REWARD_AUTO"))); v != "" {
		rewardAuto = v == "1" || v == "true" || v == "yes" || v == "on"
	}
	if v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_FOUND_BONUS_HMC")); v != "" {
		if x, err := strconv.ParseFloat(v, 64); err == nil && x >= 0 {
			foundBonus = x
		}
	}
	payoutFoundOnly := false
	if v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_COORDINATOR_PAYOUT_FOUND_ONLY"))); v != "" {
		payoutFoundOnly = v == "1" || v == "true" || v == "yes" || v == "on"
	}
	claimPerMin := 120
	if v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_CLAIM_PER_MIN")); v != "" {
		if x, err := strconv.Atoi(v); err == nil && x >= 10 {
			claimPerMin = x
		}
	}
	submitPerMin := 600
	if v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_SUBMIT_PER_MIN")); v != "" {
		if x, err := strconv.Atoi(v); err == nil && x >= 10 {
			submitPerMin = x
		}
	}
	banSec := int64(120)
	if v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_BAN_SEC")); v != "" {
		if x, err := strconv.ParseInt(v, 10, 64); err == nil && x >= 10 {
			banSec = x
		}
	}
	badStrikesToBan := 12
	if v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_BAD_STRIKES_TO_BAN")); v != "" {
		if x, err := strconv.Atoi(v); err == nil && x >= 3 {
			badStrikesToBan = x
		}
	}
	maxWorkers := 200000
	if v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_MAX_WORKERS")); v != "" {
		if x, err := strconv.Atoi(v); err == nil && x >= 1000 {
			maxWorkers = x
		}
	}
	maxActiveLeases := 500000
	if v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_MAX_ACTIVE_LEASES")); v != "" {
		if x, err := strconv.Atoi(v); err == nil && x >= 1000 {
			maxActiveLeases = x
		}
	}
	maxActiveLeasesPerWorker := 0
	if v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_MAX_ACTIVE_LEASES_PER_WORKER")); v != "" {
		if x, err := strconv.Atoi(v); err == nil && x >= 1 {
			maxActiveLeasesPerWorker = x
		}
	}
	maxDedupEntries := 500000
	if v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_MAX_DEDUP_ENTRIES")); v != "" {
		if x, err := strconv.Atoi(v); err == nil && x >= 1000 {
			maxDedupEntries = x
		}
	}
	ordersPriority := true
	if v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_COORDINATOR_ORDERS_PRIORITY"))); v != "" {
		ordersPriority = v == "1" || v == "true" || v == "yes" || v == "on"
	}
	ordersProbeURL := strings.TrimRight(strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_ORDERS_URL")), "/")
	ordersProbeEverySec := int64(3)
	if v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_ORDERS_PROBE_SEC")); v != "" {
		if x, err := strconv.ParseInt(v, 10, 64); err == nil && x >= 1 {
			ordersProbeEverySec = x
		}
	}
	hybridSignerEnabled := false
	if v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_POOL_HYBRID_SIGNER_ENABLED"))); v != "" {
		hybridSignerEnabled = v == "1" || v == "true" || v == "yes" || v == "on"
	}
	hybridSignerStrict := false
	if v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_POOL_HYBRID_SIGNER_STRICT"))); v != "" {
		hybridSignerStrict = v == "1" || v == "true" || v == "yes" || v == "on"
	}
	hybridRequireFoundSig := true
	if v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_POOL_HYBRID_REQUIRE_FOUND_SIG"))); v != "" {
		hybridRequireFoundSig = v == "1" || v == "true" || v == "yes" || v == "on"
	}
	return &workManager{
		defaultBatch:             batch,
		targetMod:                mod,
		targetURL:                targetURL,
		targetEvery:              targetEvery,
		leaseSec:                 lease,
		rewardPerM:               rewardPerM,
		foundBonus:               foundBonus,
		baseRewardHMC:            0.01,
		rewardAuto:               rewardAuto,
		payoutFoundOnly:          payoutFoundOnly,
		active:                   make(map[workKey]leaseRecord),
		worker:                   make(map[string]workerPayoutStat),
		acceptedResultHashes:     make(map[string]struct{}),
		acceptedFoundNonces:      make(map[uint64]struct{}),
		acceptedSubmitNonces:     make(map[string]struct{}),
		acceptedSignedPayloads:   make(map[string]struct{}),
		signedSubmitNonceMax:     make(map[string]uint64),
		claimPerMin:              claimPerMin,
		submitPerMin:             submitPerMin,
		banSec:                   banSec,
		badStrikesToBan:          badStrikesToBan,
		abuse:                    make(map[string]workerAbuseState),
		ipAbuse:                  make(map[string]workerAbuseState),
		maxWorkers:               maxWorkers,
		maxActiveLeases:          maxActiveLeases,
		maxActiveLeasesPerWorker: maxActiveLeasesPerWorker,
		maxDedupEntries:          maxDedupEntries,
		dropReasonCount:          make(map[string]uint64),
		ordersPriority:           ordersPriority,
		ordersProbeURL:           ordersProbeURL,
		ordersProbeEverySec:      ordersProbeEverySec,
		schedulerMode:            "baseline",
		hybridSignerEnabled:      hybridSignerEnabled,
		hybridSignerStrict:       hybridSignerStrict,
		hybridRequireFoundSig:    hybridRequireFoundSig,
	}
}

func validFoundNonceV1(foundNonce, targetMod uint64) bool {
	if targetMod == 0 {
		return false
	}
	// Coordinator baseline: v1 PoH eval (7n+13) divisibility check.
	// This makes payout depend on actually finding a valid hit.
	return chain.PohEval(foundNonce)%targetMod == 0
}

func (m *workManager) refreshTargetMod(now int64) {
	if strings.TrimSpace(m.targetURL) == "" {
		return
	}
	if now-m.targetLastAt < m.targetEvery {
		return
	}
	cl := &http.Client{Timeout: 1200 * time.Millisecond}
	resp, err := cl.Get(m.targetURL + "/api/metrics")
	if err != nil {
		m.targetLastAt = now
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		m.targetLastAt = now
		return
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		m.targetLastAt = now
		return
	}
	v, ok := body["mining_target_mod"]
	if !ok {
		m.targetLastAt = now
		return
	}
	var parsed uint64
	switch t := v.(type) {
	case float64:
		if t > 0 {
			parsed = uint64(t)
		}
	case json.Number:
		if x, err := t.Int64(); err == nil && x > 0 {
			parsed = uint64(x)
		}
	}
	if parsed > 0 {
		m.targetMod = parsed
	}
	if br, ok := body["econ_base_reward_now_hmc"]; ok {
		switch t := br.(type) {
		case float64:
			if t >= 0 {
				m.baseRewardHMC = t
			}
		case json.Number:
			if x, err := t.Float64(); err == nil && x >= 0 {
				m.baseRewardHMC = x
			}
		}
	}
	// Auto-payout mode ties worker payout to canonical economics:
	// expected payout per attempt ~= base_reward / target_mod.
	// per 1,000,000 attempts => base_reward * 1e6 / target_mod.
	if m.rewardAuto && m.targetMod > 0 && m.baseRewardHMC > 0 {
		m.rewardPerM = (m.baseRewardHMC * 1_000_000.0) / float64(m.targetMod)
		if m.rewardPerM < 0 {
			m.rewardPerM = 0
		}
	}
	m.targetLastAt = now
}

func keyFromRemoteAddr(remoteAddr string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err != nil {
		return strings.TrimSpace(remoteAddr)
	}
	return strings.TrimSpace(host)
}

func (m *workManager) allowRateSlot(state workerAbuseState, now int64, limit int, claim bool) (workerAbuseState, bool) {
	if state.BannedUntil > now {
		return state, false
	}
	minute := now / 60
	if state.MinuteUnix != minute {
		state.MinuteUnix = minute
		state.ClaimCount = 0
		state.SubmitCount = 0
	}
	if claim {
		state.ClaimCount++
		if state.ClaimCount > limit {
			return state, false
		}
	} else {
		state.SubmitCount++
		if state.SubmitCount > limit {
			return state, false
		}
	}
	return state, true
}

func (m *workManager) allowClaim(workerID, ipKey string, now int64) (bool, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneAbuseStateLocked(now)
	if s := m.abuse[workerID]; s.BannedUntil > now {
		return false, "worker_temporarily_banned"
	}
	s, ok := m.allowRateSlot(m.abuse[workerID], now, m.claimPerMin, true)
	m.abuse[workerID] = s
	if !ok {
		return false, "claim_rate_limited"
	}
	if ipKey == "" {
		return true, ""
	}
	if ip := m.ipAbuse[ipKey]; ip.BannedUntil > now {
		return false, "worker_temporarily_banned"
	}
	ip, ok := m.allowRateSlot(m.ipAbuse[ipKey], now, m.claimPerMin*4, true)
	m.ipAbuse[ipKey] = ip
	if !ok {
		return false, "claim_rate_limited"
	}
	return true, ""
}

func (m *workManager) allowSubmit(workerID, ipKey string, now int64) (bool, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneAbuseStateLocked(now)
	if s := m.abuse[workerID]; s.BannedUntil > now {
		return false, "worker_temporarily_banned"
	}
	s, ok := m.allowRateSlot(m.abuse[workerID], now, m.submitPerMin, false)
	m.abuse[workerID] = s
	if !ok {
		return false, "submit_rate_limited"
	}
	if ipKey == "" {
		return true, ""
	}
	if ip := m.ipAbuse[ipKey]; ip.BannedUntil > now {
		return false, "worker_temporarily_banned"
	}
	ip, ok := m.allowRateSlot(m.ipAbuse[ipKey], now, m.submitPerMin*4, false)
	m.ipAbuse[ipKey] = ip
	if !ok {
		return false, "submit_rate_limited"
	}
	return true, ""
}

func (m *workManager) recordDrop(reason string) {
	if strings.TrimSpace(reason) == "" {
		return
	}
	m.mu.Lock()
	m.dropReasonCount[reason]++
	m.mu.Unlock()
}

func (m *workManager) markSubmitOutcome(workerID, reason string, now int64) {
	if reason == "" {
		return
	}
	switch reason {
	case "unknown_or_already_closed_range", "work_id_mismatch", "range_leased_to_another_worker", "found_nonce_out_of_range", "result_hash_required_for_found", "duplicate_found_nonce", "invalid_signature", "invalid_pubkey", "pubkey_address_mismatch", "missing_signature_fields", "signature_required", "found_signature_required", "replay", "duplicate_signed_payload":
		// Penalize clearly bad / abusive submit patterns.
	default:
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.abuse[workerID]
	s.BadStrikes++
	if s.BadStrikes >= m.badStrikesToBan {
		s.BannedUntil = now + m.banSec
		s.BadStrikes = 0
	}
	m.abuse[workerID] = s
}

func buildWorkID(workerID string, base, batch uint64) string {
	return workerID + ":" + strconv.FormatUint(base, 10) + "+" + strconv.FormatUint(batch, 10)
}

func buildChunkID(base, batch, mod uint64) string {
	sum := sha256.Sum256([]byte(strconv.FormatUint(base, 10) + "|" + strconv.FormatUint(batch, 10) + "|" + strconv.FormatUint(mod, 10)))
	return hex.EncodeToString(sum[:16])
}

func deriveAddressFromPubHex(pubHex string) (string, bool) {
	pubHex = strings.TrimSpace(pubHex)
	if pubHex == "" {
		return "", false
	}
	pub, err := hex.DecodeString(pubHex)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return "", false
	}
	sum := sha256.Sum256(pub)
	return "HMC-" + hex.EncodeToString(sum[:])[:16], true
}

func canonicalSubmitBytes(req submitWorkRequest) []byte {
	rh, ph := worksubmit.NormalizeHashes(req.ResultHash, req.ProofHash)
	p := worksubmit.SignPayload{
		WorkerID:    strings.TrimSpace(req.WorkerID),
		BaseNonce:   req.BaseNonce,
		BatchSize:   req.BatchSize,
		WorkID:      strings.TrimSpace(req.WorkID),
		Attempts:    req.Attempts,
		Found:       req.Found,
		FoundNonce:  req.FoundNonce,
		ResultHash:  rh,
		ProofHash:   ph,
		SubmitNonce: req.SubmitNonce,
	}
	return p.CanonicalJSON()
}

func (m *workManager) validateHybridSignature(req submitWorkRequest) (ok bool, reason string, signerAddr string) {
	if !m.hybridSignerEnabled {
		return true, "", ""
	}
	pubHex := strings.TrimSpace(req.MinerPubKey)
	sigHex := strings.TrimSpace(req.MinerSig)
	addr := strings.TrimSpace(req.MinerAddress)
	hasSigFields := !(pubHex == "" && sigHex == "" && addr == "" && req.SubmitNonce == 0)
	if !hasSigFields && m.hybridSignerStrict {
		return false, "signature_required", ""
	}
	if req.Found && !hasSigFields && m.hybridRequireFoundSig {
		return false, "found_signature_required", ""
	}
	if !hasSigFields {
		return true, "", ""
	}
	if pubHex == "" || sigHex == "" || req.SubmitNonce == 0 {
		return false, "missing_signature_fields", ""
	}
	alg := strings.TrimSpace(strings.ToLower(req.MinerSigAlg))
	if alg == "" {
		alg = "ed25519"
	}
	if alg != "ed25519" {
		return false, "unsupported_sig_alg", ""
	}
	derivedAddr, okAddr := deriveAddressFromPubHex(pubHex)
	if !okAddr {
		return false, "invalid_pubkey", ""
	}
	if addr != "" && !strings.EqualFold(addr, derivedAddr) {
		return false, "pubkey_address_mismatch", ""
	}
	sig, err := hex.DecodeString(sigHex)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false, "invalid_signature", ""
	}
	pub, _ := hex.DecodeString(pubHex)
	if !ed25519.Verify(ed25519.PublicKey(pub), canonicalSubmitBytes(req), sig) {
		return false, "invalid_signature", ""
	}
	if maxNonce, ok := m.signedSubmitNonceMax[derivedAddr]; ok && req.SubmitNonce <= maxNonce {
		return false, "replay", ""
	}
	nonceKey := derivedAddr + ":" + strconv.FormatUint(req.SubmitNonce, 10)
	if _, exists := m.acceptedSubmitNonces[nonceKey]; exists {
		return false, "replay", ""
	}
	canon := canonicalSubmitBytes(req)
	sum := sha256.Sum256(append(canon, sig...))
	sigPayload := hex.EncodeToString(sum[:])
	if _, exists := m.acceptedSignedPayloads[sigPayload]; exists {
		return false, "duplicate_signed_payload", ""
	}
	return true, "", derivedAddr
}

func (m *workManager) claim(workerID string, batch uint64) (base uint64, size uint64, leaseUntil int64, targetMod uint64, reissued bool, ok bool, reason string) {
	if batch == 0 {
		batch = m.defaultBatch
	}
	now := time.Now().Unix()
	m.mu.Lock()
	m.refreshTargetMod(now)
	if _, exists := m.worker[workerID]; !exists && len(m.worker) >= m.maxWorkers {
		m.mu.Unlock()
		return 0, 0, 0, m.targetMod, false, false, "too_many_workers"
	}
	if len(m.active) >= m.maxActiveLeases {
		m.mu.Unlock()
		return 0, 0, 0, m.targetMod, false, false, "too_many_active_leases"
	}
	if m.maxActiveLeasesPerWorker > 0 {
		wleases := 0
		for _, rec := range m.active {
			if rec.WorkerID == workerID && rec.ExpiresAt > now {
				wleases++
			}
		}
		if wleases >= m.maxActiveLeasesPerWorker {
			m.mu.Unlock()
			return 0, 0, 0, m.targetMod, false, false, "too_many_worker_leases"
		}
	}
	// Reuse expired range of the same batch first.
	for k, rec := range m.active {
		if rec.BatchSize != batch {
			continue
		}
		if rec.ExpiresAt > now {
			continue
		}
		m.expiredLeases++
		rec.WorkerID = workerID
		rec.ExpiresAt = now + m.leaseSec
		rec.Reissues++
		m.active[k] = rec
		m.reissuedRanges++
		m.issuedRanges++
		m.mu.Unlock()
		return rec.BaseNonce, rec.BatchSize, rec.ExpiresAt, m.targetMod, true, true, ""
	}
	next := m.nextNonce.Load()
	if batch > math.MaxUint64-next {
		m.mu.Unlock()
		return 0, 0, 0, m.targetMod, false, false, "nonce_space_exhausted"
	}
	base = next
	m.nextNonce.Store(next + batch)
	leaseUntil = now + m.leaseSec
	m.active[workKey{base: base, batch: batch}] = leaseRecord{
		WorkerID:  workerID,
		BaseNonce: base,
		BatchSize: batch,
		ExpiresAt: leaseUntil,
	}
	m.issuedRanges++
	m.mu.Unlock()
	return base, batch, leaseUntil, m.targetMod, false, true, ""
}

func (m *workManager) submit(req submitWorkRequest) (accepted bool, reason string, payoutHMC float64, signerOut string, signed bool) {
	k := workKey{base: req.BaseNonce, batch: req.BatchSize}
	now := time.Now().Unix()
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.active[k]
	if !ok {
		m.unknownSubmits++
		return false, "unknown_or_already_closed_range", 0, "", false
	}
	if rec.WorkerID != req.WorkerID {
		m.rejectedSubmits++
		return false, "range_leased_to_another_worker", 0, "", false
	}
	if wid := strings.TrimSpace(req.WorkID); wid != "" && wid != buildWorkID(req.WorkerID, req.BaseNonce, req.BatchSize) {
		m.rejectedSubmits++
		return false, "work_id_mismatch", 0, "", false
	}
	sigOK, sigReason, signerAddr := m.validateHybridSignature(req)
	if !sigOK {
		m.signedRejects++
		m.rejectedSubmits++
		return false, sigReason, 0, "", false
	}
	if rec.ExpiresAt < now {
		delete(m.active, k)
		m.staleSubmits++
		return false, "lease_expired", 0, "", false
	}
	if req.Found {
		if _, exists := m.acceptedFoundNonces[req.FoundNonce]; exists {
			m.dedupFoundNonce++
			return false, "duplicate_found_nonce", 0, "", false
		}
		rangeEnd := req.BaseNonce + req.BatchSize
		if rangeEnd < req.BaseNonce {
			m.rejectedSubmits++
			return false, "invalid_nonce_range", 0, "", false
		}
		if req.FoundNonce < req.BaseNonce || req.FoundNonce >= rangeEnd {
			m.rejectedSubmits++
			return false, "found_nonce_out_of_range", 0, "", false
		}
		if strings.TrimSpace(req.ResultHash) == "" {
			m.rejectedSubmits++
			return false, "result_hash_required_for_found", 0, "", false
		}
		if !validFoundNonceV1(req.FoundNonce, m.targetMod) {
			m.rejectedSubmits++
			return false, "found_nonce_invalid_for_target_mod", 0, "", false
		}
	}
	delete(m.active, k)
	m.submittedItems++
	if signerAddr != "" {
		nonceKey := signerAddr + ":" + strconv.FormatUint(req.SubmitNonce, 10)
		if len(m.acceptedSubmitNonces) >= m.maxDedupEntries {
			for k := range m.acceptedSubmitNonces {
				delete(m.acceptedSubmitNonces, k)
				break
			}
		}
		m.acceptedSubmitNonces[nonceKey] = struct{}{}
		m.signedSubmitNonceMax[signerAddr] = req.SubmitNonce
		sig, _ := hex.DecodeString(strings.TrimSpace(req.MinerSig))
		canon := canonicalSubmitBytes(req)
		sum := sha256.Sum256(append(canon, sig...))
		sigPayload := hex.EncodeToString(sum[:])
		if len(m.acceptedSignedPayloads) >= m.maxDedupEntries {
			for k := range m.acceptedSignedPayloads {
				delete(m.acceptedSignedPayloads, k)
				break
			}
		}
		m.acceptedSignedPayloads[sigPayload] = struct{}{}
		m.signedAccepts++
		m.lastSignedMiner = signerAddr
	}
	if req.Found {
		rh := strings.TrimSpace(strings.ToLower(req.ResultHash))
		if rh != "" {
			if _, ok := m.acceptedResultHashes[rh]; ok {
				m.dedupSubmits++
				return false, "duplicate_result_hash", 0, "", false
			}
			if len(m.acceptedResultHashes) >= m.maxDedupEntries {
				for k := range m.acceptedResultHashes {
					delete(m.acceptedResultHashes, k)
					break
				}
			}
			m.acceptedResultHashes[rh] = struct{}{}
		}
		if len(m.acceptedFoundNonces) >= m.maxDedupEntries {
			for k := range m.acceptedFoundNonces {
				delete(m.acceptedFoundNonces, k)
				break
			}
		}
		m.acceptedFoundNonces[req.FoundNonce] = struct{}{}
		m.foundHits++
	}
	attempts := req.Attempts
	if attempts == 0 {
		attempts = req.BatchSize
	}
	if attempts > req.BatchSize {
		attempts = req.BatchSize
	}
	m.totalAttempts += attempts
	paidAttempts := attempts
	if m.payoutFoundOnly && !req.Found {
		paidAttempts = 0
	}
	payout := (float64(paidAttempts) / 1_000_000.0) * m.rewardPerM
	if req.Found {
		payout += m.foundBonus
	}
	if payout < 0 {
		payout = 0
	}
	st := m.worker[req.WorkerID]
	st.AcceptedRanges++
	st.AcceptedAtt += attempts
	if req.Found {
		st.AcceptedHits++
	}
	if signerAddr != "" {
		st.PayoutAddress = signerAddr
		st.SignedSubmits++
	}
	st.PayoutHMC += payout
	m.worker[req.WorkerID] = st
	m.totalPayoutHMC += payout
	return req.Found, "", payout, signerAddr, signerAddr != ""
}

func (m *workManager) probeOrdersActive() bool {
	if !m.ordersPriority || strings.TrimSpace(m.ordersProbeURL) == "" {
		return false
	}
	cl := &http.Client{Timeout: 1200 * time.Millisecond}
	resp, err := cl.Get(m.ordersProbeURL + "/api/tasks")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return false
	}
	tasks, _ := body["tasks"].([]any)
	for _, item := range tasks {
		row, _ := item.(map[string]any)
		if strings.EqualFold(strings.TrimSpace(fmt.Sprintf("%v", row["status"])), "open") {
			return true
		}
	}
	return false
}

func (m *workManager) schedulerModeNow() string {
	if !m.ordersPriority {
		return "baseline"
	}
	now := time.Now().Unix()
	m.schedulerMu.Lock()
	needProbe := !m.probeInFlight && ((now-m.lastOrdersProbeUnix >= m.ordersProbeEverySec) || m.lastOrdersProbeUnix == 0)
	last := m.schedulerMode
	if needProbe {
		m.probeInFlight = true
	}
	m.schedulerMu.Unlock()
	if needProbe {
		active := m.probeOrdersActive()
		mode := "baseline"
		if active {
			mode = "orders"
		}
		m.schedulerMu.Lock()
		m.probeInFlight = false
		m.lastOrdersProbeUnix = now
		m.lastOrdersActive = active
		if m.schedulerMode != mode {
			m.schedulerMode = mode
			m.schedulerTransitions++
		}
		last = m.schedulerMode
		m.schedulerMu.Unlock()
	}
	if last == "" {
		return "baseline"
	}
	return last
}

func (m *workManager) pruneAbuseStateLocked(now int64) {
	if now-m.lastAbusePruneUnix < 60 {
		return
	}
	m.lastAbusePruneUnix = now
	cutoffMinute := (now / 60) - 10
	for k, s := range m.abuse {
		if s.BannedUntil > now {
			continue
		}
		if s.BadStrikes > 0 {
			continue
		}
		if s.MinuteUnix <= cutoffMinute {
			delete(m.abuse, k)
		}
	}
	for k, s := range m.ipAbuse {
		if s.BannedUntil > now {
			continue
		}
		if s.BadStrikes > 0 {
			continue
		}
		if s.MinuteUnix <= cutoffMinute {
			delete(m.ipAbuse, k)
		}
	}
}

// enrichPoolStatsForPublic adds hashrate/worker fields expected by pool listing sites (e.g. MiningPoolStats API poll).
func enrichPoolStatsForPublic(out map[string]any, reg *lanpool.Registry) {
	if out == nil || reg == nil {
		return
	}
	online := reg.ListOnline()
	var poolGH float64
	for _, m := range online {
		poolGH += m.HashrateGHS
	}
	out["pool_hashrate_gh_s"] = poolGH
	out["hashrate"] = poolGH * 1e9
	out["hashrate_hs"] = poolGH * 1e9
	out["workers"] = len(online)
	out["workers_online"] = len(online)
}

func (m *workManager) stats(includeDetails bool) map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().Unix()
	drops := make(map[string]uint64, len(m.dropReasonCount))
	for k, v := range m.dropReasonCount {
		drops[k] = v
	}
	inFlight := m.ingressInFlight.Load()
	ackSamples := m.ackLatencySamples.Load()
	ackAvg := float64(0)
	if ackSamples > 0 {
		ackAvg = float64(m.ackLatencyMsSum.Load()) / float64(ackSamples)
	}
	m.schedulerMu.Lock()
	mode := m.schedulerMode
	transitions := m.schedulerTransitions
	ordersActive := m.lastOrdersActive
	lastProbe := m.lastOrdersProbeUnix
	m.schedulerMu.Unlock()
	out := map[string]any{
		"default_batch":                m.defaultBatch,
		"target_mod":                   m.targetMod,
		"lease_sec":                    m.leaseSec,
		"reward_per_m":                 m.rewardPerM,
		"reward_auto":                  m.rewardAuto,
		"base_reward_hmc":              m.baseRewardHMC,
		"found_bonus_hmc":              m.foundBonus,
		"payout_found_only":            m.payoutFoundOnly,
		"now_unix":                     now,
		"next_nonce":                   m.nextNonce.Load(),
		"issued_ranges":                m.issuedRanges,
		"reissued_ranges":              m.reissuedRanges,
		"submitted_items":              m.submittedItems,
		"found_hits":                   m.foundHits,
		"expired_leases":               m.expiredLeases,
		"unknown_submits":              m.unknownSubmits,
		"stale_submits":                m.staleSubmits,
		"rejected_submits":             m.rejectedSubmits,
		"dedup_submits":                m.dedupSubmits,
		"dedup_found_nonce":            m.dedupFoundNonce,
		"accepted_attempts":            m.totalAttempts,
		"total_payout_hmc":             m.totalPayoutHMC,
		"claim_per_min":                m.claimPerMin,
		"submit_per_min":               m.submitPerMin,
		"ban_sec":                      m.banSec,
		"bad_strikes_to_ban":           m.badStrikesToBan,
		"workers_count":                len(m.worker),
		"active_leases_count":          len(m.active),
		"abuse_count":                  len(m.abuse),
		"max_workers":                  m.maxWorkers,
		"max_active_leases":            m.maxActiveLeases,
		"max_active_leases_per_worker": m.maxActiveLeasesPerWorker,
		"max_dedup_entries":            m.maxDedupEntries,
		"ingress_q_len":                inFlight,
		"drop_reason_count":            drops,
		"ack_latency_ms":               ackAvg,
		"scheduler_mode":               mode,
		"scheduler_transitions":        transitions,
		"orders_active":                ordersActive,
		"orders_probe_unix":            lastProbe,
		"hybrid_signer_enabled":        m.hybridSignerEnabled,
		"hybrid_signer_strict":         m.hybridSignerStrict,
		"hybrid_require_found_sig":     m.hybridRequireFoundSig,
		"signed_submits_accepted":      m.signedAccepts,
		"signed_submits_rejected":      m.signedRejects,
		"last_signed_miner_address":    m.lastSignedMiner,
	}
	if includeDetails {
		active := make([]leaseRecord, 0, len(m.active))
		workers := make(map[string]workerPayoutStat, len(m.worker))
		abuse := make(map[string]workerAbuseState, len(m.abuse))
		for _, rec := range m.active {
			active = append(active, rec)
		}
		for k, v := range m.worker {
			workers[k] = v
		}
		for k, v := range m.abuse {
			abuse[k] = v
		}
		out["active_leases"] = active
		out["workers"] = workers
		out["abuse"] = abuse
	}
	return out
}

func validCoordinatorWorkerID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, c := range id {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_', c == '.':
		default:
			return false
		}
	}
	return true
}

func coordinatorPOSTAuthed(r *http.Request, token string, allowInsecure bool) bool {
	if token != "" {
		return coordAdminOK(r, token)
	}
	return allowInsecure
}

func addWorkRoutes(mux *http.ServeMux, token string, allowInsecure bool, reg *lanpool.Registry, wm *workManager) {
	mux.HandleFunc("/api/work/claim", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !coordinatorPOSTAuthed(r, token, allowInsecure) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
			http.Error(w, "admin authentication required", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCoordinatorJSONBodyBytes)
		var req claimWorkRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		workerID := strings.TrimSpace(req.WorkerID)
		if !validCoordinatorWorkerID(workerID) {
			http.Error(w, "invalid worker_id", http.StatusBadRequest)
			return
		}
		ipKey := keyFromRemoteAddr(r.RemoteAddr)
		if ok, reason := wm.allowClaim(workerID, ipKey, time.Now().Unix()); !ok {
			wm.recordDrop(reason)
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":        false,
				"worker_id": workerID,
				"reason":    reason,
			})
			return
		}
		base, size, leaseUntil, mod, reused, okClaim, reason := wm.claim(workerID, req.BatchSize)
		if !okClaim {
			wm.recordDrop(reason)
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":        false,
				"worker_id": workerID,
				"reason":    reason,
			})
			return
		}
		mode := wm.schedulerModeNow()
		taskClass := "baseline"
		if mode == "orders" {
			taskClass = "orders"
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":               true,
			"worker_id":        workerID,
			"base_nonce":       base,
			"batch_size":       size,
			"target_mod":       mod,
			"lease_expires_at": leaseUntil,
			"reissued":         reused,
			"work_id":          buildWorkID(workerID, base, size),
			"chunk_id":         buildChunkID(base, size, mod),
			"scheduler_mode":   mode,
			"task_class":       taskClass,
		})
	})

	mux.HandleFunc("/api/work/submit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !coordinatorPOSTAuthed(r, token, allowInsecure) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
			http.Error(w, "admin authentication required", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCoordinatorJSONBodyBytes)
		var req submitWorkRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		workerID := strings.TrimSpace(req.WorkerID)
		if !validCoordinatorWorkerID(workerID) {
			http.Error(w, "invalid worker_id", http.StatusBadRequest)
			return
		}
		if req.BatchSize == 0 {
			http.Error(w, "batch_size required", http.StatusBadRequest)
			return
		}
		now := time.Now().Unix()
		ipKey := keyFromRemoteAddr(r.RemoteAddr)
		if ok, reason := wm.allowSubmit(workerID, ipKey, now); !ok {
			wm.recordDrop(reason)
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":        false,
				"worker_id": workerID,
				"reason":    reason,
			})
			return
		}
		req.WorkerID = workerID
		start := time.Now()
		wm.ingressInFlight.Add(1)
		accepted, reason, payout, signerAddr, signedSubmit := wm.submit(req)
		wm.ingressInFlight.Add(-1)
		latMs := uint64(time.Since(start).Milliseconds())
		wm.ackLatencyMsSum.Add(latMs)
		wm.ackLatencySamples.Add(1)
		wm.markSubmitOutcome(workerID, reason, now)
		if reason != "" {
			wm.recordDrop(reason)
		}
		_ = reg.Upsert(r.RemoteAddr, lanpool.PushWorkBody{
			WorkerID:      workerID,
			HashrateGHS:   req.HashrateGHS,
			ShareAccepted: &accepted,
		})
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		okResp := true
		if reason != "" {
			w.WriteHeader(http.StatusConflict)
			okResp = false
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":             okResp,
			"worker_id":      workerID,
			"accepted":       accepted,
			"reason":         reason,
			"payout_hmc":     payout,
			"signed_submit":  signedSubmit,
			"signer_address": signerAddr,
		})
	})

	mux.HandleFunc("/api/work/stats", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		includeDetails := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("details")))
		details := includeDetails == "1" || includeDetails == "true" || includeDetails == "yes"
		if details {
			if token != "" && !coordAdminOK(r, token) {
				w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
				http.Error(w, "admin authentication required", http.StatusUnauthorized)
				return
			}
			if token == "" && !allowInsecure {
				details = false
			}
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		out := wm.stats(details)
		enrichPoolStatsForPublic(out, reg)
		_ = json.NewEncoder(w).Encode(out)
	})

	// Minimal stats shape for pool listing sites (MiningPoolStats API poll).
	mux.HandleFunc("/api/pool/stats", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		out := wm.stats(false)
		enrichPoolStatsForPublic(out, reg)
		hr := float64(0)
		if v, ok := out["hashrate_hs"].(float64); ok {
			hr = v
		}
		if hr <= 0 {
			if gh, ok := out["pool_hashrate_gh_s"].(float64); ok {
				hr = gh * 1e9
			}
		}
		wc := 0
		switch v := out["workers"].(type) {
		case int:
			wc = v
		case float64:
			wc = int(v)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":   "ok",
			"pool":     "HackMe Official Pool",
			"hashrate": hr,
			"workers":  wc,
			"miners":   wc,
		})
	})
}
