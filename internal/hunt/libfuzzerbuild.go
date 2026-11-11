package hunt

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"hackme/internal/fuzzupstream"
)

const (
	stdinSubprocessHarness = "tasks/sources/fuzz/benchmark/stdin_subprocess_libfuzzer.c"
)

// catalogTargetSegment returns a single safe path segment for a catalog target id.
// Invalid ids collapse to "_" so helpers never interpolate "../" or separators.
func catalogTargetSegment(targetID string) string {
	if id, ok := fuzzupstream.SanitizeCatalogID(targetID); ok {
		return id
	}
	return "_"
}

// LibFuzzerImportBinPath is the cached libFuzzer binary used for L2 seed sessions.
func LibFuzzerImportBinPath(repoRoot, targetID string) string {
	if repoRoot == "" {
		repoRoot = RepoRoot()
	}
	id := catalogTargetSegment(targetID)
	return filepath.Join(repoRoot, ".cache", "hunt-lf-import", id+"-libfuzzer-asan")
}

// LibFuzzerImportCorpusDir is the scratch corpus directory for one import session.
func LibFuzzerImportCorpusDir(repoRoot, targetID string) string {
	if repoRoot == "" {
		repoRoot = RepoRoot()
	}
	id := catalogTargetSegment(targetID)
	return filepath.Join(repoRoot, ".cache", "hunt-lf-import", id+"-corpus")
}

// isSubprocessStubLibFuzzerBin reports whether bin was built from stdin_subprocess_libfuzzer.c
// (generic stub). Those are not in-process target fuzzers — "hits" are harness noise.
func isSubprocessStubLibFuzzerBin(binPath string) bool {
	raw, err := os.ReadFile(binPath)
	if err != nil || len(raw) == 0 {
		return false
	}
	return bytes.Contains(raw, []byte("stdin_subprocess_libfuzzer.c"))
}

// DedicatedLibFuzzerHarness returns a target-specific libFuzzer harness if present.
func DedicatedLibFuzzerHarness(repoRoot, targetID string) string {
	if repoRoot == "" {
		repoRoot = RepoRoot()
	}
	id, ok := fuzzupstream.SanitizeCatalogID(targetID)
	if !ok {
		return ""
	}
	candidates := []string{
		filepath.Join(repoRoot, "tasks", "sources", "fuzz", "benchmark", id+"_libfuzzer.c"),
		filepath.Join(repoRoot, "tasks", "sources", "fuzz", "oss", id+"_fuzzer.c"),
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			return p
		}
	}
	return ""
}

// BuildLibFuzzerImport compiles (or reuses) a libFuzzer binary for L2 seed generation.
// Returns the fuzzer binary path and stdin driver path (subprocess mode only).
func BuildLibFuzzerImport(ctx context.Context, repoRoot, targetID string) (binPath, stdinBin string, err error) {
	if repoRoot == "" {
		repoRoot = RepoRoot()
	}
	id, ok := fuzzupstream.SanitizeCatalogID(targetID)
	if !ok {
		return "", "", fmt.Errorf("hunt: libfuzzer import: invalid target id %q", targetID)
	}
	targetID = id
	if _, err := exec.LookPath("clang"); err != nil {
		return "", "", fmt.Errorf("hunt: libfuzzer import: clang required")
	}
	binPath = LibFuzzerImportBinPath(repoRoot, targetID)
	manifest, err := fuzzupstream.LoadManifest(repoRoot)
	if err != nil {
		return "", "", err
	}
	t, err := manifest.TargetByID(targetID)
	if err != nil {
		return "", "", err
	}
	harness := DedicatedLibFuzzerHarness(repoRoot, targetID)
	if st, err := os.Stat(binPath); err == nil && st.Mode().IsRegular() && st.Size() > 0 {
		// Dedicated harness: never reuse a cached stdin_subprocess stub under the same name.
		if harness != "" && !isSubprocessStubLibFuzzerBin(binPath) {
			return binPath, "", nil
		}
		if harness == "" {
			stdinBin, _, err := fuzzupstream.BuildTarget(ctx, repoRoot, t)
			return binPath, stdinBin, err
		}
		// fall through → rebuild dedicated over stub cache
	}
	if harness != "" {
		if _, _, err := fuzzupstream.BuildTarget(ctx, repoRoot, t); err != nil {
			return "", "", err
		}
		if err := buildDedicatedLibFuzzer(ctx, repoRoot, t, harness, binPath); err != nil {
			return "", "", err
		}
		return binPath, "", nil
	}
	stdinBin, _, err = fuzzupstream.BuildTarget(ctx, repoRoot, t)
	if err != nil {
		return "", "", err
	}
	if err := buildSubprocessLibFuzzer(ctx, repoRoot, binPath); err != nil {
		return "", "", err
	}
	return binPath, stdinBin, nil
}

