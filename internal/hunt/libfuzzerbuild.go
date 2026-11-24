package hunt

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"hackme/internal/fuzzupstream"
	"hackme/internal/logsafe"
)

const (
	stdinSubprocessHarness = "tasks/sources/fuzz/benchmark/stdin_subprocess_libfuzzer.c"
)

// LibFuzzerASANOptions is the coverage-engine sanitizer env.
// abort_on_error+symbolize=0: ASAN must exit so libFuzzer writes crash-* artifacts.
// symbolize=1 (and no abort) hangs some inventory harnesses after the ERROR line
// → wall timeout → crashes=0 → no Hunt bounty.
const LibFuzzerASANOptions = "detect_leaks=0:abort_on_error=1:halt_on_error=1:allocator_may_return_null=1:symbolize=0"

// leakDetectWallSec is the wall from which a session also hunts leaks. Measured on
// the selftest target (20s walls, three seeds): with no leak present the throughput
// median is unchanged, and on the run that did hit one the cost was about a third of
// the execs — for a whole bug class that is otherwise invisible. Short walls keep it
// off so the merge window stays intact, which is why it was disabled everywhere.
const leakDetectWallSec = 180

// LibFuzzerASANOptionsForWall is the session sanitizer env for a given wall.
func LibFuzzerASANOptionsForWall(wallSec int) string {
	if wallSec >= leakDetectWallSec {
		return strings.Replace(LibFuzzerASANOptions, "detect_leaks=0", "detect_leaks=1", 1)
	}
	return LibFuzzerASANOptions
}

// ShowmapASANOptions keeps afl-showmap alive through a crashing input so the
// edge map is written. Session libFuzzer stays abort_on_error=1.
const ShowmapASANOptions = "detect_leaks=0:halt_on_error=0:allocator_may_return_null=1:abort_on_error=0:symbolize=0"

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

