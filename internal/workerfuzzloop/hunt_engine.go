package workerfuzzloop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/hunt"
	"hackme/internal/netutil"
	"hackme/internal/poolfuzz"
)

// DetectHuntEngines returns locally available Hunt engines for claim advertising.
// Byte is included only when RunnerAvailable(byte) passes (Linux + working bwrap).
// Adds libfuzzer when clang is present; aflpp when afl-fuzz is bundled.
func DetectHuntEngines() []string {
	out := make([]string, 0, 3)
	if ok, _ := hunt.RunnerAvailable(hunt.EngineByte); ok {
		out = append(out, hunt.EngineByte)
	}
	if ok, _ := hunt.RunnerAvailable(hunt.EngineLibFuzzer); ok {
		out = append(out, hunt.EngineLibFuzzer)
	}
	if ok, _ := hunt.RunnerAvailable(hunt.EngineAFLPP); ok {
		out = append(out, hunt.EngineAFLPP)
	}
	return out
}

// HuntEnginesClaimCSV is the claim capability string.
func HuntEnginesClaimCSV() string {
	return strings.Join(DetectHuntEngines(), ",")
}

// HuntEngineResult is one coverage-guided Hunt shard outcome for submit.
type HuntEngineResult struct {
	CheckResult   int32
	DurationMS    int
	Trap          string
	ExecDone      int
	Engine        string
	EngineVersion string
	CorpusDeltas  int
	Crashes       int
	SeedsIn       int
	CorpusBefore  int
	CorpusAfter   int
	// Execs is stat::number_of_executed_units for this wall (fork parent
	// sum when -fork>1). Zero is a real result and does not change pay.
	Execs      int
	Subprocess bool
	SoftSkip   bool
	SkipReason string
}