// PersistentLibFuzzerCorpusDir is the durable research corpus under reports/.
func PersistentLibFuzzerCorpusDir(repoRoot, targetID string) string {
	if repoRoot == "" {
		repoRoot = RepoRoot()
	}
	return filepath.Join(repoRoot, "reports", "oss-cve-libfuzzer", catalogTargetSegment(targetID), "corpus")
}

// LibFuzzerSessionOpts configures a bounded libFuzzer wall.
// When StdinBin is set (published inventory ASAN harness), only the generic
// subprocess LF driver is built — catalog TargetByID is not required.
type LibFuzzerSessionOpts struct {
	RepoRoot  string
	TargetID  string
	WallSec   int
	Persist   bool
	FuzzerBin string // optional in-process LF binary (inventory LLVMFuzzerTestOneInput)
	StdinBin  string // optional override (inventory / custom)
	DictBytes []byte // optional AFL/libFuzzer dictionary (-dict=)
}

// RunLibFuzzerImportSession runs libFuzzer for wallSec and imports corpus files into L2 seed cache.
func RunLibFuzzerImportSession(ctx context.Context, repoRoot, targetID string, wallSec int) (int, error) {
	return RunLibFuzzerSession(ctx, LibFuzzerSessionOpts{
		RepoRoot: repoRoot, TargetID: targetID, WallSec: wallSec, Persist: false,
	})
}

// RunPersistentLibFuzzerSession runs libFuzzer against reports/oss-cve-libfuzzer/<id>/corpus
// (does not wipe), merges into L2 seed cache, and fails if the corpus stays empty after a
// meaningful wall (broken harness detect).
func RunPersistentLibFuzzerSession(ctx context.Context, repoRoot, targetID string, wallSec int) (int, error) {
	return RunLibFuzzerSession(ctx, LibFuzzerSessionOpts{
		RepoRoot: repoRoot, TargetID: targetID, WallSec: wallSec, Persist: true,
	})
}

