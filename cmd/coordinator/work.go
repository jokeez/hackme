package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"hackme/internal/chain"
	"hackme/internal/lanpool"
	"hackme/internal/logsafe"
	"hackme/internal/poolauth"
	"hackme/internal/worksubmit"
)

const maxCoordinatorJSONBodyBytes = 2 << 20 // 2 MiB (large wasm hex + corpus seeds on claim)

type workManager struct {
	nextNonce atomic.Uint64

	// payoutLockMu serializes durable payout-lock writes (persist vs clear) so
	// an in-flight claim cannot re-insert a binding an admin unbind just deleted.
	// Order: payoutLockMu -> m.mu; never the reverse.
	payoutLockMu sync.Mutex

	defaultBatch    uint64
	maxClaimBatch   uint64 // hard cap on claim/submit batch_size (anti inflation)
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
	dedupDB                *sql.DB // optional durable dedup (issue #8 Phase 3)

	issuedRanges     uint64
	reissuedRanges   uint64
	submittedItems   uint64
	foundHits        uint64
	lastFoundHitUnix int64
	poolGHSmoothed   float64 // EMA of fleet GH/s for load retarget (avoids 0.15 GH/s pin → M runaway)
	poolRetarget     bool
	// Pool M bounds (defaults poolTargetModMin/Max; override via HACKME_COORDINATOR_POOL_TARGET_MOD_{MIN,MAX}).
	targetModMin          uint64
	targetModMax          uint64
	targetModUpdatedUnix  int64
	poolRetargetMinSec    int64
	lastPoolRetargetUnix  int64
	expiredLeases         uint64
	unknownSubmits        uint64
	staleSubmits          uint64
	rejectedSubmits       uint64
	totalAttempts         uint64
	totalPayoutHMC        float64
	totalPayoutSUP        float64
	supPolicy             supPolicy
	supMeta               map[string]workerSupMeta
	supDayID              int64
	supAccruedDay         float64
	hmcAccruedDay         float64
	dedupSubmits          uint64
	dedupFoundNonce       uint64
	fleetAddressConflicts uint64
	signedAccepts         uint64
	signedRejects         uint64

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
	chainPoHMod          uint64
	activeOrder          activeOrderSnap

	lastAbusePruneUnix    int64
	lastIdlePruneUnix     int64
	hybridSignerEnabled   bool
	hybridSignerStrict    bool
	hybridRequireFoundSig bool
	claimRequirePubKey    bool

	// Short TTL for /api/work/stats|/api/pool/stats — public pollers were rebuilding the full worker map every hit.
	statsCacheMu      sync.Mutex
	statsCachePlainAt time.Time
	statsCachePlain   map[string]any
	statsCacheDetAt   time.Time
	statsCacheDetails map[string]any
}

type workKey struct {
	base  uint64
	batch uint64
}

const (
	poolTargetModDefault = 5_000_000
	poolTargetModMin     = 2_000_000
	// Default cap 1e9 — headroom for large fleets (100k+ workers) without UI stuck at max.
	// Override via HACKME_COORDINATOR_POOL_TARGET_MOD_MAX (hard limit 2e9 in env parser).
	poolTargetModMax = 1_000_000_000
)

type leaseRecord struct {
	WorkerID  string `json:"worker_id"`
	BaseNonce uint64 `json:"base_nonce"`
	BatchSize uint64 `json:"batch_size"`
	IssuedAt  int64  `json:"issued_at"`
	ExpiresAt int64  `json:"expires_at"`
	Reissues  uint64 `json:"reissues"`
	TargetMod uint64 `json:"target_mod"`
}

type workerPayoutStat struct {
	AcceptedRanges  uint64  `json:"accepted_ranges"`
	AcceptedHits    uint64  `json:"accepted_hits"`
	AcceptedAtt     uint64  `json:"accepted_attempts"`
	PayoutHMC       float64 `json:"payout_hmc"`
	PayoutSUP       float64 `json:"payout_sup"`
	PayoutAddress   string  `json:"payout_address,omitempty"`
	AddressConflict bool    `json:"address_conflict,omitempty"`
	SignedSubmits   uint64  `json:"signed_submits,omitempty"`
	LastHashrateGHS float64 `json:"hashrate_gh_s,omitempty"`
	PeakHashrateGHS float64 `json:"peak_hashrate_gh_s,omitempty"`
	LastSeenUnix    int64   `json:"last_seen_unix,omitempty"`
	// LastPoHSeenUnix is refreshed only on accepted PoH submits with credible GH/s.
	// Fuzz heartbeats must not keep hybrid GHS "live" after mining stops.
	LastPoHSeenUnix int64 `json:"last_poh_seen_unix,omitempty"`
	// LastFuzzSeenUnix is refreshed on fuzz claim/submit. Required (with PoH) to count
	// as hybrid Dig/Hunt capacity — PoH-only miners must not throttle dig-only fleets.
	LastFuzzSeenUnix int64 `json:"last_fuzz_seen_unix,omitempty"`
	// LockSeenUnix throttles the durable payout-lock seen_at refresh (at most
	// once per hour per worker) so active bindings survive idle-GC without a
	// DB write on every lock read.
	LockSeenUnix int64  `json:"-"`
	LastClientIP string `json:"last_client_ip,omitempty"`
	Online       bool   `json:"online,omitempty"`
}

type workerAbuseState struct {
	MinuteUnix  int64 `json:"minute_unix"`
	ClaimCount  int   `json:"claim_count"`
	SubmitCount int   `json:"submit_count"`
	BadStrikes  int   `json:"bad_strikes"`
	BannedUntil int64 `json:"banned_until,omitempty"`
}

type claimWorkRequest struct {
	WorkerID     string `json:"worker_id"`
	BatchSize    uint64 `json:"batch_size,omitempty"`
	MinerPubKey  string `json:"miner_pubkey_ed25519,omitempty"`
	MinerAddress string `json:"miner_address,omitempty"`
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
	OrderTaskID  string  `json:"order_task_id,omitempty"`
	WasmGatePass bool    `json:"wasm_gate_pass,omitempty"`
}

func newWorkManagerFromEnv() *workManager {
	batch := uint64(1 << 22)
	if v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_WORK_BATCH")); v != "" {
		if x, err := strconv.ParseUint(v, 10, 64); err == nil && x >= 1000 {
			batch = x
		}
	}
	maxClaim := batch * 4
	if maxClaim < batch {
		maxClaim = batch
	}
	if v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_MAX_CLAIM_BATCH")); v != "" {
		if x, err := strconv.ParseUint(v, 10, 64); err == nil && x >= 1000 {
			maxClaim = x
		}
	}
	mod := uint64(5_000_000)
	modFromEnv := false
	if v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_TARGET_MOD")); v != "" {
		if x, err := strconv.ParseUint(v, 10, 64); err == nil && x > 0 {
			mod = x
			modFromEnv = true
		}
	}
	if !modFromEnv {
		if persisted := loadPersistedPoolTargetMod(); persisted > 0 {
			mod = persisted
		}
	}
	minM := uint64(poolTargetModMin)
	if v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_POOL_TARGET_MOD_MIN")); v != "" {
		if x, err := strconv.ParseUint(v, 10, 64); err == nil && x >= 500_000 && x <= 100_000_000 {
			minM = x
		}
	}
	maxM := uint64(poolTargetModMax)
	if v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_POOL_TARGET_MOD_MAX")); v != "" {
		if x, err := strconv.ParseUint(v, 10, 64); err == nil && x >= minM && x <= 2_000_000_000 {
			maxM = x
		}
	}
	if minM > maxM {
		minM, maxM = uint64(poolTargetModMin), uint64(poolTargetModMax)
	}
	if mod < minM {
		mod = minM
	}
	if mod > maxM {
		mod = maxM
	}
	// Solvability for PohEval=7n+13: never start (or reload) on a multiple of 7.
	mod = sanitizePoolTargetMod7n13(mod, minM, maxM)
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
	hybridSignerEnabled := poolauth.HybridSignerEnabled()
	hybridSignerStrict := hybridSignerEnabled
	if v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_POOL_HYBRID_SIGNER_STRICT"))); v != "" {
		hybridSignerStrict = v == "1" || v == "true" || v == "yes" || v == "on"
	}
	hybridRequireFoundSig := true
	if v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_POOL_HYBRID_REQUIRE_FOUND_SIG"))); v != "" {
		hybridRequireFoundSig = v == "1" || v == "true" || v == "yes" || v == "on"
	}
	// Claim pubkey: default ON when hybrid signing is enabled (closes identity-free
	// claim-as-victim abuse). Explicit HACKME_POOL_CLAIM_REQUIRE_PUBKEY=0 keeps legacy off.
	claimRequirePubKey := hybridSignerEnabled
	if v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_POOL_CLAIM_REQUIRE_PUBKEY"))); v != "" {
		claimRequirePubKey = v == "1" || v == "true" || v == "yes" || v == "on"
	}
	poolRetarget := true
	if v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_COORDINATOR_POOL_RETARGET"))); v != "" {
		poolRetarget = v == "1" || v == "true" || v == "yes" || v == "on"
	}
	poolRetargetMinSec := int64(15)
	if v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_POOL_RETARGET_MIN_SEC")); v != "" {
		if x, err := strconv.ParseInt(v, 10, 64); err == nil && x >= 3 {
			poolRetargetMinSec = x
		}
	}
	wm := &workManager{
		defaultBatch:             batch,
		maxClaimBatch:            maxClaim,
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
		claimRequirePubKey:       claimRequirePubKey,
		poolRetarget:             poolRetarget,
		targetModMin:             minM,
		targetModMax:             maxM,
		targetModUpdatedUnix:     time.Now().Unix(),
		poolRetargetMinSec:       poolRetargetMinSec,
		supPolicy:                supPolicyFromEnv(),
		supMeta:                  make(map[string]workerSupMeta),
	}
	persistPoolTargetMod(mod)
	return wm
}

func (m *workManager) targetModMinOrDefault() uint64 {
	if m == nil || m.targetModMin == 0 {
		return poolTargetModMin
	}
	return m.targetModMin
}

func poolTargetModPersistPath() string {
	if v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_TARGET_MOD_FILE")); v != "" {
		return v
	}
	dbPath := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_DB"))
	if dbPath == "" {
		dbPath = "data/coordinator.db"
	}
	return filepath.Join(filepath.Dir(dbPath), "coordinator_target_mod.txt")
}

func loadPersistedPoolTargetMod() uint64 {
	b, err := os.ReadFile(poolTargetModPersistPath())
	if err != nil {
		return 0
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return 0
	}
	x, err := strconv.ParseUint(v, 10, 64)
	if err != nil || x == 0 {
		return 0
	}
	// Mirror chain: bad stored M must not block mining after restart (report #29).
	return sanitizePoolTargetMod7n13(x, poolTargetModMin, poolTargetModMax)
}

func persistPoolTargetMod(mod uint64) {
	if mod == 0 {
		return
	}
	mod = sanitizePoolTargetMod7n13(mod, poolTargetModMin, poolTargetModMax)
	path := poolTargetModPersistPath()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte(strconv.FormatUint(mod, 10)+"\n"), 0o644)
}

func (m *workManager) targetModMaxOrDefault() uint64 {
	if m == nil || m.targetModMax == 0 {
		return poolTargetModMax
	}
	return m.targetModMax
}

// poolMinerCountBoost scales difficulty slightly with distinct online workers (log curve, capped).
// Dominated by fleet GH/s; this nudges M up when many small rigs join (100k miners scenario).
const poolGHEMAAlpha = 0.3

// poolStallEaseFoundGapSec is how long without an accepted found before the pool
// counts as stalled for M easing. Conservative: far above a healthy sparse-found
// cadence, and the load retarget pushes M back up on its next tick regardless.
const poolStallEaseFoundGapSec int64 = 3600

