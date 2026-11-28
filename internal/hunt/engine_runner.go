package hunt

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"hackme/internal/fuzzupstream"
	"time"
)

// EngineShardResult is the outcome of one coverage-guided Hunt lease window.
type EngineShardResult struct {
	Engine        string
	EngineVersion string
	WallSec       int
	CorpusDir     string
	CorpusBefore  int
	CorpusAfter   int
	NewCorpus     int
	CrashFiles    []string
	// CorpusCollectRoots bounds ReadRegularCorpusFileUnder for delta uploads (persist + AFL out).
	CorpusCollectRoots []string
	SkippedReason      string
	Err                string
	Ran                bool
	// Execs is libFuzzer stat::number_of_executed_units for this wall
	// (the fork parent's running sum when -fork>1). Zero means the process
	// did not run inputs. It is logged only and does not change pay.
	Execs int
	// Subprocess is true when the shard drove a stdin harness instead of an
	// in-process LLVMFuzzerTestOneInput binary (much lower exec/s).
	Subprocess bool
}

// EngineShardOpts configures a coverage-guided Hunt shard (lease = wall seconds).
type EngineShardOpts struct {
	RepoRoot string
	TargetID string
	Engine   string
	WallSec  int
	// SeedFiles optional seeds copied into the engine corpus before the run.
	SeedFiles [][]byte
	// DictBytes optional AFL/libFuzzer dictionary written beside the corpus (-dict=).
	DictBytes []byte
	// Published harness (inventory / custom) — used as LF stdin driver / AFL target.
	HarnessHash          string
	HarnessFetchURL      string
	HarnessContentSHA256 string
	ArtifactDB           *sql.DB // coordinator prove path; workers fetch via URL
	PinPath              string
	SourceRel            string
	// RngSeed, PullSeeds, and PublishSeeds are forwarded to the libFuzzer session.
	// AFL ignores them. PublishSeeds shares units grown during the wall.
	RngSeed      uint64
	PullSeeds    func(context.Context) ([][]byte, error)
	PublishSeeds func(context.Context, [][]byte) error
}

// LookPathFn is overridable in tests (defaults to exec.LookPath).
var LookPathFn = exec.LookPath

// RunnerAvailable reports whether the local machine can run the engine.
// Byte and coverage engines (libFuzzer/AFL++) are Linux-only so Windows miners
// soft-skip claim instead of advertising clang-only capability without isolate.
func RunnerAvailable(engine string) (ok bool, reason string) {
	engine = ParseHuntEngine(engine)
	switch engine {
	case EngineByte:
		if runtime.GOOS != "linux" {
			return false, "byte_requires_linux_bwrap"
		}
		if !BubblewrapAvailable() || !BubblewrapWorking() {
			return false, "bwrap_unavailable"
		}
		return true, ""
	case EngineLibFuzzer:
		if runtime.GOOS != "linux" {
			return false, "libfuzzer_requires_linux"
		}
		if _, err := LookPathFn("clang"); err != nil {
			return false, "clang_missing"
		}
		return true, ""
	case EngineAFLPP:
		if runtime.GOOS != "linux" {
			return false, "aflpp_requires_linux"
		}
		if _, err := LookAFLFuzz(); err != nil {
			return false, "afl_missing"
		}
		return true, ""
	default:
		return false, "unknown_engine"
	}
}

// RunEngineShard runs libFuzzer or AFL++ for WallSec and returns corpus growth + crash paths.
// Soft-skips (Ran=false) when the runner is missing — Dig/byte paths are untouched.
func RunEngineShard(ctx context.Context, opts EngineShardOpts) EngineShardResult {
	engine := ParseHuntEngine(opts.Engine)
	wall := opts.WallSec
	if wall <= 0 {
		wall = EngineLeaseSecFromConfig(nil, engine)
	}
	wall = clampEngineLeaseSec(wall)
	out := EngineShardResult{Engine: engine, WallSec: wall}
	if !IsCoverageEngine(engine) {
		out.SkippedReason = "byte_engine"
		return out
	}
	if ok, reason := RunnerAvailable(engine); !ok {
		out.SkippedReason = reason
		return out
	}
	repoRoot := strings.TrimSpace(opts.RepoRoot)
	if repoRoot == "" {
		repoRoot = RepoRoot()
	}
	targetID := strings.TrimSpace(opts.TargetID)
	if targetID == "" {
		out.SkippedReason = "no_target"
		return out
	}

	switch engine {
	case EngineLibFuzzer:
		return runLibFuzzerEngineShard(ctx, repoRoot, targetID, wall, opts)
	case EngineAFLPP:
		return runAFLPPEngineShard(ctx, repoRoot, targetID, wall, opts)
	default:
		out.SkippedReason = "unknown_engine"
		return out
	}
}