// RunLibFuzzerSession runs one LF wall with optional published stdin harness override.
func RunLibFuzzerSession(ctx context.Context, opts LibFuzzerSessionOpts) (int, error) {
	repoRoot := strings.TrimSpace(opts.RepoRoot)
	if repoRoot == "" {
		repoRoot = RepoRoot()
	}
	targetID := strings.TrimSpace(opts.TargetID)
	wallSec := opts.WallSec
	if wallSec <= 0 {
		wallSec = 120
	}
	persist := opts.Persist

	var binPath, stdinBin string
	var err error
	if fb := strings.TrimSpace(opts.FuzzerBin); fb != "" {
		if st, serr := os.Stat(fb); serr != nil || st.IsDir() {
			return 0, fmt.Errorf("hunt: libfuzzer import: fuzzer bin missing: %s", fb)
		}
		binPath = fb
	} else if strings.TrimSpace(opts.StdinBin) != "" {
		stdinBin = strings.TrimSpace(opts.StdinBin)
		if st, serr := os.Stat(stdinBin); serr != nil || st.IsDir() {
			return 0, fmt.Errorf("hunt: libfuzzer import: stdin harness missing: %s", stdinBin)
		}
		binPath, err = BuildLibFuzzerSubprocessDriver(ctx, repoRoot)
		if err != nil {
			return 0, err
		}
	} else {
		binPath, stdinBin, err = BuildLibFuzzerImport(ctx, repoRoot, targetID)
		if err != nil {
			return 0, err
		}
	}
	var corpusDir string
	if persist {
		corpusDir = PersistentLibFuzzerCorpusDir(repoRoot, targetID)
		if err := os.MkdirAll(corpusDir, 0o755); err != nil {
			return 0, err
		}
		// Seed empty persistent corpus from prior L2 cache / tiny defaults.
		if n, _ := countCorpusFiles(corpusDir); n == 0 {
			_ = seedPersistentCorpus(repoRoot, targetID, corpusDir)
		}
		// Keep scratch import dir as a mirror for legacy ImportLibFuzzerCorpusFromSession.
		// Persist import itself reads corpusDir directly (symlink is best-effort).
		scratch := LibFuzzerImportCorpusDir(repoRoot, targetID)
		_ = os.RemoveAll(scratch)
		if err := os.MkdirAll(filepath.Dir(scratch), 0o755); err == nil {
			if err := os.Symlink(corpusDir, scratch); err != nil {
				// Directory left behind / no symlink perms — ignore; persist path is source of truth.
				_ = err
			}
		}
	} else {
		corpusDir = LibFuzzerImportCorpusDir(repoRoot, targetID)
		if err := os.RemoveAll(corpusDir); err != nil && !os.IsNotExist(err) {
			return 0, err
		}
		if err := os.MkdirAll(corpusDir, 0o755); err != nil {
			return 0, err
		}
	}
	before, _ := countCorpusFiles(corpusDir)
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(wallSec+30)*time.Second)
	defer cancel()
	crashDir := filepath.Join(filepath.Dir(corpusDir), "crashes")
	_ = os.MkdirAll(crashDir, 0o755)
	args := []string{
		corpusDir,
		"-max_total_time=" + fmt.Sprint(wallSec),
		"-timeout=3",
		"-rss_limit_mb=2048",
		"-max_len=65536",
		"-artifact_prefix=" + filepath.Join(crashDir, "crash-"),
		"-print_final_stats=1",
	}
	if len(opts.DictBytes) > 0 {
		dictPath := filepath.Join(filepath.Dir(corpusDir), "libfuzzer.dict")
		if err := os.WriteFile(dictPath, opts.DictBytes, 0o600); err != nil {
			return 0, fmt.Errorf("hunt: libfuzzer dict write: %w", err)
		}
		args = append(args, "-dict="+dictPath)
	}
	env := append(os.Environ(),
		"ASAN_OPTIONS=detect_leaks=1:halt_on_error=1:allocator_may_return_null=1",
		"UBSAN_OPTIONS=halt_on_error=1",
	)
	if stdinBin != "" {
		env = append(env, "HACKME_LF_STDIN_BIN="+stdinBin)
	}
	stderrPath := filepath.Join(filepath.Dir(corpusDir), "session.stderr")
	stderrFile, _ := os.Create(stderrPath)
	if stderrFile != nil {
		defer stderrFile.Close()
	}
	extraRo := []string{filepath.Dir(binPath)}
	if stdinBin != "" {
		extraRo = append(extraRo, filepath.Dir(stdinBin))
	}
	// Native LF under the same cage as AFL++ (bwrap when required).
	// Do NOT set RLIMIT_AS for ASAN/libFuzzer — a 2GiB AS cap makes
	// ReserveShadowMemoryRange fail immediately (errno 12) → ~0.2s "walls".
	isoErr := RunIsolated(runCtx, IsolateOpts{
		Bin:         binPath,
		Args:        args,
		Env:         env,
		Dir:         filepath.Dir(corpusDir),
		ExtraBind:   []string{corpusDir},
		ExtraRoBind: extraRo,
		Wall:        time.Duration(wallSec+15) * time.Second,
		RSSLimitMB:  0,
		Stdout:      stderrFile,
		Stderr:      stderrFile,
	})
	sessionLog := ""
	if b, err := os.ReadFile(stderrPath); err == nil {
		sessionLog = string(b)
	}
	if isoErr != nil {
		low := strings.ToLower(isoErr.Error())
		// Only STRICT isolate failures soft-skip; userns/RTM_NEWADDR already bare-retried.
		if strings.Contains(low, "bwrap required") || strings.Contains(low, "isolate_strict") {
			return 0, isoErr
		}
	}

	after, _ := countCorpusFiles(corpusDir)
	// Aggressive dedupe/minimize before L2 import — prevents overnight corpus bloat.
	if after > 8 {
		mergeCtx, mergeCancel := context.WithTimeout(context.Background(), 10*time.Minute)
		mb, ma, merr := MergeMinimizeCorpus(mergeCtx, binPath, corpusDir)
		mergeCancel()
		if merr == nil {
			after = ma
			sessionLog += fmt.Sprintf("\n[merge] before=%d after=%d\n", mb, ma)
		} else {
			sessionLog += fmt.Sprintf("\n[merge] err=%v before=%d after=%d\n", merr, mb, ma)
		}
	}
	statsPath := filepath.Join(filepath.Dir(corpusDir), "session_stats.json")
	_ = writeLibFuzzerSessionStats(statsPath, targetID, wallSec, before, after, sessionLog)

	if persist && after == 0 && wallSec >= 90 {
		msg := strings.TrimSpace(sessionLog)
		if len(msg) > 400 {
			msg = msg[len(msg)-400:]
		}
		return 0, fmt.Errorf("hunt: libfuzzer persist: corpus still empty after %ds (broken harness?) %s", wallSec, msg)
	}

	// Persist sessions write to reports/.../corpus; scratch .cache/...-corpus is only a
	// symlink mirror. Import must read the real persist dir (symlink may be missing).
	if persist {
		seeds, lerr := LoadLibFuzzerSeedFiles(corpusDir, 0)
		if lerr != nil {
			return 0, lerr
		}
		if len(seeds) == 0 {
			if msg := strings.TrimSpace(sessionLog); msg != "" {
				return 0, fmt.Errorf("hunt: libfuzzer persist: no corpus files in %s (%s)", corpusDir, msg)
			}
			return 0, fmt.Errorf("hunt: libfuzzer persist: no corpus files in %s", corpusDir)
		}
		return ExportLibFuzzerSeeds(repoRoot, targetID, seeds)
	}
	n, err := ImportLibFuzzerCorpusFromSession(repoRoot, targetID)
	if err != nil {
		if msg := strings.TrimSpace(sessionLog); msg != "" {
			return 0, fmt.Errorf("%w (%s)", err, msg)
		}
		return 0, err
	}
	return n, nil
}

