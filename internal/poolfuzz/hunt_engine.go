package poolfuzz

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"hackme/internal/fuzzengine"
	"hackme/internal/hunt"
)

// Hunt engine pay kinds (coordinator-proven only — miner edges_touched ignored).
const (
	HuntEnginePayNone  = "none"  // reject / no settle
	HuntEnginePayFloor = "floor" // CLEAN without proven delta — fractional run_floor (default 10% BPS)
	HuntEnginePayEdges = "edges" // proven new coverage edges
	HuntEnginePayCrash = "crash" // ASAN-verified crash
)

// leaseProvenTrack accumulates coordinator-proven novelty while a Hunt lease is held.
type leaseProvenTrack struct {
	SeedsAccepted int
	ProvenEdges   int
	CrashesOK     int
	Engine        string
	EngineVersion string
}

var (
	leaseProven   sync.Map // key campaignID:itemID → *leaseProvenTrack
	leaseProvenMu sync.Mutex
)

func leaseProvenKey(campaignID string, itemID int64) string {
	return fmt.Sprintf("%s:%d", strings.TrimSpace(campaignID), itemID)
}

func noteLeaseProven(campaignID string, itemID int64, seeds, edges, crashes int, engine, version string) {
	key := leaseProvenKey(campaignID, itemID)
	leaseProvenMu.Lock()
	defer leaseProvenMu.Unlock()
	var tr *leaseProvenTrack
	if v, ok := leaseProven.Load(key); ok {
		tr = v.(*leaseProvenTrack)
	} else {
		tr = &leaseProvenTrack{}
		leaseProven.Store(key, tr)
	}
	tr.SeedsAccepted += seeds
	tr.ProvenEdges += edges
	tr.CrashesOK += crashes
	if engine != "" {
		tr.Engine = engine
	}
	if version != "" {
		tr.EngineVersion = version
	}
}

// noteLeaseProvenPersist updates in-memory track and durable fuzz_work_items columns (M3).
func (s *Service) noteLeaseProvenPersist(ctx context.Context, campaignID string, itemID int64, seeds, edges, crashes int, engine, version string) {
	noteLeaseProven(campaignID, itemID, seeds, edges, crashes, engine, version)
	if s == nil || s.DB == nil || itemID <= 0 {
		return
	}
	_, _ = s.DB.ExecContext(ctx, `
		UPDATE fuzz_work_items
		   SET hunt_proven_seeds = hunt_proven_seeds + ?,
		       hunt_proven_edges = hunt_proven_edges + ?,
		       hunt_proven_crashes = hunt_proven_crashes + ?,
		       hunt_proven_engine = CASE WHEN ? != '' THEN ? ELSE hunt_proven_engine END,
		       hunt_proven_engine_ver = CASE WHEN ? != '' THEN ? ELSE hunt_proven_engine_ver END
		 WHERE campaign_id = ? AND id = ?`,
		seeds, edges, crashes, engine, engine, version, version, campaignID, itemID)
}

func takeLeaseProven(campaignID string, itemID int64) leaseProvenTrack {
	key := leaseProvenKey(campaignID, itemID)
	leaseProvenMu.Lock()
	defer leaseProvenMu.Unlock()
	v, ok := leaseProven.LoadAndDelete(key)
	if !ok {
		return leaseProvenTrack{}
	}
	return *v.(*leaseProvenTrack)
}

// takeLeaseProvenPersist merges durable DB counters with in-memory track and clears
// the in-memory map only. DB columns stay as the audit stamp for the finished shard
// (do not zero — empty hunt_proven_* after CLEAN was a reporting hole).
func (s *Service) takeLeaseProvenPersist(ctx context.Context, campaignID string, itemID int64) leaseProvenTrack {
	mem := takeLeaseProven(campaignID, itemID)
	if s == nil || s.DB == nil || itemID <= 0 {
		return mem
	}
	var edges, seeds, crashes int
	var engine, ver string
	_ = s.DB.QueryRowContext(ctx, `
		SELECT hunt_proven_edges, hunt_proven_seeds, hunt_proven_crashes,
		       COALESCE(hunt_proven_engine,''), COALESCE(hunt_proven_engine_ver,'')
		  FROM fuzz_work_items WHERE campaign_id=? AND id=?`,
		campaignID, itemID).Scan(&edges, &seeds, &crashes, &engine, &ver)
	if edges > mem.ProvenEdges {
		mem.ProvenEdges = edges
	}
	if seeds > mem.SeedsAccepted {
		mem.SeedsAccepted = seeds
	}
	if crashes > mem.CrashesOK {
		mem.CrashesOK = crashes
	}
	if mem.Engine == "" {
		mem.Engine = engine
	}
	if mem.EngineVersion == "" {
		mem.EngineVersion = ver
	}
	return mem
}