// libFuzzerDriverPlan chooses the fuzz loop for one lease.
// inventory: the pin source exports LLVMFuzzerTestOneInput.
// published: the materialized ELF exports LLVMFuzzerRunDriver (not the stdin stub).
// catalog: the repo already has tasks/sources/.../<id>_libfuzzer.c (or oss <id>_fuzzer.c).
// stdin: a published ASAN binary and no in-process source (catalog jsmn).
// Stdin is not comparable to one local in-process libFuzzer process.
func libFuzzerDriverPlan(repoRoot, targetID string, inventoryEntry, publishedIsLibFuzzer bool) (inProcess bool, reason string) {
	if inventoryEntry {
		return true, "inventory"
	}
	if publishedIsLibFuzzer {
		return true, "published"
	}
	if DedicatedLibFuzzerHarness(repoRoot, targetID) != "" {
		return true, "catalog"
	}
	// No hand-written harness, but most catalog targets ship a C stdin driver that can
	// be built in-process. This is the gate that kept the pool on the fork wrapper:
	// only 7 of 68 ids have a dedicated harness, so every other lease ran fork+exec
	// per input — two orders of magnitude slower, and with libFuzzer instrumenting the
	// wrapper instead of the target, so the guided fuzzer had no coverage signal at
	// all. Measured on zlib: 92 -> 17,627 exec/s, 7 -> 263 edges. Coverage is 59 of 68
	// ids: three rust targets ship .rs drivers and six have no driver file, and those
	// keep the wrapper, as does a worker that cannot build (no clang, sources, clone).
	if InprocStdinDriverSupported(catalogStdinDriverPath(repoRoot, targetID)) {
		return true, "catalog"
	}
	return false, "stdin"
}

// catalogTargetsByRoot caches the manifest per repo root. Keying matters: a worker and
// a test binary can both resolve drivers under different roots in one process, and a
// single unkeyed cache would hand the first root's driver names to everyone else.
var catalogTargetsByRoot sync.Map // repoRoot -> map[string]fuzzupstream.Target

// catalogStdinDriverPath resolves a catalog target's driver through the manifest, so
// membership is checked (an on-disk <id>_stdin.c for an id the catalog does not define
// must not plan as "catalog" and then fail inside the build) and rust targets keep
// their .rs extension. Resolution goes through fuzzupstream.DriverSourcePath, the same
// function the build side uses — two copies would be free to disagree.
func catalogStdinDriverPath(repoRoot, targetID string) string {
	id, ok := fuzzupstream.SanitizeCatalogID(targetID)
	if !ok {
		return ""
	}
	if repoRoot == "" {
		repoRoot = RepoRoot()
	}
	byID, _ := catalogTargetsByRoot.Load(repoRoot)
	targets, _ := byID.(map[string]fuzzupstream.Target)
	if targets == nil {
		m, err := fuzzupstream.LoadManifest(repoRoot)
		if err != nil {
			return "" // no manifest, no catalog target: the wrapper stays
		}
		targets = make(map[string]fuzzupstream.Target, len(m.Targets))
		for _, t := range m.Targets {
			targets[t.ID] = t
		}
		catalogTargetsByRoot.Store(repoRoot, targets)
	}
	t, found := targets[id]
	if !found {
		return ""
	}
	return fuzzupstream.DriverSourcePath(repoRoot, t)
}

// catalogInProcessBinUsable is true when BuildLibFuzzerImport returned a
// target libFuzzer binary. The generic stdin stub exports LLVMFuzzerRunDriver
// too; it must not be selected as the in-process target.
func catalogInProcessBinUsable(bin, stdin string, err error) bool {
	if err != nil || strings.TrimSpace(bin) == "" || strings.TrimSpace(stdin) != "" {
		return false
	}
	// Regular dedicated binary only. A symlink used to pass the stub check
	// (the read fails closed, and the negation then treated it as in-process).
	return cachedDedicatedLibFuzzerBin(bin)
}