// smoothPoolGHSample updates fleet GH/s EMA; fast-tracks when measured hash jumps (GPU came online).
func (m *workManager) smoothPoolGHSample(raw float64) float64 {
	if m == nil || raw <= 0 || math.IsNaN(raw) || math.IsInf(raw, 0) {
		return 0
	}
	if raw > 5000 {
		raw = 5000
	}
	if m.poolGHSmoothed <= 0 {
		m.poolGHSmoothed = raw
		return m.poolGHSmoothed
	}
	alpha := poolGHEMAAlpha
	if raw > m.poolGHSmoothed*1.8 {
		alpha = 0.55
	} else if raw < m.poolGHSmoothed*0.35 {
		alpha = 0.15
	}
	m.poolGHSmoothed = alpha*raw + (1-alpha)*m.poolGHSmoothed
	return m.poolGHSmoothed
}

func smoothWorkerHashrateGHS(prev, sample float64) float64 {
	sample = clampWorkerHashrateGHS(sample)
	if sample <= 0 {
		return clampWorkerHashrateGHS(prev)
	}
	if prev <= 0 {
		return sample
	}
	alpha := 0.35
	if sample > prev*1.8 {
		alpha = 0.55
	}
	// Fast recovery when EMA was stuck low but GPU reports high throughput again.
	if sample > 5 && prev < sample*0.15 {
		alpha = 0.82
	}
	return clampWorkerHashrateGHS(alpha*sample + (1-alpha)*prev)
}

// effectiveWorkerHashrateGHS uses peak when rolling EMA collapsed far below observed GPU peak.
func effectiveWorkerHashrateGHS(st workerPayoutStat) float64 {
	gh := clampWorkerHashrateGHS(st.LastHashrateGHS)
	peak := clampWorkerHashrateGHS(st.PeakHashrateGHS)
	if peak > 1 && gh < peak*0.25 {
		return peak * 0.72
	}
	return gh
}

func poolMinerCountBoost(miners int) float64 {
	if miners <= 1 {
		return 1.0
	}
	boost := 1.0 + math.Log10(float64(miners))*0.05
	if boost > 1.35 {
		return 1.35
	}
	return boost
}

// poolLoadHintUnclamped is the fleet-hash + miner-count difficulty the coordinator *would* use
// before applying targetModMax (for honest UI / pool listings).
func poolLoadHintUnclamped(poolGH float64, miners int, modMin uint64) uint64 {
	if poolGH < 0.01 {
		return 0
	}
	if modMin == 0 {
		modMin = poolTargetModMin
	}
	modPerGH := float64(modMin)
	loadM := poolGH * modPerGH
	loadM *= poolMinerCountBoost(miners)
	if loadM >= float64(^uint64(0)>>1) {
		return ^uint64(0) >> 1
	}
	return uint64(loadM + 0.5)
}

func (m *workManager) clampTargetMod(u uint64) uint64 {
	minM := m.targetModMinOrDefault()
	maxM := m.targetModMaxOrDefault()
	if minM > maxM {
		minM, maxM = uint64(poolTargetModMin), uint64(poolTargetModMax)
	}
	if u < minM {
		u = minM
	}
	if u > maxM {
		u = maxM
	}
	// Same solvability rule as chain.ClampPoHTargetMod / sanitizePoHTargetMod7n13:
	// PohEval(n)=7n+13 has no solution when 7|M (report #29).
	return sanitizePoolTargetMod7n13(u, minM, maxM)
}

// sanitizePoolTargetMod7n13 nudges M off multiples of 7 while staying in [minM, maxM].
func sanitizePoolTargetMod7n13(m, minM, maxM uint64) uint64 {
	if minM > maxM {
		minM, maxM = maxM, minM
	}
	if m < minM {
		m = minM
	}
	if m > maxM {
		m = maxM
	}
	if m%7 != 0 {
		return m
	}
	if m < maxM {
		return m + 1
	}
	if m > minM {
		return m - 1
	}
	return m
}

func validFoundNonceV1(foundNonce, targetMod uint64) bool {
	if targetMod == 0 {
		return false
	}
	// Coordinator baseline: v1 PoH eval (7n+13) divisibility check.
	// This makes payout depend on actually finding a valid hit.
	return chain.PohEval(foundNonce)%targetMod == 0
}

// fetchChainMetricsFromNode loads mining_target_mod from the node metrics URL without holding workManager.mu.
func fetchChainMetricsFromNode(targetURL string) (parsed uint64, baseRewardHMC float64, hasMod, hasBase bool) {
	targetURL = strings.TrimRight(strings.TrimSpace(targetURL), "/")
	if targetURL == "" {
		return 0, 0, false, false
	}
	cl := &http.Client{Timeout: 800 * time.Millisecond}
	resp, err := cl.Get(targetURL + "/api/metrics")
	if err != nil {
		return 0, 0, false, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, 0, false, false
	}
	var body map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return 0, 0, false, false
	}
	if v, okField := body["mining_target_mod"]; okField {
		switch t := v.(type) {
		case float64:
			if t > 0 {
				parsed = uint64(t)
				hasMod = true
			}
		case json.Number:
			if x, err := t.Int64(); err == nil && x > 0 {
				parsed = uint64(x)
				hasMod = true
			}
		}
	}
	if br, ok := body["econ_base_reward_now_hmc"]; ok {
		hasBase = true
		switch t := br.(type) {
		case float64:
			if t >= 0 {
				baseRewardHMC = t
			}
		case json.Number:
			if x, err := t.Float64(); err == nil && x >= 0 {
				baseRewardHMC = x
			}
		}
	}
	return parsed, baseRewardHMC, hasMod, hasBase
}

// prefetchTargetMod refreshes chain target from the node metrics URL. Call without holding m.mu.
func (m *workManager) prefetchTargetMod(now int64) {
	if strings.TrimSpace(m.targetURL) == "" {
		return
	}
	m.mu.Lock()
	if now-m.targetLastAt < m.targetEvery {
		m.mu.Unlock()
		return
	}
	targetURL := m.targetURL
	rewardAuto := m.rewardAuto
	m.mu.Unlock()

	parsed, baseReward, hasMod, hasBase := fetchChainMetricsFromNode(targetURL)

	m.mu.Lock()
	defer m.mu.Unlock()
	if now-m.targetLastAt < m.targetEvery && !hasMod && !hasBase {
		return
	}
	if hasMod {
		m.storeChainPoHMod(parsed)
		m.applyChainTargetMod(parsed, now)
	}
	if hasBase {
		m.baseRewardHMC = baseReward
	}
	if rewardAuto && m.targetMod > 0 && m.baseRewardHMC > 0 {
		m.rewardPerM = (m.baseRewardHMC * 1_000_000.0) / float64(m.targetMod)
		if m.rewardPerM < 0 {
			m.rewardPerM = 0
		}
	}
	m.targetLastAt = now
}

func (m *workManager) applyChainTargetMod(parsed uint64, now int64) {
	if parsed == 0 {
		return
	}
	// Pool auto-retarget owns M once seeded; chain solo M (251) must not reset it.
	if m.poolRetarget && m.targetMod > 0 {
		return
	}
	if !m.poolRetarget {
		m.targetMod = m.clampTargetMod(parsed)
		m.targetModUpdatedUnix = now
		return
	}
	if m.targetMod < m.targetModMinOrDefault() {
		m.targetMod = m.clampTargetMod(parsed)
		m.targetModUpdatedUnix = now
	}
}

func clampWorkerHashrateGHS(ghs float64) float64 {
	if ghs <= 0 || math.IsNaN(ghs) || math.IsInf(ghs, 0) {
		return 0
	}
	const maxGH = 128
	if ghs > maxGH {
		return maxGH
	}
	return ghs
}

// workerHashrateGHSForSubmit prefers reported GH/s when consistent with lease wall time.
func workerHashrateGHSForSubmit(reported float64, batch uint64, issuedAt, now int64, last float64) float64 {
	wallGH := 0.0
	wall := 0.0
	if issuedAt > 0 && batch > 0 {
		wall = float64(now - issuedAt)
		if wall < 0.05 {
			wall = 0.05
		}
		wallGH = clampWorkerHashrateGHS(float64(batch) / wall / 1e9)
	}
	rep := clampWorkerHashrateGHS(reported)
	if rep > 0 {
		// Fast GPU cycles (claim→submit <1s) legitimately report calibration GH/s above batch/wall.
		if wallGH > 0 && wall >= 1.0 && rep > wallGH*2.5 {
			return wallGH
		}
		if wallGH > 0 && wall < 1.0 && rep > wallGH {
			return rep
		}
		return rep
	}
	if wallGH > 0 {
		return wallGH
	}
	return clampWorkerHashrateGHS(last)
}

func (m *workManager) maybeRetargetPoolMod(now int64) {
	if !m.poolRetarget {
		return
	}
	if m.lastFoundHitUnix > 0 {
		if m.lastPoolRetargetUnix > 0 && now-m.lastPoolRetargetUnix < m.poolRetargetMinSec {
			m.lastFoundHitUnix = now
			return
		}
		delta := now - m.lastFoundHitUnix
		if delta < 1 {
			delta = 1
		}
		prev := m.clampTargetMod(m.targetMod)
		var next uint64
		// Fast solves → harder (larger M). Slow gaps → slightly easier, never below pool floor.
		poolGH := m.poolGHSmoothed
		if poolGH <= 0 {
			poolGH, _, _ = m.poolOnlineSummaryUnlocked(120, now)
			m.smoothPoolGHSample(poolGH)
			poolGH = m.poolGHSmoothed
		}
		switch {
		case delta <= chain.PoHRetargetTargetSec*2:
			ratio := float64(chain.PoHRetargetTargetSec) / float64(delta)
			if ratio > 1.12 {
				ratio = 1.12
			}
			// Under-reported fleet GH/s (e.g. stale 0.15 GH/s calib) + fast hits must not explode M.
			if poolGH > 0 && poolGH < 1.0 && ratio > 1.0 {
				ratio = 1.01
			}
			next = uint64(float64(prev)*ratio + 0.5)
		case delta >= chain.PoHRetargetTargetSec*6:
			ratio := float64(chain.PoHRetargetTargetSec) / float64(delta)
			if ratio < 0.88 {
				ratio = 0.88
			}
			next = uint64(float64(prev)*ratio + 0.5)
		default:
			m.lastFoundHitUnix = now
			return
		}
		next = m.clampTargetMod(next)
		if next != m.targetMod {
			m.targetMod = next
			m.targetModUpdatedUnix = now
			m.lastPoolRetargetUnix = now
			persistPoolTargetMod(next)
			if m.rewardAuto && m.baseRewardHMC > 0 {
				m.rewardPerM = (m.baseRewardHMC * 1_000_000.0) / float64(m.targetMod)
				if m.rewardPerM < 0 {
					m.rewardPerM = 0
				}
			}
		}
	}
	m.lastFoundHitUnix = now
	if m.lastPoolRetargetUnix == 0 {
		m.lastPoolRetargetUnix = now
	}
}

