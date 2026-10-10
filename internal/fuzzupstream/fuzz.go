package fuzzupstream

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"hackme/internal/fuzzengine"
)

// RunInput executes bin with stdin data; returns crash info.
func RunInput(ctx context.Context, binPath string, input []byte, maxInput int) (crash bool, sanitizer, tail string, err error) {
	opts := DefaultRunInputOpts()
	if maxInput > 0 {
		opts.MaxInput = maxInput
	}
	crash, info, tail, err := RunInputDetailed(ctx, binPath, input, opts)
	if info.Raw != "" {
		sanitizer = info.Raw
	}
	return crash, sanitizer, tail, err
}

// RunInputDetailed executes one ASAN/UBSan harness input and returns normalized sanitizer info.
// Hunt stdin drivers get the bytes on stdin. cargo-fuzz / libFuzzer binaries ignore stdin and
// would spin forever — those are run once via a temp file and -runs=1.
func RunInputDetailed(ctx context.Context, binPath string, input []byte, opts RunInputOpts) (crash bool, info SanitizerInfo, tail string, err error) {
	if opts.MaxInput <= 0 {
		opts.MaxInput = 65536
	}
	if len(input) > opts.MaxInput {
		input = input[:opts.MaxInput]
	}
	bin := filepath.Clean(strings.TrimSpace(binPath))
	if _, verr := ValidateBinPath(bin); verr != nil {
		return false, SanitizerInfo{}, "", verr
	}
	if !reAbsBinPath.MatchString(bin) {
		return false, SanitizerInfo{}, "", errors.New("fuzzupstream: binary path rejected by allowlist")
	}
	if binaryLooksLikeLibFuzzer(bin) {
		return runLibFuzzerOnce(ctx, bin, input, opts)
	}
	return runStdinOnce(ctx, bin, input, opts)
}

func runStdinOnce(ctx context.Context, bin string, input []byte, opts RunInputOpts) (crash bool, info SanitizerInfo, tail string, err error) {
	runCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, bin)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Env = harnessExecEnv(opts)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	return classifyHarnessRun(runCtx, runErr, stdout.String()+stderr.String())
}

func runLibFuzzerOnce(ctx context.Context, bin string, input []byte, opts RunInputOpts) (crash bool, info SanitizerInfo, tail string, err error) {
	dir, err := os.MkdirTemp("", "hunt-lf-in-*")
	if err != nil {
		return false, SanitizerInfo{}, "", err
	}
	defer os.RemoveAll(dir)
	inPath := filepath.Join(dir, "input")
	if err := os.WriteFile(inPath, input, 0o600); err != nil {
		return false, SanitizerInfo{}, "", err
	}
	runCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	// One-shot: feed a single corpus file; never start the interactive fuzz loop.
	cmd := exec.CommandContext(runCtx, bin, inPath, "-runs=1", "-timeout=2", fmt.Sprintf("-max_len=%d", opts.MaxInput))
	cmd.Env = harnessExecEnv(opts)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	return classifyHarnessRun(runCtx, runErr, stdout.String()+stderr.String())
}

func harnessExecEnv(opts RunInputOpts) []string {
	return []string{
		"PATH=/usr/bin:/bin",
		"ASAN_OPTIONS=" + asanOptions(opts.DetectLeaks),
		"MSAN_OPTIONS=halt_on_error=1:print_stats=0",
		"UBSAN_OPTIONS=halt_on_error=1:print_stacktrace=1",
		"HOME=/tmp",
	}
}

func classifyHarnessRun(runCtx context.Context, runErr error, blob string) (crash bool, info SanitizerInfo, tail string, err error) {
	if len(blob) > 800 {
		tail = strings.TrimSpace(blob[len(blob)-800:])
	} else {
		tail = strings.TrimSpace(blob)
	}
	if runErr != nil && runCtx.Err() == context.DeadlineExceeded {
		return false, SanitizerInfo{}, tail, fmt.Errorf("fuzzupstream: exec timeout: %w", runErr)
	}
	ee, isExit := runErr.(*exec.ExitError)
	if runErr != nil && !isExit {
		// Start/permission/not-found and other infra errors must not fail-open as CLEAN.
		return false, SanitizerInfo{}, tail, runErr
	}
	if runErr == nil {
		// Exit 0: a target that echoes "heap-buffer-overflow" is not an ASAN crash (report #14).
		return false, SanitizerInfo{}, tail, nil
	}
	info = ClassifySanitizer(blob)
	if info.Class == "asan" && !hasASANBanner(blob) {
		info = SanitizerInfo{}
	}
	if info.Class != "" {
		return true, info, tail, nil
	}
	if exitSignaled(ee) {
		// Deadly signal without a sanitizer banner: needs triage, not a bounty and not CLEAN.
		return true, SanitizerInfo{Class: "signal", Subtype: "needs_triage", Raw: "signal", Security: false}, tail, nil
	}
	// Ordinary non-zero exit (parse error, exit 1) stays clean.
	return false, SanitizerInfo{}, tail, nil
}