// RunHuntEngineShard runs libFuzzer/AFL++ for the claim lease window, uploads corpus_delta,
// and returns a CLEAN (or crash-trap) submit posture. SoftSkip=true → release lease (no submit).
func RunHuntEngineShard(ctx context.Context, run ResearchSlotRun, cr ClaimResp) HuntEngineResult {
	engine := hunt.ParseHuntEngine(cr.HuntEngine)
	out := HuntEngineResult{Engine: engine, ExecDone: 1}
	if !hunt.IsCoverageEngine(engine) {
		out.SoftSkip = true
		out.SkipReason = "not_coverage_engine"
		return out
	}
	if ok, reason := hunt.RunnerAvailable(engine); !ok {
		out.SoftSkip = true
		out.SkipReason = reason
		return out
	}
	wall := cr.EngineLeaseSec
	if wall <= 0 {
		wall = hunt.EngineLeaseSecFromConfig(nil, engine)
	}
	start := time.Now()
	seedFiles := collectCoverageEngineSeeds(cr)
	out.SeedsIn = len(seedFiles)
	// Inventory packs are the bug oracle. Fuzzing {} boot starters and submitting
	// CLEAN hides a missed seed_byte_corpus (rc20 wrap-seed corpus=0).
	if strings.EqualFold(strings.TrimSpace(cr.HuntSource), "inventory") && len(seedFiles) == 0 {
		out.SoftSkip = true
		out.SkipReason = "inventory_seeds_missing"
		return out
	}
	repoRoot := strings.TrimSpace(run.RepoRoot)
	if repoRoot == "" {
		repoRoot = hunt.RepoRoot()
	}
	targetID := strings.TrimSpace(cr.UpstreamTargetID)
	corpusDir := hunt.PersistentLibFuzzerCorpusDir(repoRoot, targetID)
	beforeHashes, _ := snapshotCorpusHashes(corpusDir)
	// Snapshot crash-dir content hashes before the wall (RunEngineShard clears the
	// dir). If clear fails, collectEngineDelta still drops those bytes as stale.
	if crashBefore, err := snapshotHuntCrashHashes(corpusDir); err == nil {
		if beforeHashes == nil {
			beforeHashes = crashBefore
		} else {
			for k, v := range crashBefore {
				beforeHashes[".hunt-crash:"+k] = v
			}
		}
	}
	dictBytes := coverageEngineDict(cr)
	res := hunt.RunEngineShard(ctx, hunt.EngineShardOpts{
		RepoRoot:             repoRoot,
		TargetID:             targetID,
		Engine:               engine,
		WallSec:              wall,
		SeedFiles:            seedFiles,
		DictBytes:            dictBytes,
		HarnessHash:          strings.TrimSpace(cr.HarnessHash),
		HarnessFetchURL:      huntFetchURL(cr),
		HarnessContentSHA256: strings.TrimSpace(cr.HarnessContentSHA256),
		PinPath:              strings.TrimSpace(cr.HuntPinPath),
		SourceRel:            strings.TrimSpace(cr.HuntSourceRel),
	})
	out.EngineVersion = res.EngineVersion
	out.CorpusBefore = res.CorpusBefore
	out.CorpusAfter = res.CorpusAfter
	out.Execs = res.Execs
	out.Subprocess = res.Subprocess
	out.DurationMS = int(time.Since(start).Milliseconds())
	if !res.Ran && len(res.CrashFiles) == 0 {
		out.SoftSkip = true
		out.SkipReason = res.SkippedReason
		if res.Err != "" {
			out.SkipReason = res.SkippedReason + ":" + res.Err
		}
		return out
	}
	// Defense-in-depth: never submit CLEAN when wall clock ≪ lease (stale binaries /
	// race where Ran=true after a sub-second LF exit). Matches engine_runner floor.
	minMS := wall * 1000 * 3 / 4
	// Same floor as coordinator hunt_engine_wall (full MinEngineLeaseSec, not 75% of it).
	if floor := hunt.MinEngineLeaseSec * 1000; minMS < floor {
		minMS = floor
	}
	if out.DurationMS < minMS && len(res.CrashFiles) == 0 {
		out.SoftSkip = true
		out.SkipReason = fmt.Sprintf("lf_wall_short:elapsed_ms=%d want>=%d", out.DurationMS, minMS)
		return out
	}

	// Upload only files new since this lease window (not the whole persistent corpus).
	// 48 (was 32): short pool walls discover more units than one worker can upload
	// at the old cap, so the next miner's seed corpus stays shallow.
	// 96 matches the claim seed-file cap. 64 dropped units a forked wall
	// actually grew, so the next miner restarted shallow.
	deltas, crashes := collectEngineDelta(res, beforeHashes, 96)
	out.CorpusDeltas = len(deltas)
	out.Crashes = len(crashes)
	if len(deltas) > 0 || len(crashes) > 0 {
		ns := poolfuzz.ResearchNamespaceForTarget(cr.UpstreamTargetID)
		cl := run.HTTPClient
		if cl == nil {
			cl = netutil.HTTPClientNoRedirect(HTTPTimeoutFromEnv())
		}
		_, findings, upErr := uploadResearchCorpusDelta(ctx, cl, run.CoordURL, run.Token, run.WorkerID, run.MinerAddr,
			cr.CampaignID, cr.ItemID, ns, deltas, crashes, out.Engine, out.EngineVersion)
		if upErr != nil {
			fmt.Fprintf(os.Stderr, "hunt_engine: corpus_delta: %v\n", upErr)
			// Any corpus_delta failure after wall spent: SoftSkip — otherwise CLEAN
			// submit pays run_floor without corpus/crash evidence (and findings stall).
			errText := strings.ToLower(upErr.Error())
			out.SoftSkip = true
			out.SkipReason = "corpus_delta_upload_failed"
			if strings.Contains(errText, "requires active lease") ||
				strings.Contains(errText, "lease_not_held") ||
				strings.Contains(errText, "not leased") {
				out.SkipReason = "lease_lost_during_engine"
			}
			return out
		} else if findings > 0 {
			out.CheckResult = 1
			out.Trap = "hunt_crash:engine_corpus_delta"
		}
	}
	return out
}

