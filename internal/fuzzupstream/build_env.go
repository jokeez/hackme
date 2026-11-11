package fuzzupstream

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ScrubBuildEnv keeps a whitelist of compiler/fuzzer knobs and drops everything
// else (tokens, cloud keys, proxies, SSH agents). Prefer allowlist over denylist
// so catalog Dig/Hunt builds cannot piggyback hub admin/P2P/relay secrets via
// customer build.rs / proc-macros (FUZZ-ENV-01). Mirrors hunt.scrubIsolateEnv.
func ScrubBuildEnv(env []string) []string {
	allowExact := map[string]bool{
		"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "SHELL": true,
		"TMPDIR": true, "TMP": true, "TEMP": true, "LANG": true, "LANGUAGE": true,
		"TERM": true, "TZ": true, "PWD": true,
		"CC": true, "CXX": true, "CPP": true, "AR": true, "RANLIB": true, "LD": true,
		"CFLAGS": true, "CXXFLAGS": true, "CPPFLAGS": true, "LDFLAGS": true,
		"CARGO_HOME": true, "CARGO_TARGET_DIR": true, "CARGO_NET_OFFLINE": true,
		"CARGO_TERM_COLOR": true, "RUSTUP_HOME": true, "RUSTC": true, "RUSTFLAGS": true,
		"RUSTDOCFLAGS": true, "RUST_BACKTRACE": true,
		"XDG_CACHE_HOME": true, "XDG_CONFIG_HOME": true, "XDG_DATA_HOME": true,
		"AFL_PATH": true, "AFL_SKIP_CPUFREQ": true, "AFL_I_DONT_CARE_ABOUT_MISSING_CRASHES": true,
		"AFL_NO_UI": true, "AFL_MAP_SIZE": true, "AFL_USE_ASAN": true, "AFL_USE_UBSAN": true,
		"ASAN_OPTIONS": true, "UBSAN_OPTIONS": true, "MSAN_OPTIONS": true, "LSAN_OPTIONS": true,
		"LIBFUZZER_OPTS": true, "LLVM_PROFILE_FILE": true,
		"HACKME_POOL_COORDINATOR_URL": true, "HACKME_POOL_SEED_FROM_RESEARCH": true,
		"HACKME_PREFIX": true, "HACKME_AFL_FUZZ": true, "HACKME_LIBFUZZER_VERSION": true,
		"HACKME_ISOLATE_RSS_MB": true,
	}
	allowPrefix := []string{
		"LC_", "AFL_", "LIBFUZZER_", "CARGO_", "RUST", "CCACHE_", "SCCACHE_",
	}
	out := make([]string, 0, len(env)/4+8)
	for _, e := range env {
		key, _, _ := strings.Cut(e, "=")
		if key == "" {
			continue
		}
		uk := strings.ToUpper(key)
		if strings.Contains(uk, "TOKEN") || strings.Contains(uk, "SECRET") ||
			strings.Contains(uk, "PASSWORD") || strings.Contains(uk, "CREDENTIAL") ||
			strings.Contains(uk, "API_KEY") || strings.Contains(uk, "PRIVATE") ||
			strings.Contains(uk, "SEED_HEX") {
			continue
		}
		if allowExact[key] {
			out = append(out, e)
			continue
		}
		for _, p := range allowPrefix {
			if strings.HasPrefix(key, p) {
				out = append(out, e)
				break
			}
		}
	}
	return out
}

// BuildCmdRunner optionally replaces bare compiler/cargo exec for catalog builds.
// Hunt sets this to hunt.RunBuildIsolated so hub builds inherit bwrap + scrubbed env.
type BuildCmdRunner func(ctx context.Context, bin string, args []string, env []string, dir string, extraBind []string, wall time.Duration, stdout, stderr io.Writer) error

// BuildRunner is the active catalog build executor (default: scrubbed bare exec).
var BuildRunner BuildCmdRunner = defaultBuildRunner

func defaultBuildRunner(ctx context.Context, bin string, args []string, env []string, dir string, _ []string, wall time.Duration, stdout, stderr io.Writer) error {
	if wall > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, wall)
		defer cancel()
	}
	if !filepath.IsAbs(bin) {
		if p, err := exec.LookPath(bin); err == nil {
			bin = p
		}
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if len(env) == 0 {
		env = os.Environ()
	}
	cmd.Env = ScrubBuildEnv(env)
	if stdout != nil {
		cmd.Stdout = stdout
	}
	if stderr != nil {
		cmd.Stderr = stderr
	}
	return cmd.Run()
}

// runBuildCmd executes a compiler/cargo step via BuildRunner (scrubbed; optional cage).
func runBuildCmd(ctx context.Context, bin string, args []string, env []string, dir string, binds []string, wall time.Duration) (stderr string, err error) {
	var buf bytes.Buffer
	runner := BuildRunner
	if runner == nil {
		runner = defaultBuildRunner
	}
	err = runner(ctx, bin, args, env, dir, binds, wall, nil, &buf)
	if err != nil {
		return buf.String(), err
	}
	return buf.String(), nil
}

// BuildSandboxOptOutHint documents desk escape for operators (also in hunt docs).
func BuildSandboxOptOutHint() string {
	return "HACKME_HUNT_BUILD_SANDBOX=0 disables bwrap for local desk harness builds (env still scrubbed)"
}

// ErrBuildSandbox documents a missing cage when STRICT builds are required.
var ErrBuildSandbox = fmt.Errorf("fuzzupstream: build sandbox required")