// isSubprocessStubLibFuzzerBin reports whether bin was built from the generic
// stdin driver. Those are not in-process target fuzzers — "hits" are harness noise.
// The .c filename is only a comment plus DWARF, so a -g0 or stripped driver does
// not contain it. The getenv key is in .rodata either way.
func isSubprocessStubLibFuzzerBin(binPath string) bool {
	// O_NOFOLLOW: never follow a harness-planted symlink over the cache bin.
	raw, err := ReadRegularCorpusFile(binPath, 64<<20)
	if err != nil || len(raw) == 0 {
		return false
	}
	if bytes.Contains(raw, []byte("stdin_subprocess_libfuzzer.c")) {
		return true
	}
	return bytes.Contains(raw, []byte("HACKME_LF_STDIN_BIN"))
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

const (
	libFuzzerBinKindDedicated = "dedicated"
	libFuzzerBinKindStub      = "stub"
)

var libFuzzerImportMu sync.Map

func lockLibFuzzerImport(binPath string) func() {
	v, _ := libFuzzerImportMu.LoadOrStore(binPath, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func libFuzzerBinKindPath(binPath string) string {
	return binPath + ".kind"
}

func readLibFuzzerBinKind(binPath string) string {
	b, err := os.ReadFile(libFuzzerBinKindPath(binPath))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func writeLibFuzzerBinKind(binPath, kind string) error {
	return writeFileBeneath(filepath.Dir(binPath), libFuzzerBinKindPath(binPath), []byte(kind+"\n"), 0o644)
}

// cachedDedicatedLibFuzzerBin is a reusable in-process catalog binary.
// The sidecar avoids re-reading the ELF on every shard. A symlink is never a hit,
// so a planted link cannot be executed as the fuzzer or skip clang into a stub.
func cachedDedicatedLibFuzzerBin(binPath string) bool {
	st, err := os.Lstat(binPath)
	if err != nil || !st.Mode().IsRegular() || st.Size() <= 0 {
		return false
	}
	switch readLibFuzzerBinKind(binPath) {
	case libFuzzerBinKindDedicated:
		return true
	case libFuzzerBinKindStub:
		return false
	}
	if isSubprocessStubLibFuzzerBin(binPath) {
		_ = writeLibFuzzerBinKind(binPath, libFuzzerBinKindStub)
		return false
	}
	_ = writeLibFuzzerBinKind(binPath, libFuzzerBinKindDedicated)
	return true
}

// BuildLibFuzzerImport compiles (or reuses) a libFuzzer binary for L2 seed generation.
// Returns the fuzzer binary path and stdin driver path (subprocess mode only).
// A dedicated catalog binary is reused across shards with no clang and no manifest
// load. The worker still installs claim seeds before this binary is exec'd.
func BuildLibFuzzerImport(ctx context.Context, repoRoot, targetID string) (binPath, stdinBin string, err error) {
	if repoRoot == "" {
		repoRoot = RepoRoot()
	}
	id, ok := fuzzupstream.SanitizeCatalogID(targetID)
	if !ok {
		return "", "", fmt.Errorf("hunt: libfuzzer import: invalid target id %q", targetID)
	}
	targetID = id
	binPath = LibFuzzerImportBinPath(repoRoot, targetID)
	harness := DedicatedLibFuzzerHarness(repoRoot, targetID)
	// Fast path: later shards must not LookPath("clang") or parse the manifest.
	if harness != "" && cachedDedicatedLibFuzzerBin(binPath) {
		return binPath, "", nil
	}
	unlock := lockLibFuzzerImport(binPath)
	defer unlock()
	if harness != "" && cachedDedicatedLibFuzzerBin(binPath) {
		return binPath, "", nil
	}
	if _, err := exec.LookPath("clang"); err != nil {
		return "", "", fmt.Errorf("hunt: libfuzzer import: clang required")
	}
	manifest, err := fuzzupstream.LoadManifest(repoRoot)
	if err != nil {
		return "", "", err
	}
	t, err := manifest.TargetByID(targetID)
	if err != nil {
		return "", "", err
	}
	if st, err := os.Lstat(binPath); err == nil && st.Mode().IsRegular() && st.Size() > 0 && harness == "" {
		// An in-process build needs no stdin binary at all; the fork wrapper does.
		if inprocMarkerPresent(binPath) {
			return binPath, "", nil
		}
		// A cached fork wrapper is not worth keeping when the driver can run
		// in-process: the cache would otherwise pin every future shard to 97 exec/s
		// of blind fuzzing until someone cleared it by hand. Rebuild once, then the
		// marker takes the fast path above.
		if driver := fuzzupstream.DriverSourcePath(repoRoot, t); !InprocStdinDriverSupported(driver) {
			// Stdin driver cache. BuildTarget stats its own ASAN binary before clang.
			stdinBin, _, err := fuzzupstream.BuildTarget(ctx, repoRoot, t)
			return binPath, stdinBin, err
		}
	}
	if harness != "" {
		// Sources only. BuildTarget would also clang the unused stdin driver.
		if _, err := fuzzupstream.EnsureClone(ctx, repoRoot, t); err != nil {
			return "", "", err
		}
		if err := buildDedicatedLibFuzzer(ctx, repoRoot, t, harness, binPath); err != nil {
			return "", "", err
		}
		return binPath, "", nil
	}
	// No hand-written harness: build the catalog's own stdin driver in-process rather
	// than fork+exec it per input. The wrapper stays as the fallback for a driver that
	// cannot run in-process and for published inventory binaries without sources.
	if driver := fuzzupstream.DriverSourcePath(repoRoot, t); InprocStdinDriverSupported(driver) {
		if _, cerr := fuzzupstream.EnsureClone(ctx, repoRoot, t); cerr == nil {
			if berr := buildInprocStdinLibFuzzer(ctx, repoRoot, t, driver, binPath); berr == nil {
				return binPath, "", nil
			} else {
				log.Printf("hunt libfuzzer: in-process build failed for %s, falling back to the fork wrapper: %s",
					logsafe.ID(t.ID), logsafe.Err(berr))
			}
		}
	}
	stdinBin, _, err = fuzzupstream.BuildTarget(ctx, repoRoot, t)
	if err != nil {
		return "", "", err
	}
	if err := buildSubprocessLibFuzzer(ctx, repoRoot, binPath); err != nil {
		return "", "", err
	}
	// Drop a stale in-process marker so the wrapper is not mistaken for one later.
	_ = os.Remove(inprocMarkerPath(binPath))
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
	// SkipGenericBoot refuses {} / [] / null starters when the caller already
	// supplied claim seeds. Planting those hid a missed seed_byte_corpus.
	SkipGenericBoot bool
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
		if n, _ := countCorpusFiles(corpusDir); n == 0 && !opts.SkipGenericBoot {
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
	// Session artifacts live under corpusDir/.hunt-run (dot-dir skipped by
	// countCorpusFiles). Dir binds corpusDir only — never the parent
	// hunt-lf-import / report tree (sibling cache bins + session.stderr).
	sessionDir := filepath.Join(corpusDir, ".hunt-run")
	crashDir := filepath.Join(sessionDir, "crashes")
	// Lease-scoped crash dir: wipe leftovers from prior shards so collectCrashArtifacts
	// cannot re-harvest a stale crash into a CLEAN shard's PayRun (Dicky Mulia Fiqri /
	// 2026-11-14). RemoveAll unlinks a planted symlink without following it.
	if err := resetHuntSessionCrashDir(corpusDir, crashDir); err != nil {
		return 0, err
	}
	// The profile dict is keyed off the target id, so an unrecognised catalog id or a
	// customer repo would run on eleven generic boundary tokens. The harness binary
	// carries the target's own vocabulary in its read-only data; lifting it costs one
	// file read and measurably changes what the wall reaches. The target's own tokens
	// also give the stdin driver something to work with, where the dict is all there is.
	dictBytes := opts.DictBytes
	dictSource := "profile"
	harnessForDict := stdinBin
	if harnessForDict == "" {
		harnessForDict = binPath
	}
	// Sources first when the catalog clone is on disk: the target's own literals with
	// none of the sanitizer runtime's tables mixed in. The binary scan is the fallback
	// for a published harness that arrives without sources.
	derived := DeriveDictTokensFromSources(fuzzupstream.CloneDirForTarget(repoRoot, targetID), 0)
	derivedFrom := "sources"
	if len(derived) == 0 {
		derived = DeriveDictTokensFromBinary(harnessForDict, 0)
		derivedFrom = "binary"
	}
	if len(derived) > 0 {
		dictBytes = AugmentLibFuzzerDict(dictBytes, derived)
		if len(opts.DictBytes) == 0 {
			dictSource = derivedFrom
		} else {
			dictSource = "profile+" + derivedFrom
		}
	}
	dictPath := ""
	if len(dictBytes) > 0 {
		dictPath = filepath.Join(sessionDir, "libfuzzer.dict")
		if err := writeFileBeneath(corpusDir, dictPath, dictBytes, 0o600); err != nil {
			return 0, fmt.Errorf("hunt: libfuzzer dict write: %w", err)
		}
		log.Printf("hunt libfuzzer: dict %s (%d bytes) for %s", dictSource, len(dictBytes), logsafe.ID(targetID))
	}
	// -fork runs extra libFuzzer jobs in this process. In-process
	// LLVMFuzzer binaries keep the full schedule (3 at >=300s). The stdin
	// driver fork/execs the published harness on every unit, so each extra
	// job multiplies processes; cap that driver at 2 even on a heavy wall.
	forkJobs := libFuzzerSessionForkJobs(wallSec, stdinBin != "")
	args := libFuzzerSessionArgs(corpusDir, filepath.Join(crashDir, "crash-"), wallSec, dictPath, forkJobs)
	env := append(os.Environ(),
		"ASAN_OPTIONS="+LibFuzzerASANOptionsForWall(wallSec),
		"UBSAN_OPTIONS=halt_on_error=1:print_stacktrace=0",
	)
	if stdinBin != "" {
		env = append(env, "HACKME_LF_STDIN_BIN="+stdinBin)
	}
	stderrPath := filepath.Join(sessionDir, "session.stderr")
	stderrFile, err := createFileBeneath(corpusDir, stderrPath, 0o600)
	if err != nil {
		return 0, fmt.Errorf("hunt: libfuzzer stderr create: %w", err)
	}
	defer stderrFile.Close()
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
		Dir:         corpusDir,
		ExtraBind:   []string{corpusDir},
		ExtraRoBind: extraRo,
		Wall:        time.Duration(wallSec+15) * time.Second,
		RSSLimitMB:  0,
		Stdout:      stderrFile,
		Stderr:      stderrFile,
	})
	// Read the still-open fd — never re-open session.stderr by path (harness can
	// unlink+symlink to host secrets inside the RW corpus Dir bind).
	sessionLog := readOpenSessionLog(stderrFile)
	if isoErr != nil {
		low := strings.ToLower(isoErr.Error())
		// Only STRICT isolate failures soft-skip; userns/RTM_NEWADDR already bare-retried.
		if strings.Contains(low, "bwrap required") || strings.Contains(low, "isolate_strict") {
			return 0, isoErr
		}
	}

	after, _ := countCorpusFiles(corpusDir)
	if after > 8 && blindFuzzerRail(stdinBin) {
		// -merge=1 keeps the inputs that add coverage *as the given binary measures
		// it*. The fork wrapper is instrumented, the target it execs is not, so the
		// wrapper's own handful of edges is the whole signal and nearly every input
		// looks redundant: observed on the hub as corpus_before=97 corpus_after=1,
		// every lease, wiping the corpus the pool had collectively built.
		sessionLog += "\n[merge] skipped: fork-wrapper session has no target coverage to minimize against\n"
	}
	// Aggressive dedupe/minimize before L2 import — prevents overnight corpus bloat.
	// Bound to remaining parent/lease budget (was Background×10m → lease overrun /
	// lease_lost_during_engine + late CLEAN races).
	if after > 8 && !blindFuzzerRail(stdinBin) {
		// Minimize only with leftover lease slack. A 2-minute merge used to consume
		// most of a short lite wall and raced lease_lost_during_engine.
		mergeBudget := 30 * time.Second
		if wallSec >= 180 {
			mergeBudget = 45 * time.Second
		}
		if deadline, ok := ctx.Deadline(); ok {
			if rem := time.Until(deadline); rem > 0 && rem < mergeBudget {
				mergeBudget = rem
			}
		}
		if mergeBudget < 5*time.Second {
			sessionLog += fmt.Sprintf("\n[merge] skipped: lease budget exhausted (%s)\n", mergeBudget)
		} else {
			mergeCtx, mergeCancel := context.WithTimeout(ctx, mergeBudget)
			mb, ma, merr := MergeMinimizeCorpus(mergeCtx, binPath, corpusDir)
			mergeCancel()
			if merr == nil {
				after = ma
				sessionLog += fmt.Sprintf("\n[merge] before=%d after=%d\n", mb, ma)
			} else {
				sessionLog += fmt.Sprintf("\n[merge] err=%v before=%d after=%d\n", merr, mb, ma)
			}
		}
	}
	statsPath := filepath.Join(sessionDir, "session_stats.json")
	_ = writeLibFuzzerSessionStats(corpusDir, statsPath, targetID, wallSec, before, after, sessionLog)

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

// readOpenSessionLog reads stderr/stdout captured via an already-open file
// descriptor. Path-based re-open is unsafe after a harness RW Dir bind.
func readOpenSessionLog(f *os.File) string {
	if f == nil {
		return ""
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return ""
	}
	b, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return ""
	}
	return string(b)
}

// libFuzzerForkJobs is how many in-process libFuzzer jobs share one lease wall.
// A pool shard used to be one ASAN process, so N miners with cold corpora lost
// to one overnight local run. Two jobs on a ≥180s wall roughly double execs
// without growing the paid wall. HACKME_HUNT_LF_FORK overrides (1..4). 0 or
// empty keeps the auto rule. Pass 1 only when the binary is not libFuzzer.
// Stdin exec drivers must use libFuzzerSessionForkJobs, which caps this at 2.
func libFuzzerForkJobs(wallSec int) int {
	if v := strings.TrimSpace(os.Getenv("HACKME_HUNT_LF_FORK")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return 1
		}
		if n > 4 {
			return 4
		}
		return n
	}
	// Heavy walls have the lease slack for a third job. Two is the floor
	// once the wall is long enough that startup cost is amortized.
	if wallSec >= 300 {
		return 3
	}
	if wallSec >= 180 {
		return 2
	}
	return 1
}

// InProcessShardsToMatchLocal is how many concurrent in-process pool shards
// equal one local libFuzzer process's job-seconds.
//
// Assumption, not a measured overnight run: each in-process job on the same
// harness executes at the same rate, and the next claim already carries units
// the previous miner accepted, so the execs compound instead of restarting
// from boot starters. wallSec/forkJobs are the pool shard (libFuzzerSessionArgs).
// localSec/localJobs are one local process (overnight budget, fork 1).
//
//	shards = ceil(localSec * localJobs / (wallSec * forkJobs))
//
// This number does not apply to a stdin exec driver. That driver fork/execs
// the harness per unit. A measured trivial ASAN reader on this class of
// driver is tens of exec/s; an in-process parser is 10^4. Adding shards at
// the stdin rate does not match one local in-process process.
func InProcessShardsToMatchLocal(wallSec, forkJobs, localSec, localJobs int) int {
	if wallSec < 1 {
		wallSec = 1
	}
	if forkJobs < 1 {
		forkJobs = 1
	}
	if localSec < 1 {
		localSec = 1
	}
	if localJobs < 1 {
		localJobs = 1
	}
	num := int64(localSec) * int64(localJobs)
	den := int64(wallSec) * int64(forkJobs)
	n := (num + den - 1) / den
	if n < 1 {
		return 1
	}
	if n > 1_000_000 {
		return 1_000_000
	}
	return int(n)
}

// libFuzzerSessionForkJobs is the -fork count for one wall.
// In-process LLVMFuzzer binaries use libFuzzerForkJobs (3 at >=300s).
// The stdin driver fork/execs the harness for every unit in every job;
// fork>2 on that path OOMs the miner, so a 300s wall stays at 2.
// An explicit HACKME_HUNT_LF_FORK above 2 is capped the same way for stdin.
func libFuzzerSessionForkJobs(wallSec int, stdinDriver bool) int {
	n := libFuzzerForkJobs(wallSec)
	if stdinDriver && n > 2 {
		return 2
	}
	return n
}

// libFuzzerUnitTimeoutSec is the per-input ASAN budget. len_control=0 feeds
// 64KiB units immediately; a 3s timeout classifies those as slow and drops them.
// Overnight runs use the OSS-Fuzz-style 25s cap.
func libFuzzerUnitTimeoutSec(wallSec int) int {
	switch {
	case wallSec > 600:
		return 25
	case wallSec >= 180:
		return 10
	default:
		return 5
	}
}

// libFuzzerTuneForWall picks mutate depth for a short distributed lease.
// Entropic scheduling + value profile spend the wall on rare features instead of
// the default uniform schedule, which is what a single long local LF run gets for free.
func libFuzzerTuneForWall(wallSec int) (mutateDepth int) {
	switch {
	case wallSec >= 300:
		return 20
	case wallSec >= 180:
		return 16
	case wallSec >= 120:
		return 12
	default:
		return 8
	}
}

// libFuzzerSessionArgs is the argv tail for one Hunt libFuzzer wall.
// dictPath empty omits -dict=. Flags are LLVM libFuzzer options present since LLVM 9.
func libFuzzerSessionArgs(corpusDir, crashPrefix string, wallSec int, dictPath string, forkJobs int) []string {
	if wallSec <= 0 {
		wallSec = 120
	}
	if forkJobs < 1 {
		forkJobs = 1
	}
	if forkJobs > 4 {
		forkJobs = 4
	}
	// One job may use 2GiB. Forked jobs split that budget (floor 768) so two
	// ASAN children do not each reserve a full 2GiB shadow.
	rss := 2048
	if forkJobs > 1 {
		rss = 2048 / forkJobs
		if rss < 768 {
			rss = 768
		}
	}
	args := []string{
		corpusDir,
		"-max_total_time=" + fmt.Sprint(wallSec),
		"-timeout=" + fmt.Sprint(libFuzzerUnitTimeoutSec(wallSec)),
		"-rss_limit_mb=" + fmt.Sprint(rss),
		// libFuzzer runs LSan at shutdown unless this is set, even when
		// ASAN_OPTIONS already has detect_leaks=0. That pass eats the merge window,
		// so a short wall keeps it off; a standard/heavy wall has the budget and a
		// leak is a finding a customer pays for.
		"-detect_leaks=" + leakDetectFlag(wallSec),
		"-max_len=65536",
		"-mutate_depth=" + fmt.Sprint(libFuzzerTuneForWall(wallSec)),
		"-entropic=1",
		"-use_value_profile=1",
		"-artifact_prefix=" + crashPrefix,
		"-print_final_stats=1",
	}
	// Default -len_control ramps max length over a long run. A 3-minute pool
	// wall never reaches the sizes a 12h local run mutates. Disable the ramp
	// on lease-sized walls; overnight local keeps the ramp.
	if wallSec <= 600 {
		args = append(args, "-len_control=0")
		// A crash used to end the lease. The artifact is still written; the
		// rest of the wall keeps growing the corpus the next miner will load.
		args = append(args, "-ignore_crashes=1")
		// -reduce_inputs (default 1, LLVM) re-executes every new unit until it
		// shrinks. A lease wall spends that budget on new mutations instead.
		// MergeMinimizeCorpus still minimizes once when the corpus grew.
		args = append(args, "-reduce_inputs=0")
	}
	if forkJobs > 1 {
		args = append(args, "-fork="+fmt.Sprint(forkJobs))
	}
	if dictPath != "" {
		args = append(args, "-dict="+dictPath)
	}
	return args
}

func countCorpusFiles(dir string) (int, error) {
	return countCorpusFilesUnder(dir, dir)
}

func countCorpusFilesUnder(root, dir string) (int, error) {
	if root != "" && !corpusPathConfined(root, dir) {
		return 0, nil
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range ents {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		// Count regular files only — RW corpus may contain planted symlinks.
		if _, ok := isRegularCorpusFileUnder(root, filepath.Join(dir, e.Name())); !ok {
			continue
		}
		n++
	}
	return n, nil
}

// PublishedHarnessIsLibFuzzer reports whether a materialized harness is a
// real libFuzzer binary (exports LLVMFuzzerRunDriver) rather than a stdin
// ASAN target. The subprocess driver also links that symbol and is identified
// by HACKME_LF_STDIN_BIN, so it is never treated as the target.
func PublishedHarnessIsLibFuzzer(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() == 0 || st.Size() > 64<<20 {
		return false
	}
	b, err := io.ReadAll(io.LimitReader(f, 64<<20))
	if err != nil || len(b) == 0 {
		return false
	}
	if bytes.Contains(b, []byte("HACKME_LF_STDIN_BIN")) {
		return false
	}
	return bytes.Contains(b, []byte("LLVMFuzzerRunDriver"))
}

func seedPersistentCorpus(repoRoot, targetID, corpusDir string) error {
	// Prefer prior L2 seeds; else tiny JSON-ish starters.
	src := LibFuzzerSeedDir(repoRoot, targetID)
	if ents, err := os.ReadDir(src); err == nil {
		for _, e := range ents {
			if e.IsDir() {
				continue
			}
			b, err := ReadRegularCorpusFileUnder(src, filepath.Join(src, e.Name()), 1<<20)
			if err != nil || len(b) == 0 {
				continue
			}
			_ = writeFileBeneath(corpusDir, filepath.Join(corpusDir, e.Name()), b, 0o600)
		}
	}
	if n, _ := countCorpusFiles(corpusDir); n > 0 {
		return nil
	}
	starters := [][]byte{[]byte("{}"), []byte("[]"), []byte("null"), {0x80}, {0x00}}
	for i, b := range starters {
		_ = writeFileBeneath(corpusDir, filepath.Join(corpusDir, fmt.Sprintf("seed-boot-%d.bin", i)), b, 0o600)
	}
	return nil
}

func writeLibFuzzerSessionStats(corpusDir, path, targetID string, wall, before, after int, stderr string) error {
	feat := 0
	newUnits := 0
	execs := libFuzzerExecutedUnits(stderr)
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
	}
	if feat == 0 && newUnits > 0 {
		feat = newUnits
	}
	body := fmt.Sprintf("{\n  \"target\": %q,\n  \"wall_sec\": %d,\n  \"corpus_before\": %d,\n  \"corpus_after\": %d,\n  \"features_hint\": %d,\n  \"new_units_added\": %d,\n  \"execs\": %d,\n  \"updated_at\": %q\n}\n",
		targetID, wall, before, after, feat, newUnits, execs, time.Now().UTC().Format(time.RFC3339))
	// .hunt-run is inside the RW corpus bind. A harness can plant
	// session_stats.json (or replace .hunt-run) with a symlink after the wall;
	// os.WriteFile / MkdirAll would follow it and truncate a host file.
	if err := mkdirAllBeneath(corpusDir, filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFileBeneath(corpusDir, path, []byte(body), 0o644)
}

// libFuzzerExecutedUnits is how many inputs this wall actually ran.
// A single process prints stat::number_of_executed_units once at exit.
// -fork sends each job's stats to a temp log and deletes it; the parent
// reprints the running sum of those stats as "#<units>: cov:". Those lines
// are cumulative, so the max is the total. Missing counters stay 0: a shard
// that never executed must stay visible as zero, not as "unknown".
func libFuzzerExecutedUnits(stderr string) int {
	const statPrefix = "stat::number_of_executed_units:"
	statSum := 0
	forkMax := 0
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, statPrefix) {
			var n int
			if _, err := fmt.Sscanf(strings.TrimSpace(strings.TrimPrefix(line, statPrefix)), "%d", &n); err == nil && n > 0 {
				statSum += n
			}
			continue
		}
		// libFuzzer progress lines are "#2<TAB>INITED cov: 4 ft: 5 ..." — there is no
		// colon after the unit number, so the old "#%d: cov:" scan never matched and
		// this fallback was dead. It is the only execs signal a fork run leaves in the
		// parent's stderr (per-job final stats go to the job logs), which is why a
		// stdin-driver lease reported execs=0 while its corpus was visibly growing.
		if strings.HasPrefix(line, "#") && strings.Contains(line, "cov:") {
			var n int
			if _, err := fmt.Sscanf(line, "#%d", &n); err == nil && n > forkMax {
				forkMax = n
			}
		}
	}
	if forkMax > statSum {
		return forkMax
	}
	return statSum
}

// readLibFuzzerSessionExecs reads the execs field written beside the corpus.
// A missing file or an explicit 0 both return 0.
func readLibFuzzerSessionExecs(corpusDir string) int {
	if strings.TrimSpace(corpusDir) == "" {
		return 0
	}
	b, err := ReadRegularCorpusFileUnder(corpusDir, filepath.Join(corpusDir, ".hunt-run", "session_stats.json"), 1<<20)
	if err != nil || len(b) == 0 {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimSuffix(line, ",")
		if !strings.HasPrefix(line, `"execs":`) {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(strings.TrimPrefix(line, `"execs":`)), "%d", &n); err != nil || n < 0 {
			return 0
		}
		return n
	}
	return 0
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
	// Lstat: a symlink to some other ELF must not be executed as the driver.
	if st, err := os.Lstat(outPath); err == nil && st.Mode().IsRegular() && st.Size() > 0 {
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
	if err := writeLibFuzzerCacheBin(outPath, in); err != nil {
		return err
	}
	_ = writeLibFuzzerBinKind(outPath, libFuzzerBinKindStub)
	return nil
}

func writeLibFuzzerCacheBin(outPath string, data []byte) error {
	parent := filepath.Dir(outPath)
	if st, err := os.Lstat(parent); err == nil && st.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("hunt: refusing symlink libfuzzer cache dir %s", parent)
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	// Replaces a planted file symlink in place. os.WriteFile would follow it.
	return writeFileBeneath(parent, outPath, data, 0o755)
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
	if err := writeLibFuzzerCacheBin(outPath, in); err != nil {
		return err
	}
	_ = writeLibFuzzerBinKind(outPath, libFuzzerBinKindDedicated)
	return nil
}

func leakDetectFlag(wallSec int) string {
	if wallSec >= leakDetectWallSec {
		return "1"
	}
	return "0"
}

// stdinInprocHarness wraps a catalog stdin driver as an in-process libFuzzer target.
const stdinInprocHarness = "tasks/sources/fuzz/benchmark/stdin_inproc_libfuzzer.c"

// inprocMarkerPath marks a cached binary as the in-process build, so a later shard
// does not mistake it for the fork wrapper and build the stdin binary it needs.
func inprocMarkerPath(binPath string) string { return binPath + ".inproc" }

func inprocMarkerPresent(binPath string) bool {
	st, err := os.Lstat(inprocMarkerPath(binPath))
	return err == nil && st.Mode().IsRegular()
}

// InprocStdinDriverSupported reports whether a stdin driver can be run in-process.
// The shim calls the driver's entry once per input with stdin pointing at that input,
// so the entry has to be main(void), and a driver that calls exit() would end the
// whole session instead of the one input. Every catalog driver meets both today; this
// keeps a future one that does not from silently losing its crashes.
func InprocStdinDriverSupported(driverPath string) bool {
	if strings.TrimSpace(driverPath) == "" || !strings.HasSuffix(driverPath, ".c") {
		return false
	}
	raw, err := os.ReadFile(driverPath)
	if err != nil {
		return false
	}
	src := string(raw)
	if !mainVoidEntry.MatchString(src) {
		return false
	}
	return !exitCallSite.MatchString(src)
}

var (
	mainVoidEntry = regexp.MustCompile(`(^|\s)int\s+main\s*\(\s*void\s*\)`)
	exitCallSite  = regexp.MustCompile(`(^|[^_[:alnum:]])_?exit\s*\(`)
)

// buildInprocStdinLibFuzzer compiles the target's sources, its stdin driver with the
// entry renamed, and the in-process shim into one libFuzzer binary.
//
// Why this exists: the fork wrapper execs the stdin binary once per input, which costs
// two orders of magnitude in throughput and leaves libFuzzer counting the wrapper's
// own edges instead of the target's — a guided fuzzer with no feedback. Measured on
// the selftest target, 20s walls, three seeds: 97 exec/s and 7 edges and zero bug
// classes through the wrapper, against 2,012 exec/s and 44 edges and two classes
// in-process.
func buildInprocStdinLibFuzzer(ctx context.Context, repoRoot string, t fuzzupstream.Target, driverSrc, outPath string) error {
	shim := filepath.Join(repoRoot, stdinInprocHarness)
	if st, err := os.Stat(shim); err != nil || !st.Mode().IsRegular() {
		return fmt.Errorf("hunt: missing in-process shim %s", shim)
	}
	clonePath := filepath.Join(repoRoot, ".cache", "oss-cve-clones", t.ID)
	if err := fuzzupstream.InjectOSSCveBuildStubs(repoRoot, t.ID, clonePath); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	tmpDir, err := os.MkdirTemp("", "hunt-lf-inproc-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	tmpBin := filepath.Join(tmpDir, "fuzzer")
	args := []string{
		"-fsanitize=fuzzer,address,undefined",
		"-fno-omit-frame-pointer", "-g", "-O1",
		// The driver keeps its source untouched; only its entry symbol moves.
		"-Dmain=hackme_stdin_main",
		"-I", clonePath,
		"-o", tmpBin,
		shim, driverSrc,
	}
	for _, inc := range t.IncludeDirs {
		args = append(args, "-I"+fuzzupstream.IncludeDir(repoRoot, clonePath, inc))
	}
	args = append(args, fuzzupstream.ExpandUpstreamSrc(clonePath, t.UpstreamSrc)...)
	args = append(args, t.BuildFlags...)
	buildCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	binds := []string{tmpDir, clonePath, filepath.Dir(shim), filepath.Dir(driverSrc)}
	for _, inc := range t.IncludeDirs {
		binds = append(binds, fuzzupstream.IncludeDir(repoRoot, clonePath, inc))
	}
	stderr, err := runCompilerBuild(buildCtx, "clang", args, binds, 10*time.Minute)
	if err != nil {
		return fmt.Errorf("clang inproc libfuzzer %s: %w (%s)", t.ID, err, strings.TrimSpace(stderr))
	}
	in, err := os.ReadFile(tmpBin)
	if err != nil {
		return err
	}
	if err := writeLibFuzzerCacheBin(outPath, in); err != nil {
		return err
	}
	// The .kind sidecar is what catalogInProcessBinUsable consults, and the fork
	// wrapper writes "stub" at this same path. Without claiming it here, a target
	// whose wrapper was built first stays pinned to the blind rail forever: the stub
	// kind makes the fresh in-process binary look unusable, and the .inproc marker
	// then short-circuits every later build so nothing ever rewrites it.
	if err := writeLibFuzzerBinKind(outPath, libFuzzerBinKindDedicated); err != nil {
		return err
	}
	return os.WriteFile(inprocMarkerPath(outPath), []byte(t.ID+"\n"), 0o644)
}

// blindFuzzerRail reports whether this session ran the fork wrapper, whose coverage
// is its own and not the target's. Anything that reasons about coverage — corpus
// minimization above all — must not trust that signal.
func blindFuzzerRail(stdinBin string) bool { return strings.TrimSpace(stdinBin) != "" }

// CachedInprocLibFuzzerBin returns a ready in-process binary for a catalog target
// without building anything. The engine shard uses it to decide between the fast rail
// and a background warm-up, because a cold build (git clone plus clang) can outlive
// both the engine wall and the claim lease.
func CachedInprocLibFuzzerBin(repoRoot, targetID string) (string, bool) {
	if repoRoot == "" {
		repoRoot = RepoRoot()
	}
	id, ok := fuzzupstream.SanitizeCatalogID(targetID)
	if !ok {
		return "", false
	}
	binPath := LibFuzzerImportBinPath(repoRoot, id)
	if !inprocMarkerPresent(binPath) {
		return "", false
	}
	if !cachedDedicatedLibFuzzerBin(binPath) {
		return "", false
	}
	return binPath, true
}

// BuildInprocLibFuzzerForTarget builds (or reuses) the in-process binary for a catalog
// target. Separate from BuildLibFuzzerImport so a warm-up can run it off the lease.
func BuildInprocLibFuzzerForTarget(ctx context.Context, repoRoot, targetID string) (string, error) {
	if repoRoot == "" {
		repoRoot = RepoRoot()
	}
	id, ok := fuzzupstream.SanitizeCatalogID(targetID)
	if !ok {
		return "", fmt.Errorf("hunt: inproc build: invalid target id %q", targetID)
	}
	if bin, ready := CachedInprocLibFuzzerBin(repoRoot, id); ready {
		return bin, nil
	}
	if _, err := exec.LookPath("clang"); err != nil {
		return "", fmt.Errorf("hunt: inproc build: clang required")
	}
	manifest, err := fuzzupstream.LoadManifest(repoRoot)
	if err != nil {
		return "", err
	}
	t, err := manifest.TargetByID(id)
	if err != nil {
		return "", err
	}
	driver := fuzzupstream.DriverSourcePath(repoRoot, t)
	if !InprocStdinDriverSupported(driver) {
		return "", fmt.Errorf("hunt: inproc build: driver not usable for %s", id)
	}
	if _, err := fuzzupstream.EnsureClone(ctx, repoRoot, t); err != nil {
		return "", err
	}
	binPath := LibFuzzerImportBinPath(repoRoot, id)
	unlock := lockLibFuzzerImport(binPath)
	defer unlock()
	if bin, ready := CachedInprocLibFuzzerBin(repoRoot, id); ready {
		return bin, nil
	}
	if err := buildInprocStdinLibFuzzer(ctx, repoRoot, t, driver, binPath); err != nil {
		return "", err
	}
	return binPath, nil
}