// trivialCorpusUnit is a boot starter. Uploading it burns a delta slot the
// next miner would rather spend on a unit this lease actually discovered.
func trivialCorpusUnit(b []byte) bool {
	switch string(b) {
	case "{}", "[]", "null":
		return true
	}
	return len(b) == 1 && (b[0] == 0x00 || b[0] == 0x80)
}

func corpusReadRoot(roots []string, path string) string {
	for _, r := range roots {
		if r != "" && hunt.CorpusPathConfined(r, path) {
			return r
		}
	}
	return ""
}

func collectEngineDelta(res hunt.EngineShardResult, before map[string]string, maxFiles int) (seeds []fuzzengine.PoolCorpusSeed, crashes [][]byte) {
	if maxFiles < 1 {
		maxFiles = 32
	}
	if before == nil {
		before = map[string]string{}
	}
	dir := res.CorpusDir
	if dir == "" {
		return nil, nil
	}
	roots := res.CorpusCollectRoots
	if len(roots) == 0 {
		roots = []string{dir}
	}
	if !hunt.CorpusPathConfined(dir, dir) {
		return nil, nil
	}
	type fileHit struct {
		name string
		mod  time.Time
		b    []byte
	}
	var hits []fileHit
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	for _, e := range ents {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		// Hunt corpus: never follow symlinks planted in the RW persist bind.
		b, err := hunt.ReadRegularCorpusFileUnder(dir, p, 65536)
		if err != nil || len(b) == 0 {
			continue
		}
		sum := sha256.Sum256(b)
		hexSum := hex.EncodeToString(sum[:])
		if old, ok := before[e.Name()]; ok && old == hexSum {
			continue // unchanged from pre-lease snapshot
		}
		// Also skip if same content existed under another name before the lease.
		dup := false
		for _, h := range before {
			if h == hexSum {
				dup = true
				break
			}
		}
		if dup || trivialCorpusUnit(b) {
			continue
		}
		info, _ := e.Info()
		mod := time.Time{}
		if info != nil {
			mod = info.ModTime()
		}
		hits = append(hits, fileHit{name: e.Name(), mod: mod, b: append([]byte(nil), b...)})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].mod.After(hits[j].mod) })
	if len(hits) > maxFiles {
		hits = hits[:maxFiles]
	}
	for _, h := range hits {
		seeds = append(seeds, fuzzengine.PoolCorpusSeed{
			Input:      fuzzengine.PackInputBytesToU64(h.b),
			InputBytes: h.b,
			Energy:     2, // neutral — coordinator ignores miner energy for Hunt pay (L2)
		})
	}
	seenCrash := map[string]struct{}{}
	for _, p := range res.CrashFiles {
		if len(crashes) >= 8 {
			break
		}
		root := corpusReadRoot(roots, p)
		if root == "" {
			continue
		}
		b, err := hunt.ReadRegularCorpusFileUnder(root, p, 65536)
		if err != nil || len(b) == 0 {
			continue
		}
		sum := sha256.Sum256(b)
		hexSum := hex.EncodeToString(sum[:])
		// Same content-dedupe as seeds: a pre-lease snapshot hash or duplicate
		// in this batch must not re-enter crash credit (stale .hunt-run/crashes).
		if _, ok := seenCrash[hexSum]; ok {
			continue
		}
		stale := false
		for _, h := range before {
			if h == hexSum {
				stale = true
				break
			}
		}
		if stale {
			continue
		}
		seenCrash[hexSum] = struct{}{}
		crashes = append(crashes, b)
	}
	return seeds, crashes
}

// coverageEngineDict prefers the claim's published dict, then a target profile dict.
// Catalog campaigns often have mutator_dict for the byte path and no libfuzzer_dict,
// which left -dict= off and every worker on the generic libFuzzer schedule.
func coverageEngineDict(cr ClaimResp) []byte {
	if h := strings.TrimSpace(cr.LibFuzzerDictHex); h != "" {
		if b, err := decodeHex(h); err == nil && len(b) > 0 {
			return b
		}
	}
	if text := hunt.LibFuzzerDictTextForTarget(cr.UpstreamTargetID); text != "" {
		return []byte(text)
	}
	return nil
}