func runLibFuzzerEngineShard(ctx context.Context, repoRoot, targetID string, wall int, opts EngineShardOpts) EngineShardResult {
	out := EngineShardResult{Engine: EngineLibFuzzer, EngineVersion: LibFuzzerVersionHint(), WallSec: wall}
	corpusDir := PersistentLibFuzzerCorpusDir(repoRoot, targetID)
	if err := os.MkdirAll(corpusDir, 0o755); err != nil {
		out.Err = err.Error()
		out.SkippedReason = "corpus_dir"
		return out
	}
	out.CorpusDir = corpusDir
	out.CorpusCollectRoots = []string{corpusDir}
	before, _ := countCorpusFilesUnder(corpusDir, corpusDir)
	out.CorpusBefore = before
	// Content-addressed names so a later shard does not depend on slice index, and
	// so seed_byte_corpus actually occupies files libFuzzer will load (corpus=0
	// when the claim seeds never landed and only "{}" boot starters remained).
	seeded, seedErr := InstallEngineSeedFiles(corpusDir, opts.SeedFiles)
	if seedErr != nil && out.Err == "" {
		out.Err = seedErr.Error()
	}
	if len(opts.SeedFiles) > 0 && seeded == 0 && out.Err == "" {
		out.Err = "hunt: claim seeds produced no corpus files"
	}
	// Defense: empty claim seeds → pull L2 pack cache / tiny boots so TLV targets are not
	// stuck with only "{}" starters on miners that never saw seed_byte_corpus.
	if seeded == 0 {
		if n, _ := countCorpusFilesUnder(corpusDir, corpusDir); n == 0 {
			_ = seedPersistentCorpus(repoRoot, targetID, corpusDir)
		}
	}
	sess := LibFuzzerSessionOpts{
		RepoRoot:        repoRoot,
		TargetID:        targetID,
		WallSec:         wall,
		Persist:         true,
		DictBytes:       opts.DictBytes,
		SkipGenericBoot: len(opts.SeedFiles) > 0,
		RngSeed:         opts.RngSeed,
		PullSeeds:       opts.PullSeeds,
		PublishSeeds:    opts.PublishSeeds,
	}
	// Inventory with in-process LLVMFuzzerTestOneInput (e.g. heavywire fuzz_parse.c).
	if pin := strings.TrimSpace(opts.PinPath); pin != "" {
		if src := strings.TrimSpace(opts.SourceRel); src != "" && InventorySourceHasLibFuzzerEntry(pin, src) {
			lfBin, lerr := BuildInventoryInProcessLibFuzzer(ctx, repoRoot, &RepoPinResult{Path: pin, CommitSHA: "engine-shard"}, src)
			if lerr == nil {
				sess.FuzzerBin = lfBin
				out.Subprocess = false
			}
		}
	}
	// Published harness is still materialized so the content-sha bind fails closed.
	// A stdin ELF is not the fuzz loop when this repo already has LLVMFuzzerTestOneInput
	// for the catalog id: that driver fork/execs every unit (~tens of exec/s) and
	// -fork=2 only doubles the storm. The dedicated binary is in-process (~10^4 exec/s).
	// Build failure falls back to the published stdin driver. jsmn has no dedicated
	// source and stays on that driver.
	if sess.FuzzerBin == "" {
		if hash := strings.TrimSpace(opts.HarnessHash); hash != "" {
			stdinPath, merr := MaterializeHarness(ctx, repoRoot, hash, opts.HarnessFetchURL, opts.HarnessContentSHA256, opts.ArtifactDB)
			if merr != nil {
				out.Err = merr.Error()
				out.SkippedReason = "harness"
				return out
			}
			_, reason := libFuzzerDriverPlan(repoRoot, targetID, false, PublishedHarnessIsLibFuzzer(stdinPath))
			switch reason {
			case "published":
				sess.FuzzerBin = stdinPath
				out.Subprocess = false
			case "catalog":
				// BuildLibFuzzerImport reuses .cache/hunt-lf-import/<id>-libfuzzer-asan.
				// Claim seeds were written above and stay first; this does not reorder them.
				// Clang runs only on a cache miss, on the worker, never on the coordinator.
				if DedicatedLibFuzzerHarness(repoRoot, targetID) != "" {
					bin, stdin, berr := BuildLibFuzzerImport(ctx, repoRoot, targetID)
					if catalogInProcessBinUsable(bin, stdin, berr) {
						sess.FuzzerBin = bin
						out.Subprocess = false
					} else {
						sess.StdinBin = stdinPath
						out.Subprocess = true
					}
					break
				}
				// Targets without a dedicated harness build from their stdin driver, and
				// a cold one costs a git clone plus a clang build. That can outlive both
				// the engine wall and the claim lease, and an expired lease loses the
				// shard's submit and makes the next worker repeat the build. So this
				// lease runs the wrapper and the build happens off the lease; the next
				// shard finds it cached and takes the fast rail.
				if bin, ready := CachedInprocLibFuzzerBin(repoRoot, targetID); ready {
					sess.FuzzerBin = bin
					out.Subprocess = false
				} else {
					startInprocWarmBuild(repoRoot, targetID)
					sess.StdinBin = stdinPath
					out.Subprocess = true
				}
			default:
				sess.StdinBin = stdinPath
				out.Subprocess = true
			}
		}
	}
	started := time.Now()
	n, err := RunLibFuzzerSession(ctx, sess)
	_ = n
	// Stats are written even when the session returns an error after the wall.
	// A missing file stays 0 so a shard that never executed is visible.
	out.Execs = readLibFuzzerSessionExecs(corpusDir)
	elapsed := time.Since(started)
	after, _ := countCorpusFilesUnder(corpusDir, corpusDir)
	out.CorpusAfter = after
	if after > before {
		out.NewCorpus = after - before
	}
	// LF crashes live under corpusDir/.hunt-run (session Dir bind); never parent tree.
	runRoot := filepath.Join(corpusDir, ".hunt-run")
	out.CrashFiles = collectCrashArtifacts(corpusDir, runRoot, 16)
	if err != nil {
		out.Err = err.Error()
	}
	// Soft-skip only on preflight (no clang/build/target/harness) — never after a wall was spent.
	// Empty corpus after LF ran → CLEAN floor submit + re-seed boot starters (avoid lease churn).
	if after == 0 && len(opts.SeedFiles) == 0 {
		_ = seedPersistentCorpus(repoRoot, targetID, corpusDir)
		if after2, _ := countCorpusFilesUnder(corpusDir, corpusDir); after2 > after {
			out.CorpusAfter = after2
		}
	}
	if err != nil && isLibFuzzerPreflightErr(err) {
		// Preflight regardless of claim-seed corpus files (avoid 1ms CLEAN).
		out.SkippedReason = "lf_session"
		out.Ran = false
		return out
	}
	// Must spend most of the lease wall — claim seeds alone must not unlock CLEAN.
	minWall := time.Duration(wall) * time.Second * 3 / 4
	// Match coordinator checkHuntEngineWallFloor: floor is a full MinEngineLeaseSec,
	// not 75% of that minimum (workers were accepting ~11s walls the hub rejects).
	if minWall < time.Duration(MinEngineLeaseSec)*time.Second {
		minWall = time.Duration(MinEngineLeaseSec) * time.Second
	}
	if elapsed < minWall && len(out.CrashFiles) == 0 {
		out.SkippedReason = "lf_wall_short"
		if out.Err == "" {
			out.Err = fmt.Sprintf("elapsed_ms=%d want>=%d", elapsed.Milliseconds(), minWall.Milliseconds())
		} else {
			out.Err = fmt.Sprintf("%s; elapsed_ms=%d want>=%d", out.Err, elapsed.Milliseconds(), minWall.Milliseconds())
		}
		out.Ran = false
		return out
	}
	out.Ran = true
	return out
}

