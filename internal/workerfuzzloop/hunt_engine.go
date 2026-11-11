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
	SoftSkip      bool
	SkipReason    string
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
	repoRoot := strings.TrimSpace(run.RepoRoot)
	if repoRoot == "" {
		repoRoot = hunt.RepoRoot()
	}
	targetID := strings.TrimSpace(cr.UpstreamTargetID)
	corpusDir := hunt.PersistentLibFuzzerCorpusDir(repoRoot, targetID)
	beforeHashes, _ := snapshotCorpusHashes(corpusDir)
	var dictBytes []byte
	if h := strings.TrimSpace(cr.LibFuzzerDictHex); h != "" {
		if b, err := decodeHex(h); err == nil && len(b) > 0 {
			dictBytes = b
		}
	}
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
	if floor := hunt.MinEngineLeaseSec * 1000 * 3 / 4; minMS < floor {
		minMS = floor
	}
	if out.DurationMS < minMS && len(res.CrashFiles) == 0 {
		out.SoftSkip = true
		out.SkipReason = fmt.Sprintf("lf_wall_short:elapsed_ms=%d want>=%d", out.DurationMS, minMS)
		return out
	}

	// Upload only files new since this lease window (not the whole persistent corpus).
	deltas, crashes := collectEngineDelta(res, beforeHashes, 32)
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
			// Lease already reclaimed (or never held): do not submit — that yields
			// "work item state changed" / lease_not_held churn on the last shard.
			errText := strings.ToLower(upErr.Error())
			if strings.Contains(errText, "requires active lease") ||
				strings.Contains(errText, "lease_not_held") ||
				strings.Contains(errText, "not leased") {
				out.SoftSkip = true
				out.SkipReason = "lease_lost_during_engine"
				return out
			}
		} else if findings > 0 {
			out.CheckResult = 1
			out.Trap = "hunt_crash:engine_corpus_delta"
		}
	}
	return out
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
		b, err := hunt.ReadRegularCorpusFile(p, 65536)
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
		if dup {
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
	for _, p := range res.CrashFiles {
		if len(crashes) >= 8 {
			break
		}
		b, err := hunt.ReadRegularCorpusFile(p, 65536)
		if err != nil || len(b) == 0 {
			continue
		}
		crashes = append(crashes, b)
	}
	return seeds, crashes
}

// collectCoverageEngineSeeds merges claim inputs for LF/AFL leases:
// anchor InputBytesHex + guided CorpusSeeds + campaign seed_byte_corpus (pack TLV seeds).
// Without seed_byte_corpus, inventory packs like heavywire only teach structure on the
// hub while workers start near-empty and miss easy length-confusion bugs.
func collectCoverageEngineSeeds(cr ClaimResp) [][]byte {
	const maxSeed = 65536
	const maxFiles = 64
	seen := map[string]struct{}{}
	out := make([][]byte, 0, 16)
	add := func(b []byte) {
		if len(b) == 0 || len(b) > maxSeed || len(out) >= maxFiles {
			return
		}
		sum := sha256.Sum256(b)
		key := hex.EncodeToString(sum[:])
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, append([]byte(nil), b...))
	}
	if h := strings.TrimSpace(cr.InputBytesHex); h != "" {
		if b, err := decodeHex(h); err == nil {
			add(b)
		}
	}
	for _, m := range cr.CorpusSeeds {
		if b, ok := corpusSeedBytes(m); ok {
			add(b)
		}
	}
	if len(cr.SeedByteCorpus) > 0 {
		for _, b := range fuzzengine.ParseByteCorpus(map[string]any{"seed_byte_corpus": cr.SeedByteCorpus}) {
			add(b)
		}
	}
	return out
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
	return nil, false
}

func decodeHex(h string) ([]byte, error) {
	h = strings.TrimSpace(h)
	if h == "" {
		return nil, fmt.Errorf("empty")
	}
	return hex.DecodeString(h)
}
