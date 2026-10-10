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

// LibFuzzerImportBinPath is the cached libFuzzer binary used for L2 seed sessions.
func LibFuzzerImportBinPath(repoRoot, targetID string) string {
	if repoRoot == "" {
		repoRoot = RepoRoot()
	}
	return filepath.Join(repoRoot, ".cache", "hunt-lf-import", strings.TrimSpace(targetID)+"-libfuzzer-asan")
}

// LibFuzzerImportCorpusDir is the scratch corpus directory for one import session.
func LibFuzzerImportCorpusDir(repoRoot, targetID string) string {
	if repoRoot == "" {
		repoRoot = RepoRoot()
	}
	return filepath.Join(repoRoot, ".cache", "hunt-lf-import", strings.TrimSpace(targetID)+"-corpus")
}

// DedicatedLibFuzzerHarness returns a target-specific libFuzzer harness if present.
func DedicatedLibFuzzerHarness(repoRoot, targetID string) string {
	if repoRoot == "" {
		repoRoot = RepoRoot()
	}
	targetID = strings.TrimSpace(targetID)
	candidates := []string{
		filepath.Join(repoRoot, "tasks", "sources", "fuzz", "benchmark", targetID+"_libfuzzer.c"),
		filepath.Join(repoRoot, "tasks", "sources", "fuzz", "oss", targetID+"_fuzzer.c"),
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
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return "", "", fmt.Errorf("hunt: libfuzzer import: empty target")
	}
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
	if st, err := os.Stat(binPath); err == nil && st.Mode().IsRegular() && st.Size() > 0 {
		if DedicatedLibFuzzerHarness(repoRoot, targetID) != "" {
			return binPath, "", nil
		}
		stdinBin, _, err := fuzzupstream.BuildTarget(ctx, repoRoot, t)
		return binPath, stdinBin, err
	}
	if harness := DedicatedLibFuzzerHarness(repoRoot, targetID); harness != "" {
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
	return filepath.Join(repoRoot, "reports", "oss-cve-libfuzzer", strings.TrimSpace(targetID), "corpus")
}

// RunLibFuzzerImportSession runs libFuzzer for wallSec and imports corpus files into L2 seed cache.
func RunLibFuzzerImportSession(ctx context.Context, repoRoot, targetID string, wallSec int) (int, error) {
	return runLibFuzzerSession(ctx, repoRoot, targetID, wallSec, false)
}

// RunPersistentLibFuzzerSession runs libFuzzer against reports/oss-cve-libfuzzer/<id>/corpus
// (does not wipe), merges into L2 seed cache, and fails if the corpus stays empty after a
// meaningful wall (broken harness detect).
func RunPersistentLibFuzzerSession(ctx context.Context, repoRoot, targetID string, wallSec int) (int, error) {
	return runLibFuzzerSession(ctx, repoRoot, targetID, wallSec, true)
}

func runLibFuzzerSession(ctx context.Context, repoRoot, targetID string, wallSec int, persist bool) (int, error) {
	if wallSec <= 0 {
		wallSec = 120
	}
	binPath, stdinBin, err := BuildLibFuzzerImport(ctx, repoRoot, targetID)
	if err != nil {
		return 0, err
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
		// Keep scratch import dir as a mirror for ImportLibFuzzerCorpusFromSession helpers.
		scratch := LibFuzzerImportCorpusDir(repoRoot, targetID)
		_ = os.RemoveAll(scratch)
		if err := os.MkdirAll(filepath.Dir(scratch), 0o755); err == nil {
			_ = os.Symlink(corpusDir, scratch)
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
	cmd := exec.CommandContext(runCtx, binPath, corpusDir,
		"-max_total_time="+fmt.Sprint(wallSec),
		"-timeout=3",
		"-rss_limit_mb=2048",
		"-max_len=65536",
		"-print_final_stats=1",
	)
	cmd.Env = append(os.Environ(),
		"ASAN_OPTIONS=detect_leaks=1:halt_on_error=1:allocator_may_return_null=1",
		"UBSAN_OPTIONS=halt_on_error=1",
	)
	if stdinBin != "" {
		cmd.Env = append(cmd.Env, "HACKME_LF_STDIN_BIN="+stdinBin)
	}
	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf
	_ = cmd.Run() // sanitizer stop or timeout is ok if corpus grew
	sessionLog := outBuf.String()
	_ = os.WriteFile(filepath.Join(filepath.Dir(corpusDir), "session.stderr"), []byte(sessionLog), 0o644)

	after, _ := countCorpusFiles(corpusDir)
	statsPath := filepath.Join(filepath.Dir(corpusDir), "session_stats.json")
	_ = writeLibFuzzerSessionStats(statsPath, targetID, wallSec, before, after, sessionLog)

	if persist && after == 0 && wallSec >= 90 {
		msg := strings.TrimSpace(sessionLog)
		if len(msg) > 400 {
			msg = msg[len(msg)-400:]
		}
		return 0, fmt.Errorf("hunt: libfuzzer persist: corpus still empty after %ds (broken harness?) %s", wallSec, msg)
	}

	n, err := ImportLibFuzzerCorpusFromSession(repoRoot, targetID)
	if err != nil {
		// Persistent path may already have seeds; try export directly from persist dir.
		if persist {
			seeds, lerr := LoadLibFuzzerSeedFiles(corpusDir, 0)
			if lerr == nil && len(seeds) > 0 {
				return ExportLibFuzzerSeeds(repoRoot, targetID, seeds)
			}
		}
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
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "#") && strings.Contains(line, "cov:") {
			// BEST EFFORT parse "... cov: N ..."
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
	}
	body := fmt.Sprintf("{\n  \"target\": %q,\n  \"wall_sec\": %d,\n  \"corpus_before\": %d,\n  \"corpus_after\": %d,\n  \"features_hint\": %d,\n  \"updated_at\": %q\n}\n",
		targetID, wall, before, after, feat, time.Now().UTC().Format(time.RFC3339))
	return os.WriteFile(path, []byte(body), 0o644)
}

// ImportLibFuzzerCorpusFromSession copies an existing libFuzzer session corpus into L2 seed cache.
func ImportLibFuzzerCorpusFromSession(repoRoot, targetID string) (int, error) {
	corpusDir := LibFuzzerImportCorpusDir(repoRoot, targetID)
	seeds, err := LoadLibFuzzerSeedFiles(corpusDir, 0)
	if err != nil {
		return 0, err
	}
	if len(seeds) == 0 {
		return 0, fmt.Errorf("hunt: libfuzzer import: no corpus files in %s", corpusDir)
	}
	seedDir := LibFuzzerSeedDir(repoRoot, targetID)
	if err := os.RemoveAll(seedDir); err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	return ExportLibFuzzerSeeds(repoRoot, targetID, seeds)
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
	cmd := exec.CommandContext(buildCtx, "clang", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("clang subprocess libfuzzer: %w (%s)", err, strings.TrimSpace(stderr.String()))
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
	cmd := exec.CommandContext(buildCtx, "clang", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("clang dedicated libfuzzer %s: %w (%s)", t.ID, err, strings.TrimSpace(stderr.String()))
	}
	in, err := os.ReadFile(tmpBin)
	if err != nil {
		return err
	}
	return os.WriteFile(outPath, in, 0o755)
}