// maybeEasePoolModOnStallLocked eases M when no accepted found has landed for a long stretch.
// Unlike maybeRetargetPoolMod (found-triggered), this does not require / invent lastFoundHitUnix
// — covers the report #29 freeze where find-gap easing was unreachable.
// A pool that found earlier and then went silent counts as stalled too: gating on
// lastFoundHitUnix == 0 only armed this path while no found had EVER landed since
// boot, which is not the realistic freeze shape (a running pool whose finds stop
// later, e.g. a future eval residue class turning the gate unsatisfiable).
// Caller must hold m.mu.
func (m *workManager) maybeEasePoolModOnStallLocked(now int64) {
	if !m.poolRetarget {
		return
	}
	// A recent accepted found means the gate is solvable — not stalled.
	if m.lastFoundHitUnix > 0 && now-m.lastFoundHitUnix < poolStallEaseFoundGapSec {
		return
	}
	anchor := m.targetModUpdatedUnix
	if anchor == 0 {
		anchor = m.lastPoolRetargetUnix
	}
	if anchor == 0 {
		m.targetModUpdatedUnix = now
		m.lastPoolRetargetUnix = now
		return
	}
	if now-anchor < chain.PoHRetargetTargetSec*6 {
		return
	}
	if m.lastPoolRetargetUnix > 0 && now-m.lastPoolRetargetUnix < m.poolRetargetMinSec {
		return
	}
	prev := m.clampTargetMod(m.targetMod)
	next := m.clampTargetMod(uint64(float64(prev)*0.88 + 0.5))
	if next == m.targetMod {
		m.targetModUpdatedUnix = now
		m.lastPoolRetargetUnix = now
		return
	}
	m.targetMod = next
	m.targetModUpdatedUnix = now
	m.lastPoolRetargetUnix = now
	persistPoolTargetMod(next)
	if m.rewardAuto && m.baseRewardHMC > 0 {
		m.rewardPerM = (m.baseRewardHMC * 1_000_000.0) / float64(m.targetMod)
		if m.rewardPerM < 0 {
			m.rewardPerM = 0
		}
	}
}

// maybeRetargetPoolLoad nudges M from fleet hashrate + miner count (more miners/hash → higher M).
func (m *workManager) maybeRetargetPoolLoad(now int64, poolGH float64, miners int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maybeRetargetPoolLoadLocked(now, poolGH, miners)
}

// maybeRetargetPoolLoadLocked is maybeRetargetPoolLoad; caller must hold m.mu.
func (m *workManager) maybeRetargetPoolLoadLocked(now int64, poolGH float64, miners int) {
	if !m.poolRetarget || poolGH < 0.01 {
		return
	}
	if m.lastPoolRetargetUnix > 0 && now-m.lastPoolRetargetUnix < m.poolRetargetMinSec {
		return
	}
	smoothed := m.smoothPoolGHSample(poolGH)
	if smoothed > 0 {
		poolGH = smoothed
	}
	// Scale M with fleet GH/s (targetModMin per 1 GH/s) so more hash → higher M, less hash → lower M.
	// Do not use poolGH*1e9*targetSec (that overshoots targetModMax immediately).
	modPerGH := float64(m.targetModMinOrDefault())
	loadM := poolGH * modPerGH
	loadM *= poolMinerCountBoost(miners)
	target := m.clampTargetMod(uint64(loadM + 0.5))
	prev := m.clampTargetMod(m.targetMod)
	if target == prev {
		// Periodic tick even when load-stable: stall easing must not require an accepted found
		// (report #29 — find-gap path used to be unreachable while the gate was unsatisfiable).
		m.maybeEasePoolModOnStallLocked(now)
		return
	}
	var next uint64
	if target > prev {
		// Catch up faster after coordinator restart (M resets to default while load hint is huge).
		ratio := 1.06
		if target > prev*4 {
			ratio = 1.35
		} else if target > prev*2 {
			ratio = 1.18
		}
		next = uint64(float64(prev)*ratio + 0.5)
		if next > target {
			next = target
		}
	} else {
		next = uint64(float64(prev)*0.97 + 0.5)
		if next < target {
			next = target
		}
	}
	next = m.clampTargetMod(next)
	if next == m.targetMod {
		return
	}
	m.targetMod = next
	m.targetModUpdatedUnix = now
	m.lastPoolRetargetUnix = now
	persistPoolTargetMod(next)
	if m.rewardAuto && m.baseRewardHMC > 0 {
		m.rewardPerM = (m.baseRewardHMC * 1_000_000.0) / float64(m.targetMod)
		if m.rewardPerM < 0 {
			m.rewardPerM = 0
		}
	}
}

func (m *workManager) purgeExpiredLeases(now int64) {
	for k, rec := range m.active {
		if rec.ExpiresAt <= now {
			delete(m.active, k)
			m.expiredLeases++
		}
	}
}

func keyFromRemoteAddr(remoteAddr string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err != nil {
		return strings.TrimSpace(remoteAddr)
	}
	return strings.TrimSpace(host)
}

func (m *workManager) noteWorkerClientIP(workerID, ip string) {
	workerID = strings.TrimSpace(workerID)
	ip = strings.TrimSpace(ip)
	if workerID == "" || ip == "" {
		return
	}
	now := time.Now().Unix()
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.worker[workerID]
	if !ok {
		st = workerPayoutStat{}
	}
	st.LastClientIP = ip
	if st.LastSeenUnix == 0 {
		st.LastSeenUnix = now
	}
	m.worker[workerID] = st
}

// touchWorkerSeen refreshes last_seen for fuzz claim/submit heartbeats (PoH submit path
// already updates LastSeenUnix; fuzz workers otherwise go "offline" in the UI after 300s).
// touchWorkerSeen updates LastSeenUnix. Creates a new worker row only when under maxWorkers.
// Returns false with reason "too_many_workers" when a new id would exceed the cap (PoH parity).
func (m *workManager) touchWorkerSeen(workerID string) {
	_, _ = m.touchWorkerSeenLimited(workerID)
}

func (m *workManager) touchWorkerSeenLimited(workerID string) (ok bool, reason string) {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" || m == nil {
		return false, "invalid_worker_id"
	}
	now := time.Now().Unix()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.worker == nil {
		m.worker = make(map[string]workerPayoutStat)
	}
	if _, exists := m.worker[workerID]; !exists && m.maxWorkers > 0 && len(m.worker) >= m.maxWorkers {
		return false, "too_many_workers"
	}
	st := m.worker[workerID]
	st.LastSeenUnix = now
	st.LastFuzzSeenUnix = now
	m.worker[workerID] = st
	return true, ""
}

// Rate-gate invariant (reports #27/#28 + harden pass):
//
//  1. Identity BEFORE charging a forgeable declared worker_id.
//     Claim/release already check pubkey→lock first; submit charges the
//     worker slot only after signature / payout-lock (chargeSubmitWorker).
//  2. Peer/IP slots MAY run pre-verification — the key is the real peer address.
//  3. Failed/no-op paths must not create durable bindings (release → check-only).
//  4. submitPerMin/claimPerMin <= 0 disables the corresponding worker/IP charge
//     (unit harnesses); production always sets positive ceilings.
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

func (m *workManager) workerRateLimitPerMin(workerID string, globalMax int) int {
	if globalMax < 1 {
		globalMax = 1
	}
	gh := float64(0)
	peak := float64(0)
	if st, ok := m.worker[workerID]; ok {
		gh = st.LastHashrateGHS
		peak = st.PeakHashrateGHS
	}
	if peak > gh*1.15 {
		gh = peak * 0.82
	}
	if gh <= 0 {
		if globalMax <= 20 {
			return globalMax
		}
		lim := globalMax / 6
		if lim < 12 {
			lim = 12
		}
		return lim
	}
	// ~15 claim/submit cycles per minute per 1 GH/s (caps CPU submit-spam rigs).
	lim := int(gh * 15)
	if gh < 1 {
		// Sub-1 GH/s CPU rigs: do not apply the 20/min floor (was letting 0.02 GH/s spam ~20 claims/min).
		if lim < 2 {
			lim = 2
		}
	} else if lim < 20 {
		lim = 20
	}
	if lim > globalMax {
		lim = globalMax
	}
	// Public pool runs with high global ceilings (e.g. 6000/min); never throttle a live GPU rig to 2/min.
	if globalMax >= 120 && lim < 120 {
		lim = 120
	}
	return lim
}

func (m *workManager) allowClaim(workerID, ipKey string, now int64) (bool, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneAbuseStateLocked(now)
	if m.isHardPermabannedLocked(workerID, ipKey) {
		return false, "worker_permabanned"
	}
	if s := m.abuse[workerID]; s.BannedUntil > now {
		return false, "worker_temporarily_banned"
	}
	if m.claimPerMin <= 0 {
		return true, ""
	}
	perMin := m.workerRateLimitPerMin(workerID, m.claimPerMin)
	s, ok := m.allowRateSlot(m.abuse[workerID], now, perMin, true)
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
	ip, ok := m.allowRateSlot(m.ipAbuse[ipKey], now, perMin*4, true)
	m.ipAbuse[ipKey] = ip
	if !ok {
		return false, "claim_rate_limited"
	}
	return true, ""
}

// allowClaimPeer gates bans + IP claim bucket without charging the declared
// worker_id. Used on release (and similar) where the id is forgeable until a
// lock exists — charging the nominal worker would let a shared-token peer freeze
// an unlocked victim's claim lane (same class as report #28).
func (m *workManager) allowClaimPeer(workerID, ipKey string, now int64) (bool, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneAbuseStateLocked(now)
	if m.isHardPermabannedLocked(workerID, ipKey) {
		return false, "worker_permabanned"
	}
	if s := m.abuse[workerID]; s.BannedUntil > now {
		return false, "worker_temporarily_banned"
	}
	if m.claimPerMin <= 0 {
		return true, ""
	}
	perMin := m.workerRateLimitPerMin(workerID, m.claimPerMin)
	if ipKey == "" {
		return true, ""
	}
	if ip := m.ipAbuse[ipKey]; ip.BannedUntil > now {
		return false, "worker_temporarily_banned"
	}
	ip, ok := m.allowRateSlot(m.ipAbuse[ipKey], now, perMin*4, true)
	m.ipAbuse[ipKey] = ip
	if !ok {
		return false, "claim_rate_limited"
	}
	return true, ""
}

func (m *workManager) allowSubmit(workerID, ipKey string, now int64) (bool, string) {
	// Legacy combined gate (tests / callers that still want both). Production submit
	// handlers use allowSubmitPeer + chargeSubmitWorker so a forgeable declared
	// worker_id is not charged before signature / payout-lock (report #28).
	if ok, reason := m.allowSubmitPeer(workerID, ipKey, now); !ok {
		return false, reason
	}
	return m.chargeSubmitWorker(workerID, now)
}

// allowSubmitPeer gates bans + the IP submit bucket. The peer address is not
// forgeable, so this may run before signature verification (CPU bound).
func (m *workManager) allowSubmitPeer(workerID, ipKey string, now int64) (bool, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneAbuseStateLocked(now)
	if m.isHardPermabannedLocked(workerID, ipKey) {
		return false, "worker_permabanned"
	}
	if s := m.abuse[workerID]; s.BannedUntil > now {
		return false, "worker_temporarily_banned"
	}
	if m.submitPerMin <= 0 {
		return true, ""
	}
	perMin := m.workerRateLimitPerMin(workerID, m.submitPerMin)
	if ipKey == "" {
		return true, ""
	}
	if ip := m.ipAbuse[ipKey]; ip.BannedUntil > now {
		return false, "worker_temporarily_banned"
	}
	ip, ok := m.allowRateSlot(m.ipAbuse[ipKey], now, perMin*4, false)
	m.ipAbuse[ipKey] = ip
	if !ok {
		return false, "submit_rate_limited"
	}
	return true, ""
}

// chargeSubmitWorker consumes the declared worker_id submit slot. Call only after
// signature / payout-lock identity is proven (report #28).
func (m *workManager) chargeSubmitWorker(workerID string, now int64) (bool, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.chargeSubmitWorkerLocked(workerID, now)
}

// chargeClaimWorker consumes the declared worker_id claim slot after a successful lease.
func (m *workManager) chargeClaimWorker(workerID string, now int64) (bool, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.chargeClaimWorkerLocked(workerID, now)
}