func countCorpusFiles(dir string) (int, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range ents {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		n++
	}
	return n, nil
}

func seedPersistentCorpus(repoRoot, targetID, corpusDir string) error {
	// Prefer prior L2 seeds; else tiny JSON-ish starters.
	src := LibFuzzerSeedDir(repoRoot, targetID)
	if ents, err := os.ReadDir(src); err == nil {
		for _, e := range ents {
			if e.IsDir() {
				continue
			}
			b, err := os.ReadFile(filepath.Join(src, e.Name()))
			if err != nil || len(b) == 0 {
				continue
			}
			_ = os.WriteFile(filepath.Join(corpusDir, e.Name()), b, 0o600)
		}
	}
	if n, _ := countCorpusFiles(corpusDir); n > 0 {
		return nil
	}
	starters := [][]byte{[]byte("{}"), []byte("[]"), []byte("null"), {0x80}, {0x00}}
	for i, b := range starters {
		_ = os.WriteFile(filepath.Join(corpusDir, fmt.Sprintf("seed-boot-%d.bin", i)), b, 0o600)
	}
	return nil
}

func writeLibFuzzerSessionStats(path, targetID string, wall, before, after int, stderr string) error {
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	feat := 0
	newUnits := 0
	execs := 0
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "#") && strings.Contains(line, "cov:") {
			for _, part := range strings.Fields(line) {
				if strings.HasPrefix(part, "cov:") {
					fmt.Sscanf(strings.TrimPrefix(part, "cov:"), "%d", &feat)
				}
			}
		}
		if strings.Contains(line, "ft:") {
			for _, part := range strings.Fields(line) {
				if strings.HasPrefix(part, "ft:") {
					fmt.Sscanf(strings.TrimPrefix(part, "ft:"), "%d", &feat)
				}
			}
		}
		if strings.HasPrefix(line, "stat::new_units_added:") {
			fmt.Sscanf(strings.TrimSpace(strings.TrimPrefix(line, "stat::new_units_added:")), "%d", &newUnits)
		}
		if strings.HasPrefix(line, "stat::number_of_executed_units:") {
			fmt.Sscanf(strings.TrimSpace(strings.TrimPrefix(line, "stat::number_of_executed_units:")), "%d", &execs)
		}
	}
	if feat == 0 && newUnits > 0 {
		feat = newUnits
	}
	body := fmt.Sprintf("{\n  \"target\": %q,\n  \"wall_sec\": %d,\n  \"corpus_before\": %d,\n  \"corpus_after\": %d,\n  \"features_hint\": %d,\n  \"new_units_added\": %d,\n  \"execs\": %d,\n  \"updated_at\": %q\n}\n",
		targetID, wall, before, after, feat, newUnits, execs, time.Now().UTC().Format(time.RFC3339))
	return os.WriteFile(path, []byte(body), 0o644)
}

// ImportLibFuzzerCorpusFromSession merges an existing libFuzzer session corpus into L2 seed cache.
// Does not wipe the cache — preserves afl-* / msan-* research seeds from other lanes.
func ImportLibFuzzerCorpusFromSession(repoRoot, targetID string) (int, error) {
	corpusDir := LibFuzzerImportCorpusDir(repoRoot, targetID)
	seeds, err := LoadLibFuzzerSeedFiles(corpusDir, 0)
	if err != nil {
		return 0, err
	}
	if len(seeds) == 0 {
		return 0, fmt.Errorf("hunt: libfuzzer import: no corpus files in %s", corpusDir)
	}
	return ExportLibFuzzerSeeds(repoRoot, targetID, seeds)
}

