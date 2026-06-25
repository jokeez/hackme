package fuzznative

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	maxReproInputLen     = 65536
	defaultBinaryTimeout = 5 * time.Second
)

var (
	binaryBuildMu sync.Mutex
	binaryCache   = map[string]string{}
)

const asanMainWrapper = `
#include <stdlib.h>
int main(int argc, char **argv) {
	if (argc < 2) return 2;
	unsigned long long v = strtoull(argv[1], NULL, 0);
	int r = check((long long)v);
	return r != 0 ? 1 : 0;
}
`

// ReproMode selects Go port vs ASAN-compiled harness binary.
type ReproMode string

const (
	ReproModeGoPort     ReproMode = "go_port"
	ReproModeAsanBinary ReproMode = "asan_binary"
)

// ParseReproMode reads campaign config (defaults go_port).
func ParseReproMode(cfg map[string]any) ReproMode {
	if cfg == nil {
		return ReproModeGoPort
	}
	s := strings.TrimSpace(strings.ToLower(fmt.Sprint(cfg["native_repro_mode"])))
	switch s {
	case string(ReproModeAsanBinary), "asan", "binary", "tier_c":
		return ReproModeAsanBinary
	}
	tier := strings.TrimSpace(strings.ToLower(fmt.Sprint(cfg["depth_tier"])))
	if tier == "upstream_binary" || tier == "tier_c" {
		return ReproModeAsanBinary
	}
	return ReproModeGoPort
}

// EvalReproEx runs native repro with the selected backend.
func EvalReproEx(mode ReproMode, upstreamTarget, guardName string, input []byte, pins *PinManifest, repoRoot string) ReproResult {
	if mode == ReproModeAsanBinary {
		if res, ok := evalReproAsanBinary(upstreamTarget, guardName, input, pins, repoRoot); ok {
			return res
		}
		res := EvalRepro(upstreamTarget, guardName, input, pins)
		if res.Note != "" {
			res.Note = "asan_binary unavailable; go_port fallback: " + res.Note
		} else {
			res.Note = "asan_binary unavailable; go_port fallback"
		}
		return res
	}
	return EvalRepro(upstreamTarget, guardName, input, pins)
}

func evalReproAsanBinary(upstreamTarget, guardName string, input []byte, pins *PinManifest, repoRoot string) (ReproResult, bool) {
	target := ResolveTarget(upstreamTarget, guardName)
	inHex := hex.EncodeToString(input)
	res := ReproResult{
		Status:         StatusSkipped,
		UpstreamTarget: target,
		InputHex:       inHex,
		Note:           "asan_binary repro skipped",
	}
	if pins != nil && target != "" {
		if repo, ok := pins.Repos[target]; ok {
			res.UpstreamCommit = repo.Commit
			res.Harness = repo.FuzzHarness
		}
	}
	if len(input) > maxReproInputLen {
		input = input[:maxReproInputLen]
	}
	if repoRoot == "" {
		repoRoot = "."
	}
	if _, err := exec.LookPath("clang"); err != nil {
		res.Note = "clang not installed"
		return res, false
	}
	harnessPath, err := resolveHarnessPath(repoRoot, guardName)
	if err != nil {
		res.Note = err.Error()
		return res, false
	}
	res.Harness = filepath.Base(harnessPath)
	binPath, err := buildAsanHarness(repoRoot, harnessPath)
	if err != nil {
		res.Note = "asan build failed: " + err.Error()
		return res, false
	}
	u64Arg := inputToU64Arg(input)
	crash, guardHit, tail := runAsanBinary(context.Background(), binPath, u64Arg)
	if crash {
		res.Status = StatusNativeCrash
		res.NativeSignal = true
		res.GuardSignal = false
		res.Note = "ASAN/UBSAN crash on pinned harness — triage before CVE/disclosure"
		if tail != "" {
			res.Note += " | " + tail
		}
		return res, true
	}
	if guardHit {
		res.Status = StatusConfirmed
		res.NativeSignal = true
		res.GuardSignal = true
		res.Note = fmt.Sprintf("ASAN binary guard confirmed (%s)", filepath.Base(harnessPath))
		return res, true
	}
	goRes := EvalRepro(upstreamTarget, guardName, input, pins)
	if goRes.Status == StatusConfirmed {
		res.Status = StatusRejected
		res.Note = "wasm/go signal not reproduced on ASAN harness"
	} else {
		res.Status = StatusRejected
		res.Note = "no signal on ASAN harness"
	}
	_ = crash
	return res, true
}