func (m *workManager) chargeClaimWorkerLocked(workerID string, now int64) (bool, string) {
	if m.claimPerMin <= 0 {
		return true, ""
	}
	m.pruneAbuseStateLocked(now)
	if m.abuse == nil {
		m.abuse = make(map[string]workerAbuseState)
	}
	perMin := m.workerRateLimitPerMin(workerID, m.claimPerMin)
	s, ok := m.allowRateSlot(m.abuse[workerID], now, perMin, true)
	m.abuse[workerID] = s
	if !ok {
		return false, "claim_rate_limited"
	}
	return true, ""
}

func (m *workManager) chargeSubmitWorkerLocked(workerID string, now int64) (bool, string) {
	if m.submitPerMin <= 0 {
		return true, ""
	}
	m.pruneAbuseStateLocked(now)
	if m.abuse == nil {
		m.abuse = make(map[string]workerAbuseState)
	}
	perMin := m.workerRateLimitPerMin(workerID, m.submitPerMin)
	s, ok := m.allowRateSlot(m.abuse[workerID], now, perMin, false)
	m.abuse[workerID] = s
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

// submitRejectHTTPStatus maps coordinator reject reasons to HTTP status for miners.
func submitRejectHTTPStatus(reason string) int {
	switch reason {
	case "invalid_signature", "invalid_pubkey", "pubkey_address_mismatch", "missing_signature_fields",
		"signature_required", "found_signature_required", "replay", "duplicate_signed_payload",
		"unsupported_sig_alg":
		return http.StatusForbidden
	case "work_id_mismatch", "unknown_or_already_closed_range", "range_leased_to_another_worker",
		"found_nonce_out_of_range", "result_hash_required_for_found", "duplicate_found_nonce":
		return http.StatusBadRequest
	default:
		if reason != "" {
			return http.StatusConflict
		}
		return http.StatusOK
	}
}

func (m *workManager) markSubmitOutcome(workerID, ipKey, reason string, now int64) {
	if reason == "" {
		return
	}
	workerStrike := false
	ipStrike := false
	sigFail := false
	switch reason {
	case "work_id_mismatch", "range_leased_to_another_worker":
		// Spoofable with shared pool token: attacker leases as self, submits with
		// victim worker_id. Never charge the nominal worker — IP only.
		ipStrike = true
	case "found_nonce_out_of_range", "result_hash_required_for_found", "duplicate_found_nonce":
		// Requires a live lease under worker_id (identity gated when payout locked).
		workerStrike = true
		ipStrike = true
	case "invalid_signature", "invalid_pubkey", "pubkey_address_mismatch", "missing_signature_fields",
		"signature_required", "found_signature_required", "duplicate_signed_payload", "unsupported_sig_alg":
		// Shared pool token — do not ban worker_id on forged/unsigned sigs.
		// IP still accumulates (rotating proxies cost attacker).
		sigFail = true
		ipStrike = true
	case "replay", "unknown_or_already_closed_range", "lease_expired":
		// Benign races on fast GPU / proxy timeouts: reject only, do not temp-ban home NAT IPs.
	case "payout_address_locked":
		// M14 reject on the fuzz submit lane: the declared worker_id holds the
		// payout lock (the victim) while the submitter signed with a foreign key.
		// Strike neither — a forger must not rack strikes against the victim id,
		// and a legit miner whose client still signs with a rotated-away key hits
		// this on every retry, so this reason must never land in a strike bucket
		// (it would temp-ban the miner's own IP).
	case "batch_size_too_large", "impossible_found_rate":
		workerStrike = true
		ipStrike = true
	default:
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// Never promote sigFail → workerStrike based on locked payout: that inverted M3
	// and let claim-as-victim + unsigned submit temp-ban real miners.
	_ = sigFail
	applyStrike := func(s workerAbuseState) workerAbuseState {
		if !workerStrike && !ipStrike {
			return s
		}
		s.BadStrikes++
		if s.BadStrikes >= m.badStrikesToBan {
			s.BannedUntil = now + m.banSec
			s.BadStrikes = 0
		}
		return s
	}
	if workerStrike && workerID != "" {
		m.abuse[workerID] = applyStrike(m.abuse[workerID])
	}
	if ipStrike && strings.TrimSpace(ipKey) != "" {
		m.ipAbuse[ipKey] = applyStrike(m.ipAbuse[ipKey])
	}
}

// clearWorkerAbuse removes rate-limit / ban state for a worker (and optional IP key).
func coordinatorIdlePruneSec() int64 {
	if v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_IDLE_PRUNE_SEC")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 120 {
			return n
		}
	}
	return 600
}

func coordinatorAutoPruneIdle() bool {
	v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_AUTO_PRUNE_IDLE"))
	if v == "" {
		return true
	}
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

func coordinatorIdlePruneMaxHMC() float64 {
	if v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_IDLE_PRUNE_MAX_HMC")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
			return f
		}
	}
	return 2.0
}

// pruneStaleWorkersLocked drops offline idle workers; caller must hold m.mu.
// Settlement history lives in node worker_settlement_state.json — coordinator rows are ephemeral.
func (m *workManager) pruneStaleWorkersLocked(prefix string, maxPayout float64, staleSec int64, dryRun bool, ignorePayout bool, now int64) (removed, kept []string) {
	if m == nil {
		return nil, nil
	}
	prefix = strings.TrimSpace(prefix)
	for id, st := range m.worker {
		if prefix != "" && !strings.HasPrefix(id, prefix) {
			kept = append(kept, id)
			continue
		}
		busy := false
		for _, rec := range m.active {
			if rec.WorkerID == id {
				busy = true
				break
			}
		}
		if busy {
			kept = append(kept, id)
			continue
		}
		offline := st.LastSeenUnix <= 0 || (now-st.LastSeenUnix) > staleSec
		if !offline {
			kept = append(kept, id)
			continue
		}
		if !ignorePayout && st.PayoutHMC > maxPayout {
			kept = append(kept, id)
			continue
		}
		removed = append(removed, id)
		if !dryRun {
			m.totalPayoutHMC -= st.PayoutHMC
			if m.totalPayoutHMC < 0 {
				m.totalPayoutHMC = 0
			}
			m.totalPayoutSUP -= st.PayoutSUP
			if m.totalPayoutSUP < 0 {
				m.totalPayoutSUP = 0
			}
			delete(m.worker, id)
			delete(m.abuse, id)
		}
	}
	return removed, kept
}