// InstallEngineSeedFiles writes claim/pack seeds into a libFuzzer corpus directory.
// Names are content-addressed so index order cannot drop a seed on a later shard,
// and countCorpusFiles sees them (dotfiles and empty files do not count).
func InstallEngineSeedFiles(corpusDir string, seeds [][]byte) (int, error) {
	if strings.TrimSpace(corpusDir) == "" {
		return 0, fmt.Errorf("hunt: empty corpus dir")
	}
	if err := os.MkdirAll(corpusDir, 0o755); err != nil {
		return 0, err
	}
	seen := map[string]struct{}{}
	n := 0
	for _, b := range seeds {
		if len(b) == 0 || len(b) > 65536 {
			continue
		}
		sum := sha256.Sum256(b)
		key := hex.EncodeToString(sum[:])
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		name := "claim-seed-" + hex.EncodeToString(sum[:8]) + ".bin"
		if err := writeFileBeneath(corpusDir, filepath.Join(corpusDir, name), b, 0o600); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func isLibFuzzerPreflightErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	// Import/empty-corpus after a session is NOT preflight (wall was spent).
	if strings.Contains(msg, "no corpus files") || strings.Contains(msg, "corpus still empty") {
		return false
	}
	for _, needle := range []string{
		"clang required",
		"invalid target",
		"unknown target",
		"libfuzzer import",
		"missing subprocess harness",
		"clang subprocess libfuzzer",
		"build target",
		"manifest",
		"bwrap required", // STRICT default — no bare Hunt; miners without bwrap soft-skip
		"harness artifact",
		"hunt artifact",
		"no coverage harness",
		"inventory harness",
		"lf_wall_short",
		"afl_wall_short",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

func runAFLPPEngineShard(ctx context.Context, repoRoot, targetID string, wall int, opts EngineShardOpts) EngineShardResult {
	out := EngineShardResult{Engine: EngineAFLPP, WallSec: wall}
	afl, err := LookAFLFuzz()
	if err != nil {
		// No afl-fuzz on PATH — honor Deep lease via libFuzzer instead of soft_skip storm.
		lf := runLibFuzzerEngineShard(ctx, repoRoot, targetID, wall, opts)
		lf.Err = joinEngineErr("afl_missing→libfuzzer", err.Error(), lf.Err)
		return lf
	}
	out.EngineVersion = AFLFuzzVersion(afl)

	// Prefer afl-cc instrumented harness. Published LF/ASAN binaries are not AFL-ready —
	// running them under afl-fuzz exits in <1s (wall_short storm on hunt_standard).
	harness, instrumented, err := resolveAFLStdinHarness(ctx, repoRoot, targetID, opts)
	if err != nil {
		lf := runLibFuzzerEngineShard(ctx, repoRoot, targetID, wall, opts)
		lf.Err = joinEngineErr("afl_harness→libfuzzer", err.Error(), lf.Err)
		return lf
	}
	if !instrumented {
		lf := runLibFuzzerEngineShard(ctx, repoRoot, targetID, wall, opts)
		if lf.Err == "" {
			lf.Err = "afl_uninstrumented→libfuzzer"
		} else {
			lf.Err = "afl_uninstrumented→libfuzzer; " + lf.Err
		}
		return lf
	}
	seeds := opts.SeedFiles

	work := filepath.Join(repoRoot, ".cache", "hunt-afl", catalogTargetSegment(targetID))
	inDir := filepath.Join(work, "in")
	outDir := filepath.Join(work, "out")
	_ = os.MkdirAll(work, 0o755)
	_ = mkdirAllBeneath(work, inDir, 0o755)
	_ = os.RemoveAll(outDir) // RemoveAll unlinks symlinks, never follows them
	_ = mkdirAllBeneath(work, outDir, 0o755)

	// Seed from persistent LF corpus + claim seeds.
	persist := PersistentLibFuzzerCorpusDir(repoRoot, targetID)
	// Newest-first warmup so rare/late units are not starved by lexical a*/claim-* bias.
	copyCorpusFilesNewest(persist, persist, inDir, 64)
	for i, b := range seeds {
		if len(b) == 0 || len(b) > 65536 {
			continue
		}
		_ = writeFileBeneath(work, filepath.Join(inDir, fmt.Sprintf("claim-%d.bin", i)), b, 0o600)
	}
	if n, _ := countCorpusFilesUnder(work, inDir); n == 0 {
		_ = writeFileBeneath(work, filepath.Join(inDir, "seed1.json"), []byte("{}"), 0o600)
		_ = writeFileBeneath(work, filepath.Join(inDir, "seed2.bin"), []byte{0x00}, 0o600)
	}
	beforePersist, _ := countCorpusFilesUnder(persist, persist)
	out.CorpusBefore = beforePersist
	out.CorpusDir = persist
	out.CorpusCollectRoots = []string{persist, outDir}

	env := append(os.Environ(),
		"AFL_SKIP_CPUFREQ=1",
		"AFL_I_DONT_CARE_ABOUT_MISSING_CRASHES=1",
		"ASAN_OPTIONS="+LibFuzzerASANOptions,
		"UBSAN_OPTIONS=halt_on_error=1",
	)
	args := []string{"-i", inDir, "-o", outDir, "-V", fmt.Sprint(wall)}
	if len(opts.DictBytes) > 0 {
		dictPath := filepath.Join(work, "afl.dict")
		if err := writeFileBeneath(work, dictPath, opts.DictBytes, 0o600); err == nil {
			args = append(args, "-x", dictPath)
		}
	}
	args = append(args, "--", harness)
	started := time.Now()
	harnessDir := filepath.Dir(harness)
	extraBind := []string{work, inDir, outDir}
	if persist != "" {
		extraBind = append(extraBind, persist)
	}
	runErr := RunIsolated(ctx, IsolateOpts{
		Bin:  afl,
		Args: args,
		Env:  env,
		Dir:  work,
		Wall: time.Duration(wall+30) * time.Second,
		// ASAN targets need huge virtual address space; RLIMIT_AS≈2GiB kills them.
		RSSLimitMB: 0,
		// Match showmap/LF: RO-bind harness dir so STRICT cage can exec the target.
		ExtraBind:   extraBind,
		ExtraRoBind: []string{harnessDir},
	})
	elapsed := time.Since(started)
	if runErr != nil {
		out.Err = runErr.Error()
	}

	// Import queue into persistent corpus only — crash artifacts stay separate
	// (uploaded as crash bytes, never mixed into seed corpus).
	imported := 0
	for _, d := range []string{
		filepath.Join(outDir, "default", "queue"),
		filepath.Join(outDir, "queue"),
	} {
		// Newest-first so late coverage units are not truncated by lexical id order.
		imported += copyCorpusFilesNewest(outDir, d, persist, 256)

	}
	for _, d := range []string{
		filepath.Join(outDir, "default", "crashes"),
		filepath.Join(outDir, "crashes"),
		filepath.Join(outDir, "default", "hangs"),
		filepath.Join(outDir, "hangs"),
	} {
		out.CrashFiles = append(out.CrashFiles, listFiles(outDir, d, 16)...)
	}
	if len(out.CrashFiles) > 32 {
		out.CrashFiles = out.CrashFiles[:32]
	}
	_ = pruneCorpusDirUnder(persist, persist, defaultMaxPersistentCorpus)
	afterPersist, _ := countCorpusFilesUnder(persist, persist)
	out.CorpusAfter = afterPersist
	if afterPersist > beforePersist {
		out.NewCorpus = afterPersist - beforePersist
	} else {
		out.NewCorpus = imported
	}
	// AFL++ often exits non-zero even on a successful -V timer; only trust Ran when
	// the lease wall was mostly spent, or we have crashes / real queue imports.
	// Early "exit status 1" with empty queue must soft_skip — Deep was lying Ran=true.
	minWall := time.Duration(wall) * time.Second * 3 / 4
	if minWall < time.Duration(MinEngineLeaseSec)*time.Second {
		minWall = time.Duration(MinEngineLeaseSec) * time.Second
	}
	produced := len(out.CrashFiles) > 0 || imported > 0 || out.NewCorpus > 0
	if elapsed < minWall && !produced {
		// AFL died early (often non-instrumented / bad argv) — spend remaining wall on LF.
		remain := wall - int(elapsed.Seconds())
		if remain < MinEngineLeaseSec {
			remain = MinEngineLeaseSec
		}
		lf := runLibFuzzerEngineShard(ctx, repoRoot, targetID, remain, opts)
		msg := fmt.Sprintf("afl_wall_short→libfuzzer elapsed_ms=%d want>=%d", elapsed.Milliseconds(), minWall.Milliseconds())
		if out.Err != "" {
			msg = out.Err + "; " + msg
		}
		lf.Err = joinEngineErr(msg, "", lf.Err)
		return lf
	}
	out.Ran = true
	return out
}

func joinEngineErr(prefix, mid, suffix string) string {
	parts := make([]string, 0, 3)
	for _, p := range []string{prefix, mid, suffix} {
		p = strings.TrimSpace(p)
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "; ")
}

// resolveAFLStdinHarness returns an AFL-ready harness path and whether it is afl-cc instrumented.
// instrumented=false means caller should prefer libFuzzer (published LF/ASAN bins abort under afl-fuzz).
func resolveAFLStdinHarness(ctx context.Context, repoRoot, targetID string, opts EngineShardOpts) (string, bool, error) {
	covOpts := CoverageHarnessOpts{
		HarnessHash:          opts.HarnessHash,
		HarnessFetchURL:      opts.HarnessFetchURL,
		HarnessContentSHA256: opts.HarnessContentSHA256,
		ArtifactDB:           opts.ArtifactDB,
		PinPath:              opts.PinPath,
		SourceRel:            opts.SourceRel,
		TemplateAccept:       true,
	}
	// Prefer afl-cc + ASAN instrumented harness (real AFL edges).
	if path, instrumented, err := ResolveCoverageHarness(ctx, repoRoot, targetID, covOpts); err == nil && path != "" {
		if instrumented {
			return path, true, nil
		}
		if aflPath, aerr := BuildAFLInstrumentedHarness(ctx, repoRoot, targetID); aerr == nil && aflPath != "" {
			return aflPath, true, nil
		}
		// Published / ASAN-only — not safe under afl-fuzz.
		return path, false, nil
	}
	if aflPath, aerr := BuildAFLInstrumentedHarness(ctx, repoRoot, targetID); aerr == nil && aflPath != "" {
		return aflPath, true, nil
	}
	_, stdinBin, err := BuildLibFuzzerImport(ctx, repoRoot, targetID)
	if err != nil {
		return "", false, err
	}
	if stdinBin != "" {
		return stdinBin, false, nil
	}
	return "", false, fmt.Errorf("hunt: no AFL stdin harness for %s", targetID)
}

func copyCorpusFiles(srcRoot, src, dst string, max int) int {
	return copyCorpusFilesOrdered(srcRoot, src, dst, max, false)
}

// copyCorpusFilesNewest prefers recently written corpus units (AFL -i warmup).
func copyCorpusFilesNewest(srcRoot, src, dst string, max int) int {
	return copyCorpusFilesOrdered(srcRoot, src, dst, max, true)
}

// rejectSymlinkDir is true when path is missing, a symlink, or not a real directory.
// os.ReadDir follows directory symlinks — callers must Lstat first (exfil defense).
func rejectSymlinkDir(dir string) bool {
	st, err := os.Lstat(dir)
	return err != nil || st.Mode()&os.ModeSymlink != 0 || !st.IsDir()
}

func copyCorpusFilesOrdered(srcRoot, src, dst string, max int, newestFirst bool) int {
	if max < 1 {
		max = 32
	}
	if srcRoot != "" && !corpusPathConfined(srcRoot, src) {
		return 0
	}
	if rejectSymlinkDir(src) {
		return 0
	}
	ents, err := os.ReadDir(src)
	if err != nil {
		return 0
	}
	type cand struct {
		name string
		mod  int64
	}
	cands := make([]cand, 0, len(ents))
	for _, e := range ents {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		srcPath := filepath.Join(src, e.Name())
		// CORPUS-SYMLINK (Hunt): Lstat — never follow harness-planted symlinks
		// into host secrets (.env.vps) via e.Info()/ReadFile.
		info, ok := isRegularCorpusFileUnder(srcRoot, srcPath)
		if !ok || info.Size() <= 0 || info.Size() > 1<<20 {
			continue
		}
		cands = append(cands, cand{name: e.Name(), mod: info.ModTime().UnixNano()})
	}
	if newestFirst {
		sort.Slice(cands, func(i, j int) bool {
			if cands[i].mod == cands[j].mod {
				return cands[i].name < cands[j].name
			}
			return cands[i].mod > cands[j].mod
		})
	} else {
		// Stable lexical order so max-N imports are reproducible across warmups.
		sort.Slice(cands, func(i, j int) bool { return cands[i].name < cands[j].name })
	}
	_ = os.MkdirAll(dst, 0o755)
	n := 0
	for _, c := range cands {
		if n >= max {
			break
		}
		srcPath := filepath.Join(src, c.name)
		name := c.name
		if strings.HasPrefix(filepath.Base(src), "crash") || strings.Contains(src, "crashes") {
			name = "afl-crash-" + name
		} else if !strings.HasPrefix(name, "afl-") {
			name = "afl-" + name
		}
		dest := filepath.Join(dst, name)
		if _, err := os.Lstat(dest); err == nil {
			continue
		}
		if placeCorpusFile(srcRoot, srcPath, dest) {
			n++
		}
	}
	return n
}

// placeCorpusFile hardlinks a regular corpus input when the inode is unchanged,
// otherwise copies bytes from the O_NOFOLLOW fd. link(2) can attach a symlink
// swapped in after the open; that dest is discarded and the original fd is copied.
func placeCorpusFile(srcRoot, srcPath, dest string) bool {
	if srcRoot != "" && !corpusPathConfined(srcRoot, srcPath) {
		return false
	}
	f, err := os.OpenFile(srcPath, os.O_RDONLY|oNoFollow, 0)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 1<<20 {
		return false
	}
	if err := os.Link(srcPath, dest); err == nil {
		dstInfo, lerr := os.Lstat(dest)
		// SameFile checks device+inode. A swapped symlink (or a different
		// regular file) does not match the fd we opened with O_NOFOLLOW.
		if lerr == nil && dstInfo.Mode().IsRegular() && os.SameFile(info, dstInfo) {
			_ = os.Chmod(dest, 0o600)
			return true
		}
		_ = os.Remove(dest)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return false
	}
	b, err := io.ReadAll(io.LimitReader(f, info.Size()))
	if err != nil || int64(len(b)) != info.Size() || len(b) == 0 {
		return false
	}
	return writeFileBeneath(filepath.Dir(dest), dest, b, 0o600) == nil
}

func listFiles(root, dir string, max int) []string {
	if root != "" && !corpusPathConfined(root, dir) {
		return nil
	}
	if rejectSymlinkDir(dir) {
		return nil
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if len(out) >= max {
			break
		}
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || e.Name() == "README.txt" {
			continue
		}
		p := filepath.Join(dir, e.Name())
		// Defense-in-depth: skip symlinks (consumers also ReadRegularCorpusFile).
		if _, ok := isRegularCorpusFileUnder(root, p); !ok {
			continue
		}
		out = append(out, p)
	}
	return out
}

func collectCrashArtifacts(root, dir string, max int) []string {
	if rejectSymlinkDir(dir) {
		return nil
	}
	var out []string
	for _, sub := range []string{"crashes", "hangs", "crash", "artifacts"} {
		subDir := filepath.Join(dir, sub)
		if rejectSymlinkDir(subDir) {
			continue
		}
		out = append(out, listFiles(root, subDir, max)...)
		if len(out) >= max {
			return out[:max]
		}
	}
	return out
}

// resetHuntSessionCrashDir clears corpusDir/.hunt-run/crashes before a libFuzzer
// wall so artifacts from earlier leases cannot be re-collected. Recreates the dir
// beneath root without following symlinks (mkdirAllBeneath).
func resetHuntSessionCrashDir(corpusDir, crashDir string) error {
	// Create .hunt-run first: CorpusPathConfined EvalSymlinks-fails when neither
	// crashes nor its parent exist yet, which soft-skipped fresh corpora
	// (heatshrink/miniz/…) with "crash dir escapes corpus root".
	sessionDir := filepath.Dir(crashDir)
	if err := mkdirAllBeneath(corpusDir, sessionDir, 0o755); err != nil {
		return fmt.Errorf("hunt: create session dir: %w", err)
	}
	if !CorpusPathConfined(corpusDir, crashDir) {
		return fmt.Errorf("hunt: crash dir escapes corpus root")
	}
	if err := os.RemoveAll(crashDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("hunt: clear session crashes: %w", err)
	}
	if err := mkdirAllBeneath(corpusDir, crashDir, 0o755); err != nil {
		return fmt.Errorf("hunt: recreate session crashes: %w", err)
	}
	return nil
}

// ListCrashArtifactPaths returns regular crash/hang artifact paths under dir
// (typically corpus/.hunt-run). Used by research-slot delta upload.
func ListCrashArtifactPaths(dir string, max int) []string {
	return collectCrashArtifacts(dir, dir, max)
}

// CatalogTargetRunsInProcess reports whether a catalog id takes the in-process rail
// rather than the fork wrapper. Exported for the coverage test over the whole catalog.
func CatalogTargetRunsInProcess(repoRoot, targetID string) bool {
	in, _ := libFuzzerDriverPlan(repoRoot, targetID, false, false)
	return in
}

// Warm-up builds run one at a time per worker: a miner box should not host three
// concurrent clang builds, and nothing here may block the lease that triggered it.
var (
	inprocWarmMu    sync.Mutex
	inprocWarmBusy  = map[string]bool{}
	inprocWarmSlots = make(chan struct{}, 1)
)

// startInprocWarmBuild builds a catalog target's in-process binary off the lease.
func startInprocWarmBuild(repoRoot, targetID string) {
	id, ok := fuzzupstream.SanitizeCatalogID(targetID)
	if !ok {
		return
	}
	if repoRoot == "" {
		repoRoot = RepoRoot()
	}
	key := repoRoot + "\x00" + id
	inprocWarmMu.Lock()
	if inprocWarmBusy[key] {
		inprocWarmMu.Unlock()
		return
	}
	inprocWarmBusy[key] = true
	inprocWarmMu.Unlock()

	go func() {
		defer func() {
			inprocWarmMu.Lock()
			delete(inprocWarmBusy, key)
			inprocWarmMu.Unlock()
		}()
		select {
		case inprocWarmSlots <- struct{}{}:
			defer func() { <-inprocWarmSlots }()
		default:
			return // another warm-up holds the slot; a later shard will ask again
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		if _, err := BuildInprocLibFuzzerForTarget(ctx, repoRoot, id); err != nil {
			log.Printf("hunt libfuzzer: in-process warm build %s failed: %v", id, err)
			return
		}
		log.Printf("hunt libfuzzer: in-process binary ready for %s (next shard skips the fork wrapper)", id)
	}()
}

// Test seams for the warm-up guards (the build itself needs clang, a clone and a
// manifest, none of which belong in a unit test).
func StartInprocWarmBuildForTest(repoRoot, targetID string) { startInprocWarmBuild(repoRoot, targetID) }

func InprocWarmBusyCountForTest() int {
	inprocWarmMu.Lock()
	defer inprocWarmMu.Unlock()
	return len(inprocWarmBusy)
}

func InprocWarmSlotsForTest() int { return cap(inprocWarmSlots) }
