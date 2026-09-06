package fuzzupstream

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// RunInputDetailed executes bin with stdin data and returns normalized sanitizer info.
func RunInputDetailed(ctx context.Context, binPath string, input []byte, opts RunInputOpts) (crash bool, info SanitizerInfo, tail string, err error) {
	if opts.MaxInput <= 0 {
		opts.MaxInput = 65536
	}
	if len(input) > opts.MaxInput {
		input = input[:opts.MaxInput]
	}
	runCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, binPath)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Env = []string{
		"PATH=/usr/bin:/bin",
		"ASAN_OPTIONS=" + asanOptions(opts.DetectLeaks),
		"UBSAN_OPTIONS=halt_on_error=1:print_stacktrace=1",
		"HOME=/tmp",
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	blob := stdout.String() + stderr.String()
	if len(blob) > 800 {
		tail = strings.TrimSpace(blob[len(blob)-800:])
	} else {
		tail = strings.TrimSpace(blob)
	}
	info = ClassifySanitizer(blob)
	crash = info.Raw != "" || info.Class != ""
	if crash && info.Raw == "" {
		info = ClassifySanitizer(blob + "\nSUMMARY: AddressSanitizer: signal")
	}
	if crash {
		return true, info, tail, nil
	}
	if runErr != nil {
		if _, ok := runErr.(*exec.ExitError); ok && strings.Contains(blob, "Sanitizer") {
			info = ClassifySanitizer(blob)
			if info.Raw == "" {
				info.Raw = "signal"
			}
			return true, info, tail, nil
		}
	}
	return false, SanitizerInfo{}, tail, nil
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
	DetectLeaks bool
	MutatorDict []byte
}

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
	start := time.Now()
	rep := &HuntReport{
		TargetID:   t.ID,
		Title:      t.Title,
		Repo:       t.Repo,
		Language:   TargetLanguage(t),
		BinaryPath: binPath,
		Crashes:    []CrashFinding{},
	}
	if len(seeds) == 0 {
		seeds = [][]byte{{}, []byte("{}"), []byte("[]")}
	}
	seenCrash := map[string]bool{}
	deadline := time.Now().Add(time.Duration(timeLimitSec) * time.Second)

	for i := 0; i < budget; i++ {
		if ctx.Err() != nil {
			break
		}
		if time.Now().After(deadline) {
			break
		}
		seed := seeds[i%len(seeds)]
		rnd := randomBytes(16)
		corpus := make([][]byte, 0, len(seeds))
		for _, s := range seeds {
			if len(s) > 0 {
				corpus = append(corpus, s)
			}
		}
		input := huntMutateInput(seed, maxInput, rnd, opts.MutatorDict, corpus)
		runOpts := DefaultRunInputOpts()
		if maxInput > 0 {
			runOpts.MaxInput = maxInput
		}
		runOpts.DetectLeaks = opts.DetectLeaks
		crash, info, tail, err := RunInputDetailed(ctx, binPath, input, runOpts)
		if err != nil {
			continue
		}
		rep.Iterations++
		if !crash {
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