// BuildLibFuzzerSubprocessDriver compiles (or reuses) the generic stdin→LLVMFuzzer subprocess
// libFuzzer binary used with published inventory ASAN harnesses.
func BuildLibFuzzerSubprocessDriver(ctx context.Context, repoRoot string) (string, error) {
	if repoRoot == "" {
		repoRoot = RepoRoot()
	}
	if _, err := exec.LookPath("clang"); err != nil {
		return "", fmt.Errorf("hunt: libfuzzer import: clang required")
	}
	outPath := filepath.Join(repoRoot, ".cache", "hunt-lf-import", "_subprocess-libfuzzer-asan")
	if st, err := os.Stat(outPath); err == nil && st.Mode().IsRegular() && st.Size() > 0 {
		return outPath, nil
	}
	if err := buildSubprocessLibFuzzer(ctx, repoRoot, outPath); err != nil {
		return "", err
	}
	return outPath, nil
}

func buildSubprocessLibFuzzer(ctx context.Context, repoRoot, outPath string) error {
	harness := filepath.Join(repoRoot, stdinSubprocessHarness)
	if st, err := os.Stat(harness); err != nil || !st.Mode().IsRegular() {
		return fmt.Errorf("hunt: missing subprocess harness %s", harness)
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	tmpDir, err := os.MkdirTemp("", "hunt-lf-build-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	tmpBin := filepath.Join(tmpDir, "fuzzer")
	args := []string{
		"-fsanitize=fuzzer,address,undefined",
		"-fno-omit-frame-pointer", "-g", "-O1",
		"-o", tmpBin,
		harness,
	}
	buildCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	stderr, err := runCompilerBuild(buildCtx, "clang", args, []string{tmpDir, filepath.Dir(harness)}, 5*time.Minute)
	if err != nil {
		return fmt.Errorf("clang subprocess libfuzzer: %w (%s)", err, strings.TrimSpace(stderr))
	}
	in, err := os.ReadFile(tmpBin)
	if err != nil {
		return err
	}
	return os.WriteFile(outPath, in, 0o755)
}

func buildDedicatedLibFuzzer(ctx context.Context, repoRoot string, t fuzzupstream.Target, harness, outPath string) error {
	clonePath := filepath.Join(repoRoot, ".cache", "oss-cve-clones", t.ID)
	if err := fuzzupstream.InjectOSSCveBuildStubs(repoRoot, t.ID, clonePath); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	tmpDir, err := os.MkdirTemp("", "hunt-lf-build-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	tmpBin := filepath.Join(tmpDir, "fuzzer")
	args := []string{
		"-fsanitize=fuzzer,address,undefined",
		"-fno-omit-frame-pointer", "-g", "-O1",
		"-I", clonePath,
		"-o", tmpBin,
		harness,
	}
	switch t.ID {
	case "cjson":
		args = append(args, filepath.Join(clonePath, "cJSON.c"))
	case "libucl":
		for _, f := range []string{
			"src/ucl_parser.c", "src/ucl_util.c", "src/ucl_hash.c", "src/ucl_schema.c",
			"src/ucl_emitter.c", "src/ucl_emitter_utils.c", "src/ucl_msgpack.c", "src/ucl_sexp.c",
		} {
			args = append(args, filepath.Join(clonePath, f))
		}
		args = append(args,
			"-I"+filepath.Join(clonePath, "include"),
			"-I"+filepath.Join(clonePath, "uthash"),
			"-I"+filepath.Join(clonePath, "klib"),
			"-I"+filepath.Join(clonePath, "src"),
			"-w",
		)
	default:
		for _, inc := range t.IncludeDirs {
			args = append(args, "-I"+fuzzupstream.IncludeDir(repoRoot, clonePath, inc))
		}
		for _, src := range fuzzupstream.ExpandUpstreamSrc(clonePath, t.UpstreamSrc) {
			args = append(args, src)
		}
		for _, flag := range t.BuildFlags {
			args = append(args, flag)
		}
	}
	buildCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	binds := []string{tmpDir, clonePath, filepath.Dir(harness)}
	for _, inc := range t.IncludeDirs {
		binds = append(binds, fuzzupstream.IncludeDir(repoRoot, clonePath, inc))
	}
	stderr, err := runCompilerBuild(buildCtx, "clang", args, binds, 10*time.Minute)
	if err != nil {
		return fmt.Errorf("clang dedicated libfuzzer %s: %w (%s)", t.ID, err, strings.TrimSpace(stderr))
	}
	in, err := os.ReadFile(tmpBin)
	if err != nil {
		return err
	}
	return os.WriteFile(outPath, in, 0o755)
}