// collectCoverageEngineSeeds merges claim inputs for LF/AFL leases.
// seed_byte_corpus (pack / imported LF seeds) is reserved first. A fat guided
// snapshot used to fill the 64-file cap and the wrap seed never reached the
// engine, so libFuzzer reported corpus=0 and fuzzed only "{}" boot starters.
func collectCoverageEngineSeeds(cr ClaimResp) [][]byte {
	const maxSeed = 65536
	const maxFiles = 96
	seen := map[string]struct{}{}
	out := make([][]byte, 0, 16)
	add := func(b []byte) bool {
		if len(b) == 0 || len(b) > maxSeed || len(out) >= maxFiles {
			return false
		}
		sum := sha256.Sum256(b)
		key := hex.EncodeToString(sum[:])
		if _, ok := seen[key]; ok {
			return true
		}
		seen[key] = struct{}{}
		out = append(out, append([]byte(nil), b...))
		return true
	}
	if len(cr.SeedByteCorpus) > 0 {
		for _, b := range fuzzengine.ParseByteCorpus(map[string]any{"seed_byte_corpus": cr.SeedByteCorpus}) {
			if !add(b) {
				break
			}
		}
	}
	if h := strings.TrimSpace(cr.InputBytesHex); h != "" {
		if b, err := decodeHex(h); err == nil {
			add(b)
		}
	}
	// Havoc flags are byte-path metadata unless we plant the mutants in the LF corpus.
	appendHavocSeedBurst(out, add, cr)
	for _, m := range cr.CorpusSeeds {
		if b, ok := corpusSeedBytes(m); ok {
			if !add(b) {
				break
			}
		}
	}
	return out
}

// appendHavocSeedBurst adds a few deterministic mutants of the pack head so
// libFuzzer executes havoc_deep_v28/v210 bytes. The original seeds stay first.
func appendHavocSeedBurst(out [][]byte, add func([]byte) bool, cr ClaimResp) {
	if !cr.HavocDeepV28 && !cr.HavocDeepV210 {
		return
	}
	if len(out) == 0 {
		return
	}
	cfg := map[string]any{}
	if cr.HavocDeepV28 {
		cfg["havoc_deep_v28"] = true
	}
	if cr.HavocDeepV210 {
		cfg["havoc_deep_v210"] = true
	}
	if cr.PowerMutCap > 0 {
		cfg["power_mut_cap"] = cr.PowerMutCap
	}
	n := len(out)
	if n > 4 {
		n = 4
	}
	bases := make([][]byte, n)
	copy(bases, out[:n])
	// v210 may grow toward LF -max_len (65536). A 4096 cap never planted the
	// length class the fuzzer is allowed to execute.
	maxLen := 16384
	if cr.HavocDeepV210 {
		maxLen = 65536
	}
	for i, b := range bases {
		mut := fuzzengine.MutateBytesForHunt(b, fuzzengine.StageHavocBase, uint64(i+1)*0x9E3779B97F4A7C15, maxLen, cfg, bases)
		if !add(mut) {
			return
		}
	}
}

func corpusSeedBytes(m map[string]any) ([]byte, bool) {
	if m == nil {
		return nil, false
	}
	if h, ok := m["input_bytes_hex"].(string); ok {
		b, err := decodeHex(h)
		if err == nil && len(b) > 0 {
			return b, true
		}
	}
	switch raw := m["input_bytes"].(type) {
	case []byte:
		if len(raw) > 0 {
			return append([]byte(nil), raw...), true
		}
	case string:
		// Raw corpus bytes. Hex belongs in input_bytes_hex — "HWIR" is valid hex
		// and must not be decoded here.
		if raw != "" {
			return []byte(raw), true
		}
	}
	return nil, false
}

func decodeHex(h string) ([]byte, error) {
	h = strings.TrimSpace(h)
	if h == "" {
		return nil, fmt.Errorf("empty")
	}
	return hex.DecodeString(h)
}