// pruneStaleWorkers drops offline workers (no active lease). When ignorePayout is false, also requires payout <= maxPayout.
func (m *workManager) pruneStaleWorkers(prefix string, maxPayout float64, staleSec int64, dryRun bool, ignorePayout bool) (removed, kept []string) {
	if m == nil {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pruneStaleWorkersLocked(prefix, maxPayout, staleSec, dryRun, ignorePayout, time.Now().Unix())
}

func (m *workManager) pruneIdleWorkersLocked(now int64) []string {
	if m == nil || !coordinatorAutoPruneIdle() {
		return nil
	}
	if now-m.lastIdlePruneUnix < 300 {
		return nil
	}
	m.lastIdlePruneUnix = now
	removed, _ := m.pruneStaleWorkersLocked("", coordinatorIdlePruneMaxHMC(), coordinatorIdlePruneSec(), false, false, now)
	return removed
}

func (m *workManager) clearWorkerAbuse(workerID, ipKey string) bool {
	if m == nil {
		return false
	}
	workerID = strings.TrimSpace(workerID)
	ipKey = strings.TrimSpace(ipKey)
	if workerID == "" && ipKey == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cleared := false
	if workerID != "" {
		if _, ok := m.abuse[workerID]; ok {
			delete(m.abuse, workerID)
			cleared = true
		}
	}
	if ipKey != "" {
		if _, ok := m.ipAbuse[ipKey]; ok {
			delete(m.ipAbuse, ipKey)
			cleared = true
		}
	}
	return cleared
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

func payoutAddressLockedReason(locked, submitted string) string {
	locked = strings.TrimSpace(locked)
	submitted = strings.TrimSpace(submitted)
	if locked == "" && submitted == "" {
		return "payout_address_locked"
	}
	return fmt.Sprintf("payout_address_locked:locked=%s:submitted=%s", locked, submitted)
}

// checkClaimMinerIdentity validates claim pubkey/address against a locked worker payout.
// When claimRequirePubKey is set (default under hybrid), miner_pubkey is mandatory.
// When require is off, omitted identity is a legacy claim and does not touch the lock.
// Durable locks are NOT written here — callers bind only after a successful claim
// via bindClaimPayoutFromPub (failed/no_fuzz_work must not squat an id).
func (m *workManager) checkClaimMinerIdentity(workerID, pubHex, addrHint string) (ok bool, reason string) {
	return m.checkMinerIdentity(workerID, pubHex, addrHint, false)
}

// checkReleaseMinerIdentity validates worker_id→payout binding for release without
// creating a lock (report #27: a failed/no-op release must not register identity).
func (m *workManager) checkReleaseMinerIdentity(workerID, pubHex, addrHint string) (ok bool, reason string) {
	return m.checkMinerIdentity(workerID, pubHex, addrHint, false)
}

// bindClaimPayoutFromPub writes the sticky payout lock after a successful claim/lease.
func (m *workManager) bindClaimPayoutFromPub(workerID, pubHex string) {
	pubHex = strings.TrimSpace(pubHex)
	if pubHex == "" {
		return
	}
	derived, ok := deriveAddressFromPubHex(pubHex)
	if !ok {
		return
	}
	m.notePayoutLock(workerID, derived)
}

// checkMinerIdentity validates pubkey/address against an existing payout lock.
// When createLock is true and no lock exists, the derived address is bound (claim lane).
func (m *workManager) checkMinerIdentity(workerID, pubHex, addrHint string, createLock bool) (ok bool, reason string) {
	if m == nil {
		return true, ""
	}
	pubHex = strings.TrimSpace(pubHex)
	addrHint = strings.TrimSpace(addrHint)
	require := m.claimRequirePubKey && m.hybridSignerEnabled

	locked := m.lockedPayoutAddress(workerID)

	if require && pubHex == "" {
		return false, "claim_pubkey_required"
	}

	if pubHex == "" {
		// Legacy claim (require off): renew lease without establishing a lock from a
		// forgeable miner_address hint. Still reject if a lock already exists and the
		// hint disagrees (keeps sticky binding for keyed workers).
		if addrHint != "" && locked != "" && !strings.EqualFold(locked, addrHint) {
			return false, payoutAddressLockedReason(locked, addrHint)
		}
		return true, ""
	}

	derived, okAddr := deriveAddressFromPubHex(pubHex)
	if !okAddr {
		return false, "invalid_pubkey"
	}
	if addrHint != "" && !strings.EqualFold(addrHint, derived) {
		return false, "pubkey_address_mismatch"
	}
	if locked != "" && !strings.EqualFold(locked, derived) {
		return false, payoutAddressLockedReason(locked, derived)
	}
	if locked == "" && createLock {
		m.notePayoutLock(workerID, derived)
	}
	return true, ""
}

// notePayoutLock binds worker_id to the first claim identity and persists it.
// Refuses to create a new worker row when maxWorkers is already saturated
// (unbounded lock growth via random ids).
func (m *workManager) notePayoutLock(workerID, addr string) {
	if m == nil {
		return
	}
	workerID = strings.TrimSpace(workerID)
	addr = strings.TrimSpace(addr)
	if workerID == "" || addr == "" {
		return
	}
	m.mu.Lock()
	if m.worker == nil {
		m.worker = map[string]workerPayoutStat{}
	}
	st, exists := m.worker[workerID]
	if !exists && m.maxWorkers > 0 && len(m.worker) >= m.maxWorkers {
		m.mu.Unlock()
		return
	}
	cur := strings.TrimSpace(st.PayoutAddress)
	if cur == "" {
		st.PayoutAddress = addr
		m.worker[workerID] = st
		cur = addr
	}
	m.mu.Unlock()
	if strings.EqualFold(cur, addr) {
		m.persistPayoutLock(workerID, addr)
	}
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

// submitNonceScope keys hybrid replay protection per (miner key, worker_id).
// Fleet rigs often share HACKME_MINER_ED25519_SEED_HEX; scoping by worker avoids
// cross-rig submit_nonce collisions that caused mass replay → IP temp-bans.
func submitNonceScope(signerAddr, workerID string) string {
	wid := strings.TrimSpace(workerID)
	if wid == "" {
		return signerAddr
	}
	return signerAddr + "|" + wid
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
	scope := submitNonceScope(derivedAddr, req.WorkerID)
	if maxNonce, ok := m.signedSubmitNonceMax[scope]; ok && req.SubmitNonce <= maxNonce {
		return false, "replay", ""
	}
	// Legacy: older coordinators keyed only by miner address. Keep rejecting those
	// nonces for the same worker so a restart cannot replay pre-scope accepts.
	legacyKey := derivedAddr + ":" + strconv.FormatUint(req.SubmitNonce, 10)
	nonceKey := scope + ":" + strconv.FormatUint(req.SubmitNonce, 10)
	if _, exists := m.acceptedSubmitNonces[nonceKey]; exists {
		return false, "replay", ""
	}
	if _, exists := m.acceptedSubmitNonces[legacyKey]; exists && strings.TrimSpace(req.WorkerID) == "" {
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
	if m.maxClaimBatch > 0 && batch > m.maxClaimBatch {
		return 0, 0, 0, m.targetMod, false, false, "batch_size_too_large"
	}
	now := time.Now().Unix()
	m.prefetchTargetMod(now)
	m.mu.Lock()
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
	// Reuse expired range of the same batch before purging stale entries.
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
		rec.TargetMod = m.targetMod
		m.active[k] = rec
		m.reissuedRanges++
		m.issuedRanges++
		m.mu.Unlock()
		return rec.BaseNonce, rec.BatchSize, rec.ExpiresAt, rec.TargetMod, true, true, ""
	}
	m.purgeExpiredLeases(now)
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
		IssuedAt:  now,
		ExpiresAt: leaseUntil,
		TargetMod: m.targetMod,
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
		// Keep lease on signature failures — a shared-token forger must not burn
		// a victim lease by declaring its worker_id with a bad/unsigned sig.
		// (Replay/duplicate already kept the lease; extend to all sig rejects.)
		m.signedRejects++
		m.rejectedSubmits++
		return false, sigReason, 0, "", false
	}
	if signerAddr != "" {
		if locked := strings.TrimSpace(m.worker[req.WorkerID].PayoutAddress); locked != "" && !strings.EqualFold(locked, signerAddr) {
			m.signedRejects++
			m.rejectedSubmits++
			return false, payoutAddressLockedReason(locked, signerAddr), 0, "", false
		}
	}
	if rec.ExpiresAt < now {
		delete(m.active, k)
		m.staleSubmits++
		m.noteWorkerStale(req.WorkerID, now)
		return false, "lease_expired", 0, signerAddr, true
	}
	// Report #28 + harden: charge worker slot only after identity AND a live lease
	// (expired/no-op must not burn the victim's submit budget).
	if okRate, reasonRate := m.chargeSubmitWorkerLocked(req.WorkerID, now); !okRate {
		m.rejectedSubmits++
		return false, reasonRate, 0, signerAddr, true
	}
	if m.maxClaimBatch > 0 && req.BatchSize > m.maxClaimBatch {
		delete(m.active, k)
		m.rejectedSubmits++
		m.permabanWorkerLocked(req.WorkerID, "")
		return false, "batch_size_too_large", 0, "", false
	}
	if req.Found {
		prev := m.worker[req.WorkerID]
		avgAtt := uint64(0)
		if prev.AcceptedRanges > 0 {
			avgAtt = prev.AcceptedAtt / prev.AcceptedRanges
		}
		// 100% found with inflated attempts/range (hdssh-style), not fast GPU on easy M.
		suspiciousAvg := m.defaultBatch * 2
		if m.maxClaimBatch > 0 && m.maxClaimBatch*2 < suspiciousAvg {
			suspiciousAvg = m.maxClaimBatch * 2
		}
		if prev.AcceptedRanges >= 10 && prev.AcceptedHits >= prev.AcceptedRanges &&
			avgAtt > suspiciousAvg {
			delete(m.active, k)
			m.rejectedSubmits++
			m.permabanWorkerLocked(req.WorkerID, prev.LastClientIP)
			return false, "impossible_found_rate", 0, "", false
		}
	}
	leaseMod := m.targetMod
	if rec.TargetMod > 0 {
		leaseMod = rec.TargetMod
	}
	issuedAt := rec.IssuedAt
	validateMod := leaseMod
	resultHashKey := ""
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
		if !validFoundNonceV1(req.FoundNonce, validateMod) {
			m.rejectedSubmits++
			return false, "found_nonce_invalid_for_target_mod", 0, "", false
		}
		// M2: reject duplicate result_hash before consuming lease / recording nonces.
		resultHashKey = strings.TrimSpace(strings.ToLower(req.ResultHash))
		if resultHashKey != "" {
			if _, exists := m.acceptedResultHashes[resultHashKey]; exists {
				m.dedupSubmits++
				return false, "duplicate_result_hash", 0, "", false
			}
		}
	}

	orderID := strings.TrimSpace(req.OrderTaskID)
	needOrderPath := req.Found && (req.WasmGatePass || orderID != "")
	var orderSnap activeOrderSnap
	var chainSolve orderSolveRelayResult
	leaseDeadAfterChain := false
	if needOrderPath {
		// H3: release work mutex around orders probe / WASM / HTTP relay.
		m.mu.Unlock()
		orderSnap = m.activeOrderSnapshot()
		if orderSnap.ID != "" && strings.TrimSpace(orderSnap.WasmHex) != "" {
			if !req.WasmGatePass {
				m.mu.Lock()
				m.rejectedSubmits++
				return false, "wasm_gate_required", 0, "", false
			}
			if okGate, gateReason := m.verifyOrderWasmGate(orderSnap, req.FoundNonce); !okGate {
				m.mu.Lock()
				m.rejectedSubmits++
				return false, gateReason, 0, "", false
			}
			if orderID != "" && orderID != orderSnap.ID {
				m.mu.Lock()
				m.rejectedSubmits++
				return false, "order_task_mismatch", 0, "", false
			}
		}
		// H2: relay before consuming lease/dedup so transient chain/ops failures remain retryable.
		if req.Found && req.WasmGatePass && orderID != "" && signerAddr != "" && orderSnap.ID == orderID {
			mod := leaseMod
			if orderSnap.ChainMod > 0 {
				mod = orderSnap.ChainMod
			}
			chainSolve = m.relayOrderSolve(signerAddr, req.FoundNonce, mod, orderID)
			if !chainSolve.OK {
				m.mu.Lock()
				m.rejectedSubmits++
				return false, "order_chain_solve_failed:" + chainSolve.Reason, 0, signerAddr, signerAddr != ""
			}
		}
		m.mu.Lock()
		// Refresh after the relay so the found-dedup timestamp reflects chain-ACK time,
		// not submit-entry time (stall easing consumes lastFoundHitUnix).
		now = time.Now().Unix()
		// V2-H1: after chain ACK, mark found dedup immediately so lease expiry cannot skip it.
		if chainSolve.OK && req.Found {
			m.recordFoundDedupLocked(req.FoundNonce, resultHashKey, now)
		}
		rec, ok = m.active[k]
		if chainSolve.OK {
			if !ok || rec.WorkerID != req.WorkerID || rec.ExpiresAt < now {
				leaseDeadAfterChain = true
			}
		}
		if !chainSolve.OK || !leaseDeadAfterChain {
			if !ok {
				m.unknownSubmits++
				return false, "unknown_or_already_closed_range", 0, "", false
			}
			if rec.WorkerID != req.WorkerID {
				m.rejectedSubmits++
				return false, "range_leased_to_another_worker", 0, "", false
			}
			if rec.ExpiresAt < now {
				delete(m.active, k)
				m.staleSubmits++
				m.noteWorkerStale(req.WorkerID, now)
				return false, "lease_expired", 0, "", false
			}
			if req.Found && !chainSolve.OK {
				if _, exists := m.acceptedFoundNonces[req.FoundNonce]; exists {
					m.dedupFoundNonce++
					return false, "duplicate_found_nonce", 0, "", false
				}
				if resultHashKey != "" {
					if _, exists := m.acceptedResultHashes[resultHashKey]; exists {
						m.dedupSubmits++
						return false, "duplicate_result_hash", 0, "", false
					}
				}
			}
		}
		if ok {
			issuedAt = rec.IssuedAt
		}
	}

	if ok {
		delete(m.active, k)
	}
	m.submittedItems++
	if signerAddr != "" {
		scope := submitNonceScope(signerAddr, req.WorkerID)
		nonceKey := scope + ":" + strconv.FormatUint(req.SubmitNonce, 10)
		if len(m.acceptedSubmitNonces) >= m.maxDedupEntries {
			for nk := range m.acceptedSubmitNonces {
				delete(m.acceptedSubmitNonces, nk)
				break
			}
		}
		m.acceptedSubmitNonces[nonceKey] = struct{}{}
		m.signedSubmitNonceMax[scope] = req.SubmitNonce
		sig, _ := hex.DecodeString(strings.TrimSpace(req.MinerSig))
		canon := canonicalSubmitBytes(req)
		sum := sha256.Sum256(append(canon, sig...))
		sigPayload := hex.EncodeToString(sum[:])
		if len(m.acceptedSignedPayloads) >= m.maxDedupEntries {
			for pk := range m.acceptedSignedPayloads {
				delete(m.acceptedSignedPayloads, pk)
				break
			}
		}
		m.acceptedSignedPayloads[sigPayload] = struct{}{}
		m.persistSignedPayload(sigPayload)
		m.signedAccepts++
		m.lastSignedMiner = signerAddr
	}
	if req.Found && !chainSolve.OK {
		m.recordFoundDedupLocked(req.FoundNonce, resultHashKey, now)
	}
	attempts := req.Attempts
	if attempts == 0 {
		attempts = req.BatchSize
	}
	if attempts > req.BatchSize {
		attempts = req.BatchSize
	}
	attempts = m.clampPaidAttempts(attempts, req.BatchSize)
	reportedGH := clampWorkerHashrateGHS(req.HashrateGHS)
	rawGH := workerHashrateGHSForSubmit(reportedGH, req.BatchSize, issuedAt, now, m.worker[req.WorkerID].LastHashrateGHS)
	gh := smoothWorkerHashrateGHS(m.worker[req.WorkerID].LastHashrateGHS, rawGH)
	if req.Found && reportedGH <= 0 {
		gh = 0
	}
	if gh > 0 {
		ia := issuedAt
		if ia <= 0 {
			ia = now - m.leaseSec
		}
		elapsed := float64(now - ia)
		if elapsed < 0.25 {
			elapsed = 0.25
		}
		maxCred := uint64(gh * 1e9 * elapsed * 1.15)
		if maxCred > 0 && attempts > maxCred {
			attempts = maxCred
		}
	} else if !req.Found {
		// No credible hashrate: do not pay attempt accrual (found proofs still validated).
		attempts = 0
	} else if req.Found && gh <= 0 {
		// Found without hashrate proof: found_bonus only, no attempt inflation.
		attempts = 0
	}
	attempts = m.clampPaidAttempts(attempts, req.BatchSize)
	m.totalAttempts += attempts
	paidAttempts := attempts
	if m.payoutFoundOnly && !req.Found {
		paidAttempts = 0
	}
	payout := (float64(paidAttempts) / 1_000_000.0) * m.rewardPerM
	if req.Found && !chainSolve.OK {
		payout += m.foundBonus
	}
	// Order block committed on chain: escrow pays miner_address; coordinator pays only small attempt accrual.
	if chainSolve.OK {
		payout = (float64(paidAttempts) / 1_000_000.0) * m.rewardPerM
	}
	if payout < 0 {
		payout = 0
	}
	// Hard per-submit HMC ceiling (attempt accrual + found bonus).
	maxSubmitHMC := (float64(m.defaultBatch)/1_000_000.0)*m.rewardPerM*1.05 + m.foundBonus*1.05
	if payout > maxSubmitHMC {
		payout = maxSubmitHMC
	}
	st := m.worker[req.WorkerID]
	st.AcceptedRanges++
	st.AcceptedAtt += attempts
	if req.Found {
		st.AcceptedHits++
	}
	if signerAddr != "" {
		if strings.TrimSpace(st.PayoutAddress) == "" {
			st.PayoutAddress = signerAddr
			m.persistPayoutLock(req.WorkerID, signerAddr)
		}
		st.SignedSubmits++
	}
	if gh > 0 {
		st.LastHashrateGHS = gh
		if gh > st.PeakHashrateGHS {
			st.PeakHashrateGHS = gh
		}
		st.LastPoHSeenUnix = now
	}
	st.LastSeenUnix = time.Now().Unix()
	st.PayoutHMC += payout
	hybridOK := signerAddr != ""
	if sup := m.computeSUPAccrual(req.WorkerID, payout, paidAttempts, hybridOK, now); sup > 0 {
		st.PayoutSUP += sup
		m.totalPayoutSUP += sup
	}
	m.worker[req.WorkerID] = st
	m.totalPayoutHMC += payout
	// Keep pool M aligned with live fleet GH/s even when nothing polls /api/work/stats (top pools
	// continuously adjust difficulty from observed work; we approximate via submit heartbeats).
	retNow := time.Now().Unix()
	poolGH, online, _ := m.poolOnlineSummaryUnlocked(120, retNow)
	if smoothed := m.smoothPoolGHSample(poolGH); smoothed > poolGH {
		poolGH = smoothed
	}
	m.maybeRetargetPoolLoadLocked(retNow, poolGH, online)
	return req.Found, "", payout, signerAddr, signerAddr != ""
}

func (m *workManager) probeOrdersActive() bool {
	if !m.ordersPriority || strings.TrimSpace(m.ordersProbeURL) == "" {
		return false
	}
	cl := &http.Client{Timeout: 1200 * time.Millisecond}
	req, err := http.NewRequest(http.MethodGet, m.ordersProbeURL+"/api/tasks", nil)
	if err != nil {
		return false
	}
	if tok := m.ordersAdminToken(); tok != "" {
		req.Header.Set("X-Hackme-Admin-Token", tok)
	}
	resp, err := cl.Do(req)
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
		if active {
			_ = m.refreshActiveOrderLocked()
		} else {
			m.activeOrder = activeOrderSnap{}
		}
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

func bytesToMB(b uint64) float64 {
	return float64(b) / (1024 * 1024)
}

// runtimeMemSnapshot returns coordinator heap stats and in-memory map sizes (for leak audits).
func (m *workManager) runtimeMemSnapshot() map[string]any {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	m.mu.Lock()
	abuseN := len(m.abuse)
	ipAbuseN := len(m.ipAbuse)
	workerN := len(m.worker)
	activeN := len(m.active)
	dedupN := len(m.acceptedSignedPayloads)
	now := time.Now().Unix()
	_, _, rigs := m.poolOnlineSummaryUnlocked(poolHashrateStaleSec, now)
	rigsN := len(rigs)
	m.mu.Unlock()
	return map[string]any{
		"heap_alloc_mb":           roundMemMB(bytesToMB(ms.HeapAlloc)),
		"heap_inuse_mb":           roundMemMB(bytesToMB(ms.HeapInuse)),
		"heap_sys_mb":             roundMemMB(bytesToMB(ms.HeapSys)),
		"stack_inuse_mb":          roundMemMB(bytesToMB(ms.StackInuse)),
		"gc_cycles":               ms.NumGC,
		"abuse_workers":           abuseN,
		"ip_abuse_entries":        ipAbuseN,
		"workers_tracked":         workerN,
		"active_leases":           activeN,
		"active_rigs":             rigsN,
		"signed_dedup_cache":      dedupN,
		"client_ip_trust_enabled": trustClientForwardedFor,
	}
}

func roundMemMB(v float64) float64 {
	return math.Round(v*100) / 100
}

func (m *workManager) recordFoundDedupLocked(foundNonce uint64, resultHashKey string, now int64) {
	if resultHashKey != "" {
		if _, exists := m.acceptedResultHashes[resultHashKey]; !exists {
			if len(m.acceptedResultHashes) >= m.maxDedupEntries {
				for hk := range m.acceptedResultHashes {
					delete(m.acceptedResultHashes, hk)
					break
				}
			}
			m.acceptedResultHashes[resultHashKey] = struct{}{}
			m.persistResultHash(resultHashKey)
		}
	}
	if _, exists := m.acceptedFoundNonces[foundNonce]; !exists {
		if len(m.acceptedFoundNonces) >= m.maxDedupEntries {
			for nk := range m.acceptedFoundNonces {
				delete(m.acceptedFoundNonces, nk)
				break
			}
		}
		m.acceptedFoundNonces[foundNonce] = struct{}{}
		m.foundHits++
		m.maybeRetargetPoolMod(now)
	}
}

func fleetBaseWorkerID(id string) string {
	i := strings.LastIndex(id, "-gpu")
	if i <= 0 || i+4 >= len(id) {
		return id
	}
	for _, c := range id[i+4:] {
		if c < '0' || c > '9' {
			return id
		}
	}
	return id[:i]
}

func mergeWorkerStat(dst, src workerPayoutStat) (workerPayoutStat, bool) {
	dstAddr := strings.TrimSpace(dst.PayoutAddress)
	srcAddr := strings.TrimSpace(src.PayoutAddress)
	addrConflict := dstAddr != "" && srcAddr != "" && !strings.EqualFold(dstAddr, srcAddr)
	if addrConflict || dst.AddressConflict || src.AddressConflict {
		// Keep accruals visible for ops/settle drift detection; clear address so
		// autopilot cannot pay the wrong HMC target after a fleet merge.
		// Conflict is sticky for the rest of this fold: a later -gpuN row must
		// not reinstall its own address into the blanked slot (report #17).
		dst.PayoutAddress = ""
		dst.AddressConflict = true
	}
	dst.AcceptedRanges += src.AcceptedRanges
	dst.AcceptedHits += src.AcceptedHits
	dst.AcceptedAtt += src.AcceptedAtt
	dst.PayoutHMC += src.PayoutHMC
	dst.PayoutSUP += src.PayoutSUP
	dst.SignedSubmits += src.SignedSubmits
	dst.LastHashrateGHS += src.LastHashrateGHS
	dst.PeakHashrateGHS += src.PeakHashrateGHS
	if dst.LastHashrateGHS > dst.PeakHashrateGHS {
		dst.PeakHashrateGHS = dst.LastHashrateGHS
	}
	if src.LastSeenUnix > dst.LastSeenUnix {
		dst.LastSeenUnix = src.LastSeenUnix
	}
	if src.LastPoHSeenUnix > dst.LastPoHSeenUnix {
		dst.LastPoHSeenUnix = src.LastPoHSeenUnix
	}
	if src.LastFuzzSeenUnix > dst.LastFuzzSeenUnix {
		dst.LastFuzzSeenUnix = src.LastFuzzSeenUnix
	}
	if !dst.AddressConflict && !addrConflict && dst.PayoutAddress == "" && srcAddr != "" {
		dst.PayoutAddress = srcAddr
	}
	if dst.LastClientIP == "" && src.LastClientIP != "" {
		dst.LastClientIP = src.LastClientIP
	}
	dst.Online = dst.Online || src.Online
	if src.AddressConflict {
		dst.AddressConflict = true
	}
	return dst, addrConflict
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

const poolHashrateStaleSec int64 = 300 // include GPU rigs with 90s leases + submit jitter (120s hid Windows ~3 GH/s)

// poolOnlineSummaryUnlocked aggregates live hashrate; caller must hold m.mu.
func (m *workManager) poolOnlineSummaryUnlocked(staleSec, now int64) (poolGH float64, online int, rigs []map[string]any) {
	if m == nil {
		return 0, 0, nil
	}
	if staleSec < poolHashrateStaleSec {
		staleSec = poolHashrateStaleSec
	}
	type rigAgg struct {
		gh       float64
		lastSeen int64
	}
	agg := make(map[string]rigAgg, len(m.worker))
	for id, st := range m.worker {
		if st.LastSeenUnix <= 0 || (now-st.LastSeenUnix) > staleSec {
			continue
		}
		gh := effectiveWorkerHashrateGHS(st)
		if gh > 0 {
			poolGH += gh
		}
		base := fleetBaseWorkerID(id)
		cur := agg[base]
		cur.gh += gh
		if st.LastSeenUnix > cur.lastSeen {
			cur.lastSeen = st.LastSeenUnix
		}
		agg[base] = cur
	}
	for id, st := range agg {
		online++
		rigs = append(rigs, map[string]any{
			"worker_id":      id,
			"name":           id,
			"hashrate_gh_s":  st.gh,
			"online":         true,
			"source":         "coordinator_submit",
			"last_seen_unix": st.lastSeen,
		})
	}
	return poolGH, online, rigs
}

// poolHashrateFromWorkersUnlocked sums hashrate_gh_s for all workers with a recent last_seen (caller holds m.mu).
func (m *workManager) poolHashrateFromWorkersUnlocked(now int64) float64 {
	if m == nil {
		return 0
	}
	var sum float64
	for _, st := range m.worker {
		if st.LastSeenUnix <= 0 || (now-st.LastSeenUnix) > poolHashrateStaleSec {
			continue
		}
		if gh := effectiveWorkerHashrateGHS(st); gh > 0 {
			sum += gh
		}
	}
	return sum
}

// poolOnlineSummary aggregates live hashrate from recent worker submits (public pool rigs).
func (m *workManager) poolOnlineSummary(staleSec int64) (poolGH float64, online int, rigs []map[string]any) {
	if m == nil {
		return 0, 0, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().Unix()
	return m.poolOnlineSummaryUnlocked(staleSec, now)
}

// enrichPoolStatsForPublic adds hashrate/worker fields expected by pool listing sites (e.g. MiningPoolStats API poll).
func enrichPoolStatsForPublic(out map[string]any, reg *lanpool.Registry, wm *workManager) {
	if out == nil {
		return
	}
	const staleSec int64 = 120
	poolGH, online, rigs := float64(0), 0, []map[string]any(nil)
	if wm != nil {
		poolGH, online, rigs = wm.poolOnlineSummary(staleSec)
	}
	if poolGH <= 0 && reg != nil {
		for _, m := range reg.ListOnline() {
			poolGH += m.HashrateGHS
		}
		online = len(reg.ListOnline())
	}
	out["pool_hashrate_gh_s"] = poolGH
	out["hashrate"] = poolGH * 1e9
	out["hashrate_hs"] = poolGH * 1e9
	out["miners"] = online
	out["workers_online"] = online
	if len(rigs) > 0 {
		anyRigs := make([]any, len(rigs))
		for i, r := range rigs {
			anyRigs[i] = r
		}
		out["active_rigs"] = anyRigs
	}
	if wm != nil {
		wm.maybeRetargetPoolLoad(time.Now().Unix(), poolGH, online)
		wm.mu.Lock()
		applied := wm.clampTargetMod(wm.targetMod)
		hint := poolLoadHintUnclamped(poolGH, online, wm.targetModMinOrDefault())
		minM := wm.targetModMinOrDefault()
		maxM := wm.targetModMaxOrDefault()
		updated := wm.targetModUpdatedUnix
		rpm := wm.rewardPerM
		ra := wm.rewardAuto
		wm.mu.Unlock()
		out["target_mod"] = applied
		out["target_mod_updated_unix"] = updated
		out["target_mod_min"] = minM
		out["target_mod_max"] = maxM
		if hint > 0 {
			out["target_mod_load_hint"] = hint
		}
		out["target_mod_load_capped"] = hint > maxM && maxM > 0
		if ra {
			out["reward_per_m"] = rpm
		}
	}
	// Never overwrite workers{} breakdown map (settlement + dashboard need payout_hmc per id).
}

func shallowCloneStats(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in)+8)
	for k, v := range in {
		out[k] = v
	}
	return out
}

const workStatsCacheTTL = 2 * time.Second

// statsCached returns a shallow clone of stats with a short TTL to cut public poller CPU.
func (m *workManager) statsCached(includeDetails bool) map[string]any {
	ttl := workStatsCacheTTL
	m.statsCacheMu.Lock()
	now := time.Now()
	if includeDetails {
		if m.statsCacheDetails != nil && now.Sub(m.statsCacheDetAt) < ttl {
			out := shallowCloneStats(m.statsCacheDetails)
			m.statsCacheMu.Unlock()
			return out
		}
	} else if m.statsCachePlain != nil && now.Sub(m.statsCachePlainAt) < ttl {
		out := shallowCloneStats(m.statsCachePlain)
		m.statsCacheMu.Unlock()
		return out
	}
	m.statsCacheMu.Unlock()

	built := m.stats(includeDetails)

	m.statsCacheMu.Lock()
	if includeDetails {
		m.statsCacheDetails = built
		m.statsCacheDetAt = time.Now()
	} else {
		m.statsCachePlain = built
		m.statsCachePlainAt = time.Now()
	}
	out := shallowCloneStats(built)
	m.statsCacheMu.Unlock()
	return out
}

func (m *workManager) stats(includeDetails bool) map[string]any {
	now := time.Now().Unix()
	m.prefetchTargetMod(now)
	m.mu.Lock()
	defer m.mu.Unlock()
	_ = m.pruneIdleWorkersLocked(now)
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
	poolGHStat, onlineStat, _ := m.poolOnlineSummaryUnlocked(poolHashrateStaleSec, now)
	workersGH := m.poolHashrateFromWorkersUnlocked(now)
	if workersGH > poolGHStat {
		poolGHStat = workersGH
	}
	hintStat := poolLoadHintUnclamped(poolGHStat, onlineStat, m.targetModMinOrDefault())
	minStat := m.targetModMinOrDefault()
	maxStat := m.targetModMaxOrDefault()
	out := map[string]any{
		"default_batch":                m.defaultBatch,
		"max_claim_batch":              m.maxClaimBatch,
		"target_mod":                   m.clampTargetMod(m.targetMod),
		"target_mod_updated_unix":      m.targetModUpdatedUnix,
		"pool_retarget_enabled":        m.poolRetarget,
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
		"fleet_address_conflicts":      m.fleetAddressConflicts,
		"accepted_attempts":            m.totalAttempts,
		"total_payout_hmc":             m.totalPayoutHMC,
		"total_payout_sup":             m.totalPayoutSUP,
		"sup_policy":                   m.supPolicyStats(),
		"claim_per_min":                m.claimPerMin,
		"submit_per_min":               m.submitPerMin,
		"ban_sec":                      m.banSec,
		"bad_strikes_to_ban":           m.badStrikesToBan,
		"workers_tracked":              len(m.worker),
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
		"claim_require_pubkey":         m.claimRequirePubKey && m.hybridSignerEnabled,
		"signed_submits_accepted":      m.signedAccepts,
		"signed_submits_rejected":      m.signedRejects,
		"last_signed_miner_address":    m.lastSignedMiner,
	}
	out["target_mod_min"] = minStat
	out["target_mod_max"] = maxStat
	if hintStat > 0 {
		out["target_mod_load_hint"] = hintStat
	}
	out["target_mod_load_capped"] = hintStat > maxStat && maxStat > 0
	out["pool_hashrate_gh_s"] = poolGHStat
	out["hashrate"] = poolGHStat * 1e9
	out["hashrate_hs"] = poolGHStat * 1e9
	if m.poolGHSmoothed > poolGHStat {
		out["pool_hashrate_gh_s_smoothed"] = m.poolGHSmoothed
	}
	// Per-worker payout summary is public pool transparency; miner UIs need it without admin token.
	// last_client_ip stays admin-only (details=1) — do not expose miner IPs on the public edge.
	workers := make(map[string]workerPayoutStat, len(m.worker))
	for k, v := range m.worker {
		st := v
		online := st.LastSeenUnix > 0 && (now-st.LastSeenUnix) <= poolHashrateStaleSec
		st.Online = online
		if !online {
			st.LastHashrateGHS = 0
		} else {
			st.LastHashrateGHS = effectiveWorkerHashrateGHS(st)
		}
		if !includeDetails {
			st = redactPublicWorkerStat(st)
		}
		base := fleetBaseWorkerID(k)
		var conflict bool
		workers[base], conflict = mergeWorkerStat(workers[base], st)
		if conflict {
			m.fleetAddressConflicts++
		}
	}
	out["workers"] = workers
	// Public miner count must match the merged workers{} map (fleet -gpuN collapse),
	// not raw m.worker len — otherwise dashboard shows N+1 vs site N.
	out["workers_count"] = len(workers)
	onlineMerged := 0
	for _, st := range workers {
		if st.Online {
			onlineMerged++
		}
	}
	out["workers_online"] = onlineMerged
	out["miners"] = onlineMerged
	if includeDetails {
		active := make([]leaseRecord, 0, len(m.active))
		abuse := make(map[string]workerAbuseState, len(m.abuse))
		for _, rec := range m.active {
			active = append(active, rec)
		}
		for k, v := range m.abuse {
			abuse[k] = v
		}
		out["active_leases"] = active
		out["abuse"] = abuse
	}
	return out
}

// redactPublicWorkerStat strips fields that must not appear on unauthenticated pool APIs.
func redactPublicWorkerStat(st workerPayoutStat) workerPayoutStat {
	st.LastClientIP = ""
	return st
}

func parsePayoutLockedReason(reason string) (locked, submitted string) {
	reason = strings.TrimSpace(reason)
	if !strings.HasPrefix(reason, "payout_address_locked") {
		return "", ""
	}
	rest := strings.TrimPrefix(reason, "payout_address_locked")
	rest = strings.TrimPrefix(rest, ":")
	for _, part := range strings.Split(rest, ":") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "locked=") {
			locked = strings.TrimPrefix(part, "locked=")
		}
		if strings.HasPrefix(part, "submitted=") {
			submitted = strings.TrimPrefix(part, "submitted=")
		}
	}
	return locked, submitted
}