// stampHuntProvenOnSubmit writes final lease prove fields after engine submit
// (worker hunt_engine / engine_version + coordinator-proven counters).
func (s *Service) stampHuntProvenOnSubmit(ctx context.Context, campaignID string, itemID int64, proven leaseProvenTrack, engine, version string) {
	if s == nil || s.DB == nil || itemID <= 0 {
		return
	}
	eng := strings.TrimSpace(engine)
	if eng == "" {
		eng = proven.Engine
	}
	ver := strings.TrimSpace(version)
	if ver == "" {
		ver = proven.EngineVersion
	}
	_, _ = s.DB.ExecContext(ctx, `
		UPDATE fuzz_work_items
		   SET hunt_proven_edges=?,
		       hunt_proven_seeds=?,
		       hunt_proven_crashes=?,
		       hunt_proven_engine=?,
		       hunt_proven_engine_ver=?
		 WHERE campaign_id=? AND id=?`,
		proven.ProvenEdges, proven.SeedsAccepted, proven.CrashesOK, eng, ver, campaignID, itemID)
}

func peekLeaseProven(campaignID string, itemID int64) leaseProvenTrack {
	key := leaseProvenKey(campaignID, itemID)
	leaseProvenMu.Lock()
	defer leaseProvenMu.Unlock()
	v, ok := leaseProven.Load(key)
	if !ok {
		return leaseProvenTrack{}
	}
	return *v.(*leaseProvenTrack)
}

func clearLeaseProven(campaignID string, itemID int64) {
	leaseProvenMu.Lock()
	defer leaseProvenMu.Unlock()
	leaseProven.Delete(leaseProvenKey(campaignID, itemID))
}

// DecideHuntEnginePay chooses settle posture from coordinator-proven work only.
// Miner-reported edges_touched must not influence this decision.
func DecideHuntEnginePay(provenEdges, seedsAccepted, crashesOK int, minerEdges int, minerEdgesOK bool) (kind string, fullPay bool) {
	_ = minerEdges
	_ = minerEdgesOK // explicitly untrusted
	if crashesOK > 0 {
		return HuntEnginePayCrash, true
	}
	if provenEdges > 0 {
		return HuntEnginePayEdges, true
	}
	if seedsAccepted > 0 && provenEdges == 0 {
		// Seeds accepted but no new edges after coordinator replay — floor.
		return HuntEnginePayFloor, false
	}
	// CLEAN without corpus delta — pennies / floor (no full shard PayRun).
	return HuntEnginePayFloor, false
}

// huntEngineServerWallMS returns milliseconds since the work item was leased
// (updated_at on claim). Fail-closed if the lease row is missing.
func (s *Service) huntEngineServerWallMS(ctx context.Context, campaignID string, itemID int64, now int64) (int64, error) {
	if s == nil || s.DB == nil {
		return 0, fmt.Errorf("poolfuzz: hunt_engine_wall: no db")
	}
	var updatedAt int64
	var status string
	err := s.DB.QueryRowContext(ctx,
		`SELECT status, updated_at FROM fuzz_work_items WHERE campaign_id=? AND id=?`,
		campaignID, itemID).Scan(&status, &updatedAt)
	if err != nil {
		return 0, fmt.Errorf("poolfuzz: hunt_engine_wall: lease lookup: %w", err)
	}
	if strings.TrimSpace(status) != "leased" {
		return 0, fmt.Errorf("poolfuzz: hunt_engine_wall: item not leased (status=%s)", status)
	}
	if updatedAt <= 0 || now < updatedAt {
		return 0, nil
	}
	return (now - updatedAt) * 1000, nil
}

// checkHuntEngineWallFloor rejects coverage-engine CLEAN submits that never spent
// the lease wall. Wall is measured on the coordinator (serverWallMS = now − lease
// updated_at). Miner DurationMS is ignored for the floor (forgeable).
// Escape: HACKME_HUNT_ENGINE_MIN_WALL_MS=0. Default floor ≈ 75% of engine_lease_sec
// (min 15s) so instant claim→submit cannot settle as CLEAN.
func checkHuntEngineWallFloor(req SubmitRequest, cfg map[string]any, serverWallMS int64) error {
	if !huntEngineSubmitMode(req) {
		return nil
	}
	wallMS := serverWallMS
	if wallMS < 0 {
		wallMS = 0
	}
	if v := strings.TrimSpace(os.Getenv("HACKME_HUNT_ENGINE_MIN_WALL_MS")); v != "" {
		if v == "0" || strings.EqualFold(v, "off") || strings.EqualFold(v, "false") {
			return nil
		}
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if wallMS < int64(n) {
				return fmt.Errorf("poolfuzz: hunt_engine_wall_too_short server_wall_ms=%d want>=%d", wallMS, n)
			}
			return nil
		}
	}
	leaseSec := hunt.EngineLeaseSecFromConfig(cfg, req.HuntEngine)
	if leaseSec < hunt.MinEngineLeaseSec {
		leaseSec = hunt.MinEngineLeaseSec
	}
	minMS := int64(leaseSec * 1000 * 3 / 4)
	if minMS < int64(hunt.MinEngineLeaseSec*1000) {
		minMS = int64(hunt.MinEngineLeaseSec * 1000)
	}
	if wallMS < minMS {
		return fmt.Errorf("poolfuzz: hunt_engine_wall_too_short server_wall_ms=%d want>=%d (lease_sec=%d)", wallMS, minMS, leaseSec)
	}
	return nil
}