var libFuzzerDetectCache sync.Map // abs path → libFuzzerDetectEntry

type libFuzzerDetectEntry struct {
	size    int64
	modNano int64
	isLF    bool
}

func binaryLooksLikeLibFuzzer(bin string) bool {
	st, err := os.Stat(bin)
	if err != nil || !st.Mode().IsRegular() {
		return false
	}
	modNano := st.ModTime().UnixNano()
	if v, ok := libFuzzerDetectCache.Load(bin); ok {
		e := v.(libFuzzerDetectEntry)
		if e.size == st.Size() && e.modNano == modNano {
			return e.isLF
		}
	}
	isLF := scanLibFuzzerMarkers(bin)
	libFuzzerDetectCache.Store(bin, libFuzzerDetectEntry{size: st.Size(), modNano: modNano, isLF: isLF})
	return isLF
}

func scanLibFuzzerMarkers(bin string) bool {
	f, err := os.Open(bin)
	if err != nil {
		return false
	}
	defer f.Close()
	markers := [][]byte{
		[]byte("LLVMFuzzerRunDriver"),
		[]byte("SUMMARY: libFuzzer:"),
		[]byte("libFuzzer: deadly signal"),
		[]byte("ERROR: libFuzzer:"),
	}
	buf := make([]byte, 1<<20)
	for {
		n, rerr := f.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			for _, m := range markers {
				if bytes.Contains(chunk, m) {
					return true
				}
			}
		}
		if rerr == io.EOF {
			return false
		}
		if rerr != nil {
			return false
		}
	}
}

func exitSignaled(ee *exec.ExitError) bool {
	if ee == nil {
		return false
	}
	ws, ok := ee.Sys().(syscall.WaitStatus)
	return ok && ws.Signaled()
}

func detectSanitizer(blob string) string {
	info, ok := ClassifySanitizerOutput(blob)
	if !ok {
		return ""
	}
	if info.Raw != "" {
		return info.Raw
	}
	return info.Subtype
}

// Mutate applies staged mutations via fuzzengine havoc (interesting, dict-ops).
func Mutate(input []byte, maxLen int, rnd []byte) []byte {
	return MutateWithDict(input, maxLen, rnd, nil)
}

// MutateWithDict applies mutations with optional domain dictionary and corpus autodict.
func MutateWithDict(input []byte, maxLen int, rnd []byte, dict []byte) []byte {
	return huntMutateInput(input, maxLen, rnd, dict, nil)
}