func resolveHarnessPath(repoRoot, guardName string) (string, error) {
	g := strings.TrimSpace(guardName)
	g = strings.TrimPrefix(g, "upstream_")
	if g == "" {
		return "", fmt.Errorf("fuzznative: empty guard_name")
	}
	upstreamDir := filepath.Join(repoRoot, "tasks", "sources", "security", "upstream")
	abs := filepath.Clean(filepath.Join(upstreamDir, g+".c"))
	prefix := filepath.Clean(upstreamDir) + string(os.PathSeparator)
	if !strings.HasPrefix(abs, prefix) {
		return "", fmt.Errorf("fuzznative: harness path blocked")
	}
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("fuzznative: harness missing for %s", g)
	}
	return abs, nil
}

func buildAsanHarness(repoRoot, harnessPath string) (string, error) {
	harnessBytes, err := os.ReadFile(harnessPath)
	if err != nil {
		return "", err
	}
	includeDir := filepath.Join(repoRoot, "tasks", "sources", "security", "upstream")
	sum := sha256.Sum256(append(harnessBytes, []byte(includeDir+"-asan-v1")...))
	cacheKey := hex.EncodeToString(sum[:16])

	binaryBuildMu.Lock()
	defer binaryBuildMu.Unlock()
	if cached, ok := binaryCache[cacheKey]; ok {
		if _, err := os.Stat(cached); err == nil {
			return cached, nil
		}
	}
	cacheDir := filepath.Join(repoRoot, ".cache", "native-repro")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}
	binPath := filepath.Join(cacheDir, cacheKey+".bin")
	if st, err := os.Stat(binPath); err == nil && st.Mode().IsRegular() {
		binaryCache[cacheKey] = binPath
		return binPath, nil
	}
	tmpDir, err := os.MkdirTemp("", "hackme-asan-build-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)
	srcPath := filepath.Join(tmpDir, "wrapper.c")
	src := string(harnessBytes) + asanMainWrapper
	if err := os.WriteFile(srcPath, []byte(src), 0o600); err != nil {
		return "", err
	}
	tmpBin := filepath.Join(tmpDir, "repro")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "clang",
		"-fsanitize=address,undefined",
		"-fno-omit-frame-pointer",
		"-g", "-O1",
		"-I", includeDir,
		"-o", tmpBin,
		srcPath,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("clang: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	in, err := os.ReadFile(tmpBin)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(binPath, in, 0o755); err != nil {
		return "", err
	}
	binaryCache[cacheKey] = binPath
	return binPath, nil
}

func inputToU64Arg(input []byte) string {
	var u uint64
	for i := 0; i < 8 && i < len(input); i++ {
		u |= uint64(input[i]) << (8 * i)
	}
	return fmt.Sprintf("0x%x", u)
}

func runAsanBinary(ctx context.Context, binPath, u64Arg string) (crash, guardHit bool, tail string) {
	ctx, cancel := context.WithTimeout(ctx, defaultBinaryTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath, u64Arg)
	cmd.Env = []string{
		"PATH=/usr/bin:/bin",
		"ASAN_OPTIONS=detect_leaks=0:halt_on_error=1:allocator_may_return_null=1:print_stacktrace=0",
		"UBSAN_OPTIONS=halt_on_error=1:print_stacktrace=0",
		"HOME=/tmp",
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	blob := stdout.String() + stderr.String()
	if len(blob) > 600 {
		tail = strings.TrimSpace(blob[len(blob)-600:])
	} else {
		tail = strings.TrimSpace(blob)
	}
	crash = sanitizerCrash(blob, runErr)
	if !crash && runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			guardHit = true
		}
	}
	return crash, guardHit, tail
}

func sanitizerCrash(blob string, runErr error) bool {
	if strings.Contains(blob, "heap-buffer-overflow") ||
		strings.Contains(blob, "stack-buffer-overflow") ||
		strings.Contains(blob, "use-after-free") ||
		strings.Contains(blob, "runtime error:") ||
		strings.Contains(blob, "SUMMARY: UndefinedBehaviorSanitizer") ||
		strings.Contains(blob, "SEGV on unknown address") {
		return true
	}
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			sig := exitErr.Sys()
			_ = sig
			// Signal kills often have exit code != 1
			if exitErr.ExitCode() != 0 && exitErr.ExitCode() != 1 {
				return strings.Contains(blob, "AddressSanitizer") || strings.Contains(blob, "UndefinedBehaviorSanitizer")
			}
		}
	}
	return false
}
