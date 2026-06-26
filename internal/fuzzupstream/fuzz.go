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
	if maxInput <= 0 {
		maxInput = 65536
	}
	if len(input) > maxInput {
		input = input[:maxInput]
	}
	runCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, binPath)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Env = []string{
		"PATH=/usr/bin:/bin",
		"ASAN_OPTIONS=detect_leaks=0:halt_on_error=1:allocator_may_return_null=1:print_stacktrace=1",
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
	sanitizer = detectSanitizer(blob)
	crash = sanitizer != ""
	if crash {
		return true, sanitizer, tail, nil
	}
	if runErr != nil {
		if _, ok := runErr.(*exec.ExitError); ok && strings.Contains(blob, "Sanitizer") {
			return true, "signal", tail, nil
		}
	}
	return false, "", tail, nil
}

func detectSanitizer(blob string) string {
	for _, sig := range []string{
		"heap-buffer-overflow",
		"stack-buffer-overflow",
		"use-after-free",
		"double-free",
		"SEGV on unknown address",
		"SUMMARY: AddressSanitizer",
		"SUMMARY: UndefinedBehaviorSanitizer",
		"runtime error:",
	} {
		if strings.Contains(blob, sig) {
			return sig
		}
	}
	return ""
}

func IsSecuritySanitizer(san string) bool {
	if san == "" {
		return false
	}
	if strings.Contains(san, "UndefinedBehaviorSanitizer") || strings.Contains(san, "runtime error:") {
		return false
	}
	return strings.Contains(san, "AddressSanitizer") ||
		strings.Contains(san, "heap-buffer-overflow") ||
		strings.Contains(san, "stack-buffer-overflow") ||
		strings.Contains(san, "use-after-free") ||
		strings.Contains(san, "double-free") ||
		strings.Contains(san, "SEGV on unknown address")
}

// Mutate applies 1–4 random mutations to a copy of input.
func Mutate(input []byte, maxLen int, rnd []byte) []byte {
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
		case 3: // append interesting
			interesting := [][]byte{{'{'}, {'['}, {'"'}, {'`'}, {'\n'}, {0xff}, {0x00}}
			ch := interesting[int(rnd[i+1])%len(interesting)]
			if len(out)+len(ch) <= maxLen {
				out = append(out, ch...)
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

// Hunt runs mutational fuzz on a built upstream binary.
func Hunt(ctx context.Context, repoRoot string, t Target, binPath string, seeds [][]byte, budget int, maxInput int, timeLimitSec int) (*HuntReport, error) {
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
		input := Mutate(seed, maxInput, rnd)
		crash, san, tail, err := RunInput(ctx, binPath, input, maxInput)
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
			TargetID:   t.ID,
			Title:      t.Title,
			Repo:       t.Repo,
			InputHex:   hex.EncodeToString(input),
			InputLen:   len(input),
			Sanitizer:  san,
			Tail:       tail,
			Iteration:  i,
			CWE:        t.CWE,
			Disclosure: "HOLD — responsible disclosure to upstream maintainer before publish",
		}
		rep.Crashes = append(rep.Crashes, cf)
	}
	rep.ElapsedSec = time.Since(start).Seconds()
	sec := 0
	for _, c := range rep.Crashes {
		if IsSecuritySanitizer(c.Sanitizer) {
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