// HuntEngineEnabled is true unless HACKME_HUNT_COVERAGE_ENGINE=0.
func HuntEngineEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_HUNT_COVERAGE_ENGINE")))
	if v == "" {
		return true
	}
	switch v {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// HuntEngineAllowByteFallback is true when coverage engine soft-skip may fall back to byte Hunt.
// Default OFF — coverage-guided SKU must not silently dilute into mutational Hunt (audit M2).
// Set HACKME_HUNT_ENGINE_BYTE_FALLBACK=1 to re-enable legacy fleets without clang.
func HuntEngineAllowByteFallback() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_HUNT_ENGINE_BYTE_FALLBACK")))
	if v == "" {
		return false
	}
	switch v {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// HuntEngineAllowByteFallbackForConfig denies byte fallback on Deep/Heavy even if env enables it,
// unless HACKME_HUNT_ENGINE_BYTE_FALLBACK_DEEP=1 (escape hatch).
func HuntEngineAllowByteFallbackForConfig(cfg map[string]any) bool {
	if !HuntEngineAllowByteFallback() {
		return false
	}
	if hunt.EngineProfileFromConfig(cfg) == hunt.EngineProfileDeep {
		v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_HUNT_ENGINE_BYTE_FALLBACK_DEEP")))
		return v == "1" || v == "true" || v == "yes" || v == "on"
	}
	return true
}

// WorkerHuntEngines parses claim capability list (comma-separated).
func WorkerHuntEngines(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, p := range parts {
		e := hunt.ParseHuntEngine(p)
		if seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	return out
}

// WorkerSupportsHuntEngine reports whether advertised engines include want.
func WorkerSupportsHuntEngine(advertised []string, want string) bool {
	want = hunt.ParseHuntEngine(want)
	if len(advertised) == 0 {
		// Legacy workers: libfuzzer_oneshot harness implies byte + oneshot ASAN,
		// not necessarily coverage LF shard. Only byte is assumed.
		return want == hunt.EngineByte
	}
	for _, e := range advertised {
		if hunt.ParseHuntEngine(e) == want {
			return true
		}
	}
	return false
}

// ProveHuntCorpusSeedEdges ASAN-oneshots each seed under the campaign Hunt harness
// (content_sha256-bound), then records coverage only from coordinator-derived afl-showmap
// bitmaps. Structural/fingerprint buckets alone never unlock pay (audit H1).
// Miner edges_touched is never consulted.
// Cap: HACKME_HUNT_ENGINE_PROVE_MAX (default 16) — extra seeds still store, may not all pay edges (L1).
func (s *Service) ProveHuntCorpusSeedEdges(ctx context.Context, campaignID string, cfg map[string]any, seeds []fuzzengine.PoolCorpusSeed, now int64) (provenEdges int, err error) {
	if s == nil || !IsHuntCampaign(cfg) {
		return 0, nil
	}
	maxProve := envIntClamped("HACKME_HUNT_ENGINE_PROVE_MAX", 16, 1, 64)
	targetID := strings.TrimSpace(jsonString(cfg["upstream_target_id"]))
	hash := strings.TrimSpace(jsonString(cfg["harness_hash"]))
	covHarness, instrumented, _ := hunt.ResolveCoverageHarness(ctx, hunt.RepoRoot(), targetID, hunt.CoverageHarnessOpts{
		HarnessHash:          hash,
		HarnessFetchURL:      huntHarnessFetchURL(cfg),
		HarnessContentSHA256: huntHarnessContentSHA256(ctx, s, hash, cfg),
		ArtifactDB:           s.DB,
		CoverageHarnessHash:  strings.TrimSpace(jsonString(cfg["coverage_harness_hash"])),
		PinPath:              strings.TrimSpace(jsonString(cfg["hunt_pin_path"])),
		SourceRel:            strings.TrimSpace(jsonString(cfg["hunt_source_rel"])),
		TemplateAccept:       true,
	})
	n := 0
	for _, seed := range seeds {
		if n >= maxProve {
			break
		}
		b := seed.InputBytes
		if len(b) == 0 {
			continue
		}
		n++
		executed, crashed, _, xerr := huntEngineSeedExec(ctx, s, campaignID, cfg, b)
		if xerr != nil {
			// Infra / slot pressure: fail closed for this seed (no novelty pay).
			continue
		}
		if !executed {
			continue
		}
		// Crash novelty is paid via CrashesOK / ASAN finding path — still record
		// coverage for CLEAN executions only so crash uploads cannot double-dip edges.
		if crashed {
			continue
		}
		u := seed.Input
		if u == 0 {
			u = fuzzengine.PackInputBytesToU64(b)
		}
		// H1: require real showmap bitmap from instrumented (or best-effort) harness.
		var edgeBitmap []byte
		if covHarness != "" {
			edgeBitmap, _ = hunt.ShowmapCoverageBitmap(ctx, covHarness, b)
		}
		if !fuzzengine.BitmapHasCoverageSignal(edgeBitmap) {
			// No structural fallback for pay — CLEAN without map → floor.
			_ = instrumented
			continue
		}
		newEdge, _, cerr := s.recordCoverage(ctx, campaignID, cfg, u, b, edgeBitmap, now)
		if cerr != nil {
			return provenEdges, cerr
		}
		if newEdge {
			provenEdges++
		}
	}
	return provenEdges, nil
}

// huntEngineSeedExec runs one seed through the Hunt ASAN harness (ExecPer=1).
// Overridable in tests. Fail closed when HACKME_POOL_HUNT_REPLAY=0.
var huntEngineSeedExec = defaultHuntEngineSeedExec

func defaultHuntEngineSeedExec(ctx context.Context, s *Service, campaignID string, cfg map[string]any, input []byte) (executed, crashed bool, trap string, err error) {
	_ = campaignID
	if !huntReplayEnabled() {
		return false, false, "", fmt.Errorf("poolfuzz: hunt replay required for engine seed prove")
	}
	targetID := strings.TrimSpace(jsonString(cfg["upstream_target_id"]))
	if targetID == "" {
		return false, false, "", fmt.Errorf("poolfuzz: hunt missing upstream_target_id")
	}
	release, err := acquireHuntReplaySlot(ctx)
	if err != nil {
		return false, false, "", err
	}
	defer release()
	maxB := fuzzengine.ParseMaxInputBytes(cfg)
	if maxB <= 0 {
		maxB = 4096
	}
	if len(input) > maxB {
		input = input[:maxB]
	}
	rep, err := hunt.ReplayShard(ctx, hunt.ReplayShardOpts{
		RepoRoot:             hunt.RepoRoot(),
		Spec:                 hunt.HarnessSpecFromConfig(cfg),
		TargetID:             targetID,
		HarnessHash:          strings.TrimSpace(jsonString(cfg["harness_hash"])),
		HarnessFetchURL:      huntHarnessFetchURL(cfg),
		HarnessContentSHA256: huntHarnessContentSHA256(ctx, s, strings.TrimSpace(jsonString(cfg["harness_hash"])), cfg),
		ArtifactDB:           s.DB,
		Input:                input,
		MaxInput:             maxB,
		ExecPer:              1,
		Config:               cfg,
	})
	if err != nil {
		return false, false, "", err
	}
	return true, rep.Crash, rep.Trap, nil
}

// huntEnginePayStats exposed for pool stats / soak.
var (
	huntEnginePayFloorN atomic.Uint64
	huntEnginePayFullN  atomic.Uint64
)

func noteHuntEnginePay(kind string, full bool) {
	if full {
		huntEnginePayFullN.Add(1)
	} else if kind == HuntEnginePayFloor {
		huntEnginePayFloorN.Add(1)
	}
}

// huntEngineFloorPayRunEnabled reports whether CLEAN coverage-engine shards (floor pay)
// enqueue PayRun. Default ON: submit already passed hunt_engine_wall_too_short.
// Opt out: HACKME_HUNT_ENGINE_FLOOR_PAY_RUN=0 (lab / soak without run-pool debit).
func huntEngineFloorPayRunEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_HUNT_ENGINE_FLOOR_PAY_RUN")))
	switch v {
	case "0", "false", "no", "off":
		return false
	case "1", "true", "yes", "on":
		return true
	default:
		return true
	}
}

// HuntEnginePayStats returns counters for ops.
func HuntEnginePayStats() map[string]any {
	return map[string]any{
		"coverage_engine_enabled": HuntEngineEnabled(),
		"byte_fallback":           HuntEngineAllowByteFallback(),
		"pay_floor_n":             huntEnginePayFloorN.Load(),
		"pay_full_n":              huntEnginePayFullN.Load(),
	}
}