func huntMutateInput(seed []byte, maxInput int, rnd []byte, dict []byte, corpus [][]byte) []byte {
	if maxInput <= 0 {
		maxInput = 65536
	}
	if len(rnd) < 8 {
		rnd = append(rnd, randomBytes(8-len(rnd))...)
	}
	stage := fuzzengine.MutationStage(int(rnd[0]) % (fuzzengine.StageDeterministicMax + 12))
	salt := uint64(rnd[1]) | uint64(rnd[2])<<8 | uint64(rnd[3])<<16 | uint64(rnd[4])<<24
	cfg := map[string]any{}
	if len(dict) > 0 {
		cfg["mutator_dict"] = dict
	}
	return fuzzengine.MutateBytesForHunt(seed, stage, salt, maxInput, cfg, corpus)
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

// HuntRunOptions configures one upstream Hunt mutational session.
type HuntRunOptions struct {
	DetectLeaks   bool
	MutatorDict   []byte
	CorpusDir     string   // persist growing corpus here (optional)
	ExtraSeedDirs []string // additional dirs to load seeds from (libFuzzer cache, etc.)
	MaxCorpus     int      // live corpus cap (default 512)
}

const defaultMaxCorpus = 512

// Hunt runs mutational fuzz on a built upstream binary.
func Hunt(ctx context.Context, repoRoot string, t Target, binPath string, seeds [][]byte, budget int, maxInput int, timeLimitSec int) (*HuntReport, error) {
	return HuntWithOptions(ctx, repoRoot, t, binPath, seeds, budget, maxInput, timeLimitSec, HuntRunOptions{
		DetectLeaks: DetectLeaksEnabled(),
	})
}

// HuntWithOptions runs mutational fuzz with sanitizer and mutator dictionary options.
func HuntWithOptions(ctx context.Context, repoRoot string, t Target, binPath string, seeds [][]byte, budget int, maxInput int, timeLimitSec int, opts HuntRunOptions) (*HuntReport, error) {
	if budget <= 0 {
		budget = 60000
	}
	if timeLimitSec <= 0 {
		timeLimitSec = 600
	}
	if opts.MaxCorpus <= 0 {
		opts.MaxCorpus = defaultMaxCorpus
	}
	start := time.Now()
	rep := &HuntReport{
		TargetID:   t.ID,
		Title:      t.Title,
		Repo:       t.Repo,
		Language:   TargetLanguage(t),
		BinaryPath: binPath,
		Crashes:    []CrashFinding{},
		DictBytes:  len(opts.MutatorDict),
		CorpusDir:  opts.CorpusDir,
	}
	if len(seeds) == 0 {
		seeds = [][]byte{{}, []byte("{}"), []byte("[]")}
	}
	// Merge persisted + extra seed dirs into the live seed pool.
	live := make([][]byte, 0, len(seeds)+64)
	seenHash := map[string]struct{}{}
	addSeed := func(b []byte) {
		if len(b) == 0 {
			return
		}
		h := corpusKey(b)
		if _, ok := seenHash[h]; ok {
			return
		}
		seenHash[h] = struct{}{}
		cp := append([]byte(nil), b...)
		live = append(live, cp)
	}
	for _, s := range seeds {
		addSeed(s)
	}
	if opts.CorpusDir != "" {
		for _, b := range loadCorpusDir(opts.CorpusDir, opts.MaxCorpus) {
			addSeed(b)
		}
	}
	for _, dir := range opts.ExtraSeedDirs {
		for _, b := range loadCorpusDir(dir, opts.MaxCorpus/2) {
			addSeed(b)
		}
	}
	if len(live) == 0 {
		live = [][]byte{{}, []byte("{}"), []byte("[]")}
	}

	seenCrash := map[string]bool{}
	deadline := time.Now().Add(time.Duration(timeLimitSec) * time.Second)
	execErrors := 0
	saved := 0
	lengthSeen := map[int]int{}

	for i := 0; i < budget; i++ {
		if ctx.Err() != nil {
			break
		}
		if time.Now().After(deadline) {
			break
		}
		seed := live[i%len(live)]
		rnd := randomBytes(16)
		corpus := live
		if len(corpus) > 64 {
			// Bound mutator corpus view for CPU; still rotate via live seeds.
			corpus = live[:64]
		}
		input := huntMutateInput(seed, maxInput, rnd, opts.MutatorDict, corpus)
		runOpts := DefaultRunInputOpts()
		if maxInput > 0 {
			runOpts.MaxInput = maxInput
		}
		runOpts.DetectLeaks = opts.DetectLeaks
		crash, info, tail, err := RunInputDetailed(ctx, binPath, input, runOpts)
		if err != nil {
			execErrors++
			continue
		}
		rep.Iterations++
		if !crash {
			// Novelty heuristic without coverage: rare lengths + unique prefix hash.
			if shouldKeepNovel(input, lengthSeen, seenHash, i) {
				if addCorpusFile(opts.CorpusDir, input, &saved, opts.MaxCorpus) {
					addSeed(input)
				}
			}
			continue
		}
		origLen := len(input)
		if len(input) > 1 {
			tr := TrimCrashInput(ctx, binPath, input, runOpts, info)
			if len(tr.Input) > 0 {
				input = tr.Input
			}
		}
		key := hex.EncodeToString(input)
		if len(key) > 64 {
			key = key[:64]
		}
		if seenCrash[key] {
			continue
		}
		seenCrash[key] = true
		if addCorpusFile(opts.CorpusDir, input, &saved, opts.MaxCorpus) {
			addSeed(input)
		}
		cf := CrashFinding{
			TargetID:         t.ID,
			Title:            t.Title,
			Repo:             t.Repo,
			InputHex:         hex.EncodeToString(input),
			InputLen:         len(input),
			OriginalInputLen: origLen,
			Trimmed:          len(input) < origLen,
			Sanitizer:        info.Raw,
			SanitizerClass:   info.Class,
			SanitizerSubtype: info.Subtype,
			SanitizerLabel:   info.Label,
			Tail:             tail,
			Iteration:        i,
			CWE:              t.CWE,
			Disclosure:       "HOLD — responsible disclosure to upstream maintainer before publish",
		}
		rep.Crashes = append(rep.Crashes, cf)
	}
	rep.ElapsedSec = time.Since(start).Seconds()
	rep.CorpusSize = len(live)
	rep.CorpusSaved = saved
	if rep.Iterations == 0 && execErrors > 0 {
		return rep, fmt.Errorf("fuzzupstream: hunt produced 0 successful execs (%d infra errors)", execErrors)
	}
	sec := 0
	for _, c := range rep.Crashes {
		if c.SanitizerClass == "asan" || IsSecuritySanitizer(c.Sanitizer) {
			sec++
		}
	}
	switch {
	case sec > 0:
		rep.Verdict = "CVE_CANDIDATE"
	case len(rep.Crashes) > 0:
		rep.Verdict = "INFORMATIONAL"
	default:
		rep.Verdict = "CLEAN"
	}
	return rep, nil
}

// SaveCrashArtifact writes crash input to outDir.
func SaveCrashArtifact(outDir string, cf CrashFinding) (string, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("crash-%s-%s.bin", cf.TargetID, cf.InputHex[:min(16, len(cf.InputHex))])
	path := filepath.Join(outDir, name)
	b, err := hex.DecodeString(cf.InputHex)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
