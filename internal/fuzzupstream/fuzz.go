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

// Mutate applies 1–4 random mutations to a copy of input.
func Mutate(input []byte, maxLen int, rnd []byte) []byte {
	return MutateWithDict(input, maxLen, rnd, nil)
}

// MutateWithDict applies mutations with optional domain dictionary splice bytes.
func MutateWithDict(input []byte, maxLen int, rnd []byte, dict []byte) []byte {
	if maxLen <= 0 {
		maxLen = 65536
	}
	out := make([]byte, len(input))
	copy(out, input)
	if len(out) == 0 {
		out = []byte{0}
	}
	ops := 1 + int(rnd[0]%4)
	for i := 0; i < ops; i++ {
		if len(rnd) < i+2 {
			break
		}
		switch rnd[i+1] % 7 {
		case 0: // bitflip
			if len(out) > 0 {
				p := int(rnd[i+1]) % len(out)
				out[p] ^= 1 << (rnd[i+1] % 8)
			}
		case 1: // insert byte
			if len(out) < maxLen {
				p := int(rnd[i+1]) % (len(out) + 1)
				out = append(out, 0)
				copy(out[p+1:], out[p:])
				out[p] = rnd[i+1]
			}
		case 2: // delete byte
			if len(out) > 1 {
				p := int(rnd[i+1]) % len(out)
				out = append(out[:p], out[p+1:]...)
			}
		case 3: // append interesting / dict splice
			if len(dict) > 0 {
				start := int(rnd[i+1]) % len(dict)
				n := 1 + int(rnd[i+1]%4)
				if start+n > len(dict) {
					n = len(dict) - start
				}
				if n > 0 && len(out)+n <= maxLen {
					out = append(out, dict[start:start+n]...)
				}
			} else {
				interesting := [][]byte{{'{'}, {'['}, {'"'}, {'`'}, {'\n'}, {0xff}, {0x00}}
				ch := interesting[int(rnd[i+1])%len(interesting)]
				if len(out)+len(ch) <= maxLen {
					out = append(out, ch...)
				}
			}
		case 4: // resize
			n := int(rnd[i+1]%32) + 1
			if n > maxLen {
				n = maxLen
			}
			out = make([]byte, n)
			for j := range out {
				out[j] = rnd[(i+j)%len(rnd)]
			}
		case 5: // duplicate slice
			if len(out) > 0 && len(out)*2 <= maxLen {
				out = append(out, out...)
			}
		default: // random byte replace
			if len(out) > 0 {
				p := int(rnd[i+1]) % len(out)
				out[p] = rnd[i+1]
			}
		}
	}
	if len(out) > maxLen {
		out = out[:maxLen]
	}
	return out
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
		input := MutateWithDict(seed, maxInput, rnd, opts.MutatorDict)
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