// workersByPayoutAddress returns pool workers paying out to the given HMC address (case-insensitive).
// Client IPs are redacted — this route is public.
func (m *workManager) workersByPayoutAddress(addr string) map[string]workerPayoutStat {
	m.mu.Lock()
	defer m.mu.Unlock()
	addr = strings.TrimSpace(addr)
	out := make(map[string]workerPayoutStat)
	if addr == "" {
		return out
	}
	for id, st := range m.worker {
		if strings.EqualFold(strings.TrimSpace(st.PayoutAddress), addr) {
			out[id] = redactPublicWorkerStat(st)
		}
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

// coordinatorWorkPOSTAuthed allows claim/submit with admin or worker-scoped token.
func coordinatorWorkPOSTAuthed(r *http.Request, adminToken, workerToken string, allowInsecure bool) bool {
	if adminToken != "" && coordAdminOK(r, adminToken) {
		return true
	}
	if workerToken != "" && coordAdminOK(r, workerToken) {
		return true
	}
	if adminToken == "" && workerToken == "" {
		return allowInsecure
	}
	return false
}

func addWorkRoutes(mux *http.ServeMux, adminToken, workerToken string, allowInsecure bool, reg *lanpool.Registry, wm *workManager, db *sql.DB) {
	mux.HandleFunc("/api/work/claim", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !coordinatorWorkPOSTAuthed(r, adminToken, workerToken, allowInsecure) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
			http.Error(w, "coordinator authentication required", http.StatusUnauthorized)
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
		if okID, reasonID := wm.checkClaimMinerIdentity(workerID, req.MinerPubKey, req.MinerAddress); !okID {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "worker_id": workerID, "reason": reasonID})
			return
		}
		ipKey := clientIPKey(r)
		now := time.Now().Unix()
		if ok, reason := wm.allowClaimPeer(workerID, ipKey, now); !ok {
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
		if okClaim {
			modePre := wm.schedulerModeNow()
			if modePre == "orders" {
				snap := wm.activeOrderSnapshot()
				if snap.ID != "" && snap.ChainMod > 0 {
					wm.mu.Lock()
					k := workKey{base: base, batch: size}
					if rec, ok := wm.active[k]; ok {
						rec.TargetMod = snap.ChainMod
						wm.active[k] = rec
						mod = snap.ChainMod
					}
					wm.mu.Unlock()
				}
			}
		}
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
		wm.bindClaimPayoutFromPub(workerID, req.MinerPubKey)
		if okCharge, reasonCharge := wm.chargeClaimWorker(workerID, now); !okCharge {
			wm.mu.Lock()
			delete(wm.active, workKey{base: base, batch: size})
			wm.mu.Unlock()
			wm.recordDrop(reasonCharge)
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":        false,
				"worker_id": workerID,
				"reason":    reasonCharge,
			})
			return
		}
		wm.noteWorkerClientIP(workerID, ipKey)
		mode := wm.schedulerModeNow()
		taskClass := "baseline"
		if mode == "orders" {
			taskClass = "orders"
		}
		modOut := mod
		resp := map[string]any{
			"ok":               true,
			"worker_id":        workerID,
			"base_nonce":       base,
			"batch_size":       size,
			"target_mod":       modOut,
			"lease_expires_at": leaseUntil,
			"reissued":         reused,
			"work_id":          buildWorkID(workerID, base, size),
			"chunk_id":         buildChunkID(base, size, modOut),
			"scheduler_mode":   mode,
			"task_class":       taskClass,
		}
		if mode == "orders" {
			snap := wm.activeOrderSnapshot()
			if snap.ID != "" {
				if snap.ChainMod > 0 {
					modOut = snap.ChainMod
					resp["target_mod"] = modOut
					resp["chunk_id"] = buildChunkID(base, size, modOut)
					wm.mu.Lock()
					wk := workKey{base: base, batch: size}
					if rec, ok := wm.active[wk]; ok {
						rec.TargetMod = modOut
						wm.active[wk] = rec
					}
					wm.mu.Unlock()
				}
				resp["order_task_id"] = snap.ID
				resp["order_reward_hmc"] = snap.RewardHMC
				resp["wasm_check_hex"] = snap.WasmHex
			}
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/api/work/submit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !coordinatorWorkPOSTAuthed(r, adminToken, workerToken, allowInsecure) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
			http.Error(w, "coordinator authentication required", http.StatusUnauthorized)
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
		ipKey := clientIPKey(r)
		// Report #28: peer/IP gate only. Worker slot is charged inside submit()
		// after signature + payout-lock identity is proven.
		if ok, reason := wm.allowSubmitPeer(workerID, ipKey, now); !ok {
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
		wm.markSubmitOutcome(workerID, ipKey, reason, now)
		if reason != "" {
			wm.recordDrop(reason)
		}
		// Peer/IP board updates only on accepted work — forgeable declared ids on
		// rejected submits must not overwrite LastClientIP / peer registry.
		if reason == "" {
			_ = reg.Upsert(r.RemoteAddr, lanpool.PushWorkBody{
				WorkerID:      workerID,
				IP:            ipKey,
				HashrateGHS:   req.HashrateGHS,
				ShareAccepted: &accepted,
			})
			wm.noteWorkerClientIP(workerID, ipKey)
			if db != nil {
				if peerFlusher != nil {
					peerFlusher.mark(workerID)
				} else if err := persistPeer(r.Context(), db, workerID, reg); err != nil {
					log.Printf("peer persist %s: %v", logsafe.ID(workerID), err)
				}
			}
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		okResp := true
		if reason != "" {
			w.WriteHeader(submitRejectHTTPStatus(reason))
			okResp = false
		}
		out := map[string]any{
			"ok":             okResp,
			"worker_id":      workerID,
			"accepted":       accepted,
			"reason":         reason,
			"payout_hmc":     payout,
			"signed_submit":  signedSubmit,
			"signer_address": signerAddr,
		}
		if strings.HasPrefix(reason, "payout_address_locked") {
			locked, submittedAddr := parsePayoutLockedReason(reason)
			if locked != "" {
				out["locked_payout_address"] = locked
			}
			if submittedAddr != "" {
				out["submitted_payout_address"] = submittedAddr
			}
		}
		if strings.HasPrefix(reason, "order_chain_solve_failed:") {
			out["order_chain_solve"] = false
			out["order_chain_reason"] = strings.TrimPrefix(reason, "order_chain_solve_failed:")
		} else if accepted && strings.TrimSpace(req.OrderTaskID) != "" && req.WasmGatePass && req.Found {
			out["order_chain_solve"] = true
		}
		_ = json.NewEncoder(w).Encode(out)
	})

	// Remove offline test/stale workers from coordinator memory (dashboard workers list).
	mux.HandleFunc("/api/work/admin/prune-workers", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !coordinatorPOSTAuthed(r, adminToken, allowInsecure) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
			http.Error(w, "admin authentication required", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCoordinatorJSONBodyBytes)
		var req struct {
			Prefix       string  `json:"prefix"`
			MaxPayoutHMC float64 `json:"max_payout_hmc"`
			StaleSec     int64   `json:"stale_sec"`
			DryRun       bool    `json:"dry_run"`
			IgnorePayout bool    `json:"ignore_payout"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.StaleSec <= 0 {
			req.StaleSec = 3600
		}
		if req.MaxPayoutHMC <= 0 {
			req.MaxPayoutHMC = 0.001
		}
		removed, kept := wm.pruneStaleWorkers(req.Prefix, req.MaxPayoutHMC, req.StaleSec, req.DryRun, req.IgnorePayout)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":      true,
			"removed": removed,
			"kept":    kept,
			"dry_run": req.DryRun,
			"prefix":  strings.TrimSpace(req.Prefix),
		})
	})

	mux.HandleFunc("/api/work/admin/clear-abuse", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !coordinatorPOSTAuthed(r, adminToken, allowInsecure) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
			http.Error(w, "admin authentication required", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCoordinatorJSONBodyBytes)
		var req struct {
			WorkerID string `json:"worker_id"`
			IPKey    string `json:"ip_key"`
			All      bool   `json:"all"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		cleared := false
		if req.All {
			wm.mu.Lock()
			if len(wm.abuse) > 0 || len(wm.ipAbuse) > 0 {
				wm.abuse = make(map[string]workerAbuseState)
				wm.ipAbuse = make(map[string]workerAbuseState)
				cleared = true
			}
			wm.mu.Unlock()
		} else {
			cleared = wm.clearWorkerAbuse(req.WorkerID, req.IPKey)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":        true,
			"cleared":   cleared,
			"worker_id": strings.TrimSpace(req.WorkerID),
		})
	})

	mux.HandleFunc("/api/work/admin/revoke-worker", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !coordinatorPOSTAuthed(r, adminToken, allowInsecure) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
			http.Error(w, "admin authentication required", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCoordinatorJSONBodyBytes)
		var req struct {
			WorkerID string `json:"worker_id"`
			IPKey    string `json:"ip_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		out := wm.revokeWorker(req.WorkerID, req.IPKey, true)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(out)
	})

	// Admin recovery for sticky worker_id→payout locks (report #27).
	mux.HandleFunc("/api/work/admin/unbind-payout-lock", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !coordinatorPOSTAuthed(r, adminToken, allowInsecure) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
			http.Error(w, "admin authentication required", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxCoordinatorJSONBodyBytes)
		var req struct {
			WorkerID  string   `json:"worker_id"`
			WorkerIDs []string `json:"worker_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if len(req.WorkerIDs) > 0 {
			if len(req.WorkerIDs) > 256 {
				http.Error(w, "too many worker_ids (max 256)", http.StatusBadRequest)
				return
			}
			ids := make([]string, 0, len(req.WorkerIDs))
			for _, id := range req.WorkerIDs {
				id = strings.TrimSpace(id)
				if id == "" || !validCoordinatorWorkerID(id) {
					http.Error(w, "invalid worker_id in worker_ids", http.StatusBadRequest)
					return
				}
				ids = append(ids, id)
			}
			clearedN := 0
			failed := []map[string]string{}
			for _, id := range ids {
				did, _, uerr := wm.clearPayoutLock(id)
				if uerr != nil {
					failed = append(failed, map[string]string{"worker_id": id, "error": uerr.Error()})
					continue
				}
				if did {
					clearedN++
				}
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			if len(failed) > 0 {
				w.WriteHeader(http.StatusInternalServerError)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":      len(failed) == 0,
				"cleared": clearedN,
				"failed":  failed,
			})
			return
		}
		workerID := strings.TrimSpace(req.WorkerID)
		if workerID == "" || !validCoordinatorWorkerID(workerID) {
			http.Error(w, "invalid worker_id", http.StatusBadRequest)
			return
		}
		cleared, prev, uerr := wm.clearPayoutLock(workerID)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if uerr != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":        false,
				"error":     uerr.Error(),
				"worker_id": workerID,
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":               true,
			"cleared":          cleared,
			"worker_id":        workerID,
			"previous_address": prev,
		})
	})

	mux.HandleFunc("/api/work/admin/memstats", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !coordinatorPOSTAuthed(r, adminToken, allowInsecure) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
			http.Error(w, "admin authentication required", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		out := wm.runtimeMemSnapshot()
		out["ok"] = true
		_ = json.NewEncoder(w).Encode(out)
	})

	mux.HandleFunc("/api/work/admin/gc", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !coordinatorPOSTAuthed(r, adminToken, allowInsecure) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
			http.Error(w, "admin authentication required", http.StatusUnauthorized)
			return
		}
		before := wm.runtimeMemSnapshot()
		runtime.GC()
		runtime.GC()
		after := wm.runtimeMemSnapshot()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"before": before,
			"after":  after,
		})
	})

	mux.HandleFunc("/api/work/by-wallet", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		addr := strings.TrimSpace(r.URL.Query().Get("address"))
		if addr == "" {
			http.Error(w, "address query required (HMC-...)", http.StatusBadRequest)
			return
		}
		if !strings.HasPrefix(addr, "HMC-") || len(addr) > 96 {
			http.Error(w, "invalid HMC address", http.StatusBadRequest)
			return
		}
		workers := wm.workersByPayoutAddress(addr)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":            true,
			"address":       addr,
			"workers_count": len(workers),
			"workers":       workers,
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
			if adminToken != "" && !coordAdminOK(r, adminToken) {
				w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-coordinator"`)
				http.Error(w, "admin authentication required", http.StatusUnauthorized)
				return
			}
			if adminToken == "" && !allowInsecure {
				details = false
			}
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=1")
		out := wm.statsCached(details)
		enrichPoolStatsForPublic(out, reg, wm)
		_ = json.NewEncoder(w).Encode(out)
	})

	// Minimal stats shape for pool listing sites (MiningPoolStats API poll).
	mux.HandleFunc("/api/pool/stats", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		out := wm.statsCached(false)
		enrichPoolStatsForPublic(out, reg, wm)
		hr, wc := poolListingHashrateAndMiners(out)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=1")
		pub := map[string]any{
			"status":   "ok",
			"pool":     "HackMe Official Pool",
			"hashrate": hr,
			"workers":  wc,
			"miners":   wc,
		}
		if tip, ok := wm.liveCanonicalTipHeightFromNode(); ok {
			// Common keys for pool listing pollers (e.g. MiningPoolStats). Not pool-found blocks.
			pub["block_height"] = tip
			pub["tip_height"] = tip
			pub["network_block_height"] = tip
			pub["last_block_height"] = tip
		}
		_ = json.NewEncoder(w).Encode(pub)
	})

	// MiningBoard pool-v1 feed: https://miningboard.com/en/pools/submit
	// Required: coin, pool.hashrate (raw H/s), pool.miners.
	mux.HandleFunc("/api/miningboard/hmc", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		out := wm.statsCached(false)
		enrichPoolStatsForPublic(out, reg, wm)
		hr, wc := poolListingHashrateAndMiners(out)
		pub := map[string]any{
			"spec":       "miningboard-pool-v1",
			"coin":       "HMC",
			"algorithm":  "Useful PoW / PoH (workerpoh)",
			"updated_at": time.Now().UTC().Format(time.RFC3339),
			"pool": map[string]any{
				"hashrate":      hr,
				"miners":        wc,
				"workers":       wc,
				"fee_percent":   0,
				"payout_scheme": "CUSTOM",
				"min_payout":    0.0001,
			},
		}
		if tip, ok := wm.liveCanonicalTipHeightFromNode(); ok {
			pub["network"] = map[string]any{
				"hashrate": hr, // single public pool for this chain
				"height":   tip,
			}
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=30")
		_ = json.NewEncoder(w).Encode(pub)
	})
}

