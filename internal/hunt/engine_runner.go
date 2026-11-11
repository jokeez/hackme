package hunt

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
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
	SkippedReason string
	Err           string
	Ran           bool
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

func runLibFuzzerEngineShard(ctx context.Context, repoRoot, targetID string, wall int, opts EngineShardOpts) EngineShardResult {
	out := EngineShardResult{Engine: EngineLibFuzzer, EngineVersion: LibFuzzerVersionHint(), WallSec: wall}
	corpusDir := PersistentLibFuzzerCorpusDir(repoRoot, targetID)
	if err := os.MkdirAll(corpusDir, 0o755); err != nil {
		out.Err = err.Error()
		out.SkippedReason = "corpus_dir"
		return out
	}
	out.CorpusDir = corpusDir
	before, _ := countCorpusFiles(corpusDir)
	out.CorpusBefore = before
	seeded := 0
	for i, b := range opts.SeedFiles {
		if len(b) == 0 || len(b) > 65536 {
			continue
		}
		if err := os.WriteFile(filepath.Join(corpusDir, fmt.Sprintf("claim-seed-%d.bin", i)), b, 0o600); err == nil {
			seeded++
		}
	}
	// Defense: empty claim seeds → pull L2 pack cache / tiny boots so TLV targets are not
	// stuck with only "{}" starters on miners that never saw seed_byte_corpus.
	if seeded == 0 {
		if n, _ := countCorpusFiles(corpusDir); n == 0 {
			_ = seedPersistentCorpus(repoRoot, targetID, corpusDir)
		}
	}
	sess := LibFuzzerSessionOpts{
		RepoRoot:  repoRoot,
		TargetID:  targetID,
		WallSec:   wall,
		Persist:   true,
		DictBytes: opts.DictBytes,
	}
	// Inventory with in-process LLVMFuzzerTestOneInput (e.g. heavywire fuzz_parse.c).
	if pin := strings.TrimSpace(opts.PinPath); pin != "" {
		if src := strings.TrimSpace(opts.SourceRel); src != "" && InventorySourceHasLibFuzzerEntry(pin, src) {
			lfBin, lerr := BuildInventoryInProcessLibFuzzer(ctx, repoRoot, &RepoPinResult{Path: pin, CommitSHA: "engine-shard"}, src)
			if lerr == nil {
				sess.FuzzerBin = lfBin
			}
		}
	}
	// Published stdin ASAN harness + subprocess LF driver when no in-process LF.
	if sess.FuzzerBin == "" {
		if hash := strings.TrimSpace(opts.HarnessHash); hash != "" {
			stdinPath, merr := MaterializeHarness(ctx, repoRoot, hash, opts.HarnessFetchURL, opts.HarnessContentSHA256, opts.ArtifactDB)
			if merr != nil {
				out.Err = merr.Error()
				out.SkippedReason = "harness"
				return out
			}
			sess.StdinBin = stdinPath
		}
	}
	started := time.Now()
	n, err := RunLibFuzzerSession(ctx, sess)
	_ = n
	elapsed := time.Since(started)
	after, _ := countCorpusFiles(corpusDir)
	out.CorpusAfter = after
	if after > before {
		out.NewCorpus = after - before
	}
	out.CrashFiles = collectCrashArtifacts(filepath.Dir(corpusDir), 16)
	if err != nil {
		out.Err = err.Error()
	}
	// Soft-skip only on preflight (no clang/build/target/harness) — never after a wall was spent.
	// Empty corpus after LF ran → CLEAN floor submit + re-seed boot starters (avoid lease churn).
	if after == 0 {
		_ = seedPersistentCorpus(repoRoot, targetID, corpusDir)
		if after2, _ := countCorpusFiles(corpusDir); after2 > after {
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
	if minWall < time.Duration(MinEngineLeaseSec)*time.Second*3/4 {
		minWall = time.Duration(MinEngineLeaseSec) * time.Second * 3 / 4
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
	_ = os.MkdirAll(inDir, 0o755)
	_ = os.RemoveAll(outDir)
	_ = os.MkdirAll(outDir, 0o755)

	// Seed from persistent LF corpus + claim seeds.
	persist := PersistentLibFuzzerCorpusDir(repoRoot, targetID)
	copyCorpusFiles(persist, inDir, 32)
	for i, b := range seeds {
		if len(b) == 0 || len(b) > 65536 {
			continue
		}
		_ = os.WriteFile(filepath.Join(inDir, fmt.Sprintf("claim-%d.bin", i)), b, 0o600)
	}
	if n, _ := countCorpusFiles(inDir); n == 0 {
		_ = os.WriteFile(filepath.Join(inDir, "seed1.json"), []byte("{}"), 0o600)
		_ = os.WriteFile(filepath.Join(inDir, "seed2.bin"), []byte{0x00}, 0o600)
	}
	beforePersist, _ := countCorpusFiles(persist)
	out.CorpusBefore = beforePersist
	out.CorpusDir = persist

	env := append(os.Environ(),
		"AFL_SKIP_CPUFREQ=1",
		"AFL_I_DONT_CARE_ABOUT_MISSING_CRASHES=1",
		"ASAN_OPTIONS=detect_leaks=0:abort_on_error=1:allocator_may_return_null=1",
		"UBSAN_OPTIONS=halt_on_error=1",
	)
	args := []string{"-i", inDir, "-o", outDir, "-V", fmt.Sprint(wall)}
	if len(opts.DictBytes) > 0 {
		dictPath := filepath.Join(work, "afl.dict")
		if err := os.WriteFile(dictPath, opts.DictBytes, 0o600); err == nil {
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
		imported += copyCorpusFiles(d, persist, 64)
	}
	for _, d := range []string{
		filepath.Join(outDir, "default", "crashes"),
		filepath.Join(outDir, "crashes"),
	} {
		out.CrashFiles = append(out.CrashFiles, listFiles(d, 16)...)
	}
	_ = pruneCorpusDir(persist, defaultMaxPersistentCorpus)
	afterPersist, _ := countCorpusFiles(persist)
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
	if minWall < time.Duration(MinEngineLeaseSec)*time.Second*3/4 {
		minWall = time.Duration(MinEngineLeaseSec) * time.Second * 3 / 4
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

func copyCorpusFiles(src, dst string, max int) int {
	if max < 1 {
		max = 32
	}
	ents, err := os.ReadDir(src)
	if err != nil {
		return 0
	}
	// Stable order so max-N imports are reproducible across lease warmups.
	sort.Slice(ents, func(i, j int) bool { return ents[i].Name() < ents[j].Name() })
	_ = os.MkdirAll(dst, 0o755)
	n := 0
	for _, e := range ents {
		if n >= max {
			break
		}
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		srcPath := filepath.Join(src, e.Name())
		info, err := e.Info()
		if err != nil || info.Size() <= 0 || info.Size() > 1<<20 {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(filepath.Base(src), "crash") || strings.Contains(src, "crashes") {
			name = "afl-crash-" + name
		} else if !strings.HasPrefix(name, "afl-") {
			name = "afl-" + name
		}
		dest := filepath.Join(dst, name)
		if _, err := os.Stat(dest); err == nil {
			continue
		}
		if placeCorpusFile(srcPath, dest) {
			n++
		}
	}
	return n
}

// placeCorpusFile prefers a same-FS hardlink (no Read+Write) then falls back to copy.
// Mode is always 0600 so linked files match WriteFile semantics for AFL inputs.
func placeCorpusFile(srcPath, dest string) bool {
	if err := os.Link(srcPath, dest); err == nil {
		_ = os.Chmod(dest, 0o600)
		return true
	}
	b, err := os.ReadFile(srcPath)
	if err != nil || len(b) == 0 {
		return false
	}
	return os.WriteFile(dest, b, 0o600) == nil
}

func listFiles(dir string, max int) []string {
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
		out = append(out, filepath.Join(dir, e.Name()))
	}
	return out
}

func collectCrashArtifacts(dir string, max int) []string {
	var out []string
	for _, sub := range []string{"crashes", "crash", "artifacts"} {
		out = append(out, listFiles(filepath.Join(dir, sub), max)...)
		if len(out) >= max {
			return out[:max]
		}
	}
	return out
}