// poolListingHashrateAndMiners extracts pool-global raw H/s and connected miner/worker count
// for public listing pollers (MiningPoolStats, MiningBoard, …).
func poolListingHashrateAndMiners(out map[string]any) (hr float64, wc int) {
	if v, ok := out["hashrate_hs"].(float64); ok {
		hr = v
	}
	if hr <= 0 {
		if gh, ok := out["pool_hashrate_gh_s"].(float64); ok {
			hr = gh * 1e9
		}
	}
	if rigs, ok := out["active_rigs"].([]any); ok && len(rigs) > 0 {
		wc = len(rigs)
	}
	if wc == 0 {
		if n, ok := out["workers_online"].(int); ok && n > 0 {
			wc = n
		} else if n, ok := out["miners"].(int); ok && n > 0 {
			wc = n
		} else if m, ok := out["workers"].(map[string]any); ok {
			wc = len(m)
		} else {
			switch v := out["workers"].(type) {
			case int:
				if v > 0 {
					wc = v
				}
			case float64:
				// JSON numbers decode as float64. Avoid int(float64) (CodeQL
				// go/incorrect-integer-conversion); round-trip via ParseInt bitSize 0 (= int).
				if v >= 1 && v == math.Trunc(v) {
					if n, err := strconv.ParseInt(strconv.FormatFloat(v, 'f', 0, 64), 10, 0); err == nil && n >= 1 {
						wc = int(n)
					}
				}
			}
		}
	}
	return hr, wc
}
