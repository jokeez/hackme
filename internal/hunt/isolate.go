package hunt

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// IsolateOpts bounds a native Hunt harness / fuzzer process.
type IsolateOpts struct {
	Bin        string
	Args       []string
	Env        []string
	Dir        string
	Wall       time.Duration
	RSSLimitMB int
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	// ExtraBind / ExtraRoBind are additional paths for the bwrap cage (corpus, stdin driver).
	ExtraBind   []string
	ExtraRoBind []string
	// AllowBare permits plain exec only when bwrap is missing or fails to *launch*
	// (tests / showmap helpers). It never skips an available cage.
	AllowBare bool
}

// IsolateRequired reports whether coverage-engine shards should prefer bwrap.
// Default ON (HACKME_HUNT_ISOLATE_REQUIRE empty/1). Set 0 to skip bwrap entirely.
// Launch failures may bare-retry only when IsolateStrict is opted out.
func IsolateRequired() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_HUNT_ISOLATE_REQUIRE")))
	if v == "" {
		return true
	}
	switch v {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// IsolateStrict refuses bare exec when bwrap is missing or fails to start.
// Default ON — miners must not fall through to unsandboxed foreign harnesses.
// Opt out only on locked-down operator hosts: HACKME_HUNT_ISOLATE_STRICT=0.
func IsolateStrict() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_HUNT_ISOLATE_STRICT")))
	if v == "" {
		return true
	}
	switch v {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// BubblewrapAvailable reports whether bwrap is on PATH.
func BubblewrapAvailable() bool {
	_, err := exec.LookPath("bwrap")
	return err == nil
}

var (
	bwrapProbeMu   sync.Mutex
	bwrapProbeOK   bool
	bwrapProbeDone bool
	bwrapProbePath string
)

// BubblewrapWorking probes that bwrap can run /bin/true in a minimal cage.
// Result is cached per resolved bwrap path (PATH changes in tests re-probe).
// Prefer this over parsing stderr of untrusted programs when deciding AllowBare.
func BubblewrapWorking() bool {
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		return false
	}
	bwrapProbeMu.Lock()
	defer bwrapProbeMu.Unlock()
	if bwrapProbeDone && bwrapProbePath == bwrap {
		return bwrapProbeOK
	}
	bwrapProbeOK = probeBubblewrap()
	bwrapProbePath = bwrap
	bwrapProbeDone = true
	return bwrapProbeOK
}

// ResetBubblewrapProbeForTest clears the cached probe (unit tests only).
func ResetBubblewrapProbeForTest() {
	bwrapProbeMu.Lock()
	defer bwrapProbeMu.Unlock()
	bwrapProbeDone = false
	bwrapProbeOK = false
	bwrapProbePath = ""
}

func probeBubblewrap() bool {
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		return false
	}
	trueBin, err := exec.LookPath("true")
	if err != nil {
		trueBin = "/bin/true"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bwrap,
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
		"--unshare-pid",
		"--unshare-net",
		"--die-with-parent",
		"--", trueBin,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = out
		return false
	}
	return true
}

// isolateEffectiveUID is os.Geteuid; tests override to exercise root refuse without root.
var isolateEffectiveUID = os.Geteuid

// refuseRootIsolated blocks paid Hunt exec as root unless AllowBare or HACKME_HUNT_ALLOW_ROOT.
// Applies to both bare and bwrap paths — namespaces ≠ uid drop.
func refuseRootIsolated(allowBare bool) error {
	if isolateEffectiveUID() != 0 || allowBare {
		return nil
	}
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_HUNT_ALLOW_ROOT")))
	if v == "1" || v == "true" || v == "yes" || v == "on" {
		return nil
	}
	return fmt.Errorf("hunt isolate: refusing root exec (set HACKME_HUNT_ALLOW_ROOT=1 to override)")
}

// RunIsolated executes bin under wall-clock + bubblewrap (--unshare-net) when available.
// When IsolateRequired and bwrap is missing (and AllowBare is false), returns an error
// instead of falling through to bare exec (M1 fail-closed for paid Hunt).
func RunIsolated(ctx context.Context, opts IsolateOpts) error {
	if strings.TrimSpace(opts.Bin) == "" {
		return fmt.Errorf("hunt isolate: empty bin")
	}
	if err := refuseRootIsolated(opts.AllowBare); err != nil {
		return err
	}
	wall := opts.Wall
	if wall <= 0 {
		wall = 60 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, wall+15*time.Second)
	defer cancel()

	env := append([]string{}, opts.Env...)
	if opts.RSSLimitMB > 0 {
		env = append(env, fmt.Sprintf("HACKME_ISOLATE_RSS_MB=%d", opts.RSSLimitMB))
	}
	env = scrubIsolateEnv(env)

	argv, useBwrap := wrapBubblewrap(opts.Bin, opts.Args, opts.Dir, opts.ExtraBind, opts.ExtraRoBind)
	if useBwrap && !IsolateRequired() {
		// Explicit escape (hub/desktop): skip bwrap when userns cages are denied.
		useBwrap = false
		argv = append([]string{opts.Bin}, opts.Args...)
	}
	// If the one-shot probe says the cage cannot run, never enter bwrap when
	// bare is permitted (AllowBare or non-STRICT). Start would succeed then Wait
	// fail with RTM_NEWADDR — and we intentionally never bare-retry after Wait.
	if useBwrap && !BubblewrapWorking() && (opts.AllowBare || !IsolateStrict()) {
		useBwrap = false
		argv = append([]string{opts.Bin}, opts.Args...)
	}
	if !useBwrap {
		if IsolateStrict() && !opts.AllowBare {
			return fmt.Errorf("hunt isolate: bwrap required (HACKME_HUNT_ISOLATE_STRICT)")
		}
		// Missing bwrap: AllowBare or non-STRICT → bare; else fail closed.
		return runBareIsolated(runCtx, opts, env, wall)
	}
	// CodeQL go/command-injection: MatchString barriers must sit in THIS function
	// (helpers that return slices are not treated as sanitizers).
	if len(argv) == 0 {
		return fmt.Errorf("hunt isolate: empty argv")
	}
	out := make([]string, 0, len(argv))
	for i, raw := range argv {
		a := strings.TrimSpace(raw)
		if a == "" {
			return fmt.Errorf("hunt isolate: empty argv element")
		}
		needAbs := i == 0 || filepath.IsAbs(a) || strings.HasPrefix(a, "/")
		if needAbs {
			cleaned := filepath.Clean(a)
			if i == 0 {
				if abs, e := filepath.Abs(cleaned); e == nil {
					cleaned = abs
				}
			}
			if !reSafeAbsPath.MatchString(cleaned) {
				slash := filepath.ToSlash(cleaned)
				if !reSafeAbsPathWin.MatchString(slash) {
					return fmt.Errorf("hunt isolate: path rejected by allowlist")
				}
				out = append(out, cleaned)
				continue
			}
			out = append(out, reSafeAbsPath.FindString(cleaned))
			continue
		}
		if !reIsolateArgToken.MatchString(a) {
			return fmt.Errorf("hunt isolate: arg rejected by allowlist")
		}
		out = append(out, reIsolateArgToken.FindString(a))
	}
	cmd := exec.CommandContext(runCtx, out[0], out[1:]...)
	cmd.Env = env
	if opts.Stdin != nil {
		cmd.Stdin = opts.Stdin
	}
	var stderrBuf bytes.Buffer
	attachIsolatedStdio(cmd, opts.Stdout, opts.Stderr, &stderrBuf)
	setProcessGroup(cmd)
	// Kill the whole process group while the leader is still alive (Cancel runs
	// before Wait returns). Post-Wait Getpgid was ESRCH and left orphans.
	cmd.Cancel = func() error { return killProcessGroup(cmd) }
	cmd.WaitDelay = 2 * time.Second
	allowBareFallback := !IsolateStrict() || opts.AllowBare
	// Bare fallback ONLY when bwrap fails to *launch* (Start error).
	// Never after Wait — a malicious harness/build.rs can forge
	// "bwrap: … Permission denied" on stderr to force unsandboxed re-exec.
	if err := cmd.Start(); err != nil {
		if bwrapCageFailed(err, "") && allowBareFallback {
			return runBareIsolated(runCtx, opts, env, wall)
		}
		if IsolateStrict() && !opts.AllowBare {
			return fmt.Errorf("hunt isolate: bwrap required (HACKME_HUNT_ISOLATE_STRICT): %v", err)
		}
		return err
	}
	if opts.RSSLimitMB > 0 && cmd.Process != nil {
		_ = setRSSLimitMB(cmd.Process.Pid, opts.RSSLimitMB)
	}
	waitErr := cmd.Wait()
	if runCtx.Err() != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("hunt isolate: wall exceeded (%s)", wall)
		}
		return fmt.Errorf("hunt isolate: cancelled: %w", runCtx.Err())
	}
	// Post-Wait: never bare-retry. Cage setup failures (RTM_NEWADDR / uid_map)
	// surface as errors under STRICT; forged stderr cannot unsandbox.
	if waitErr != nil && IsolateStrict() && !opts.AllowBare && bwrapSetupFailed(stderrBuf.String()) {
		return fmt.Errorf("hunt isolate: bwrap required (HACKME_HUNT_ISOLATE_STRICT): %v", waitErr)
	}
	return waitErr
}

func bwrapCageFailed(err error, stderr string) bool {
	msg := ""
	if err != nil {
		msg += err.Error() + " "
	}
	msg += stderr
	msg = strings.ToLower(msg)
	for _, needle := range []string{
		"namespace failed", "creating new namespace", "permission denied",
		"operation not permitted", "rtm_newaddr", "loopback", "uid map",
		"bwrap:",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// bwrapSetupFailed is true only when stderr carries bwrap's own diagnostics.
// Harness ASAN/"permission denied" must not strip the sandbox after Start.
func bwrapSetupFailed(stderr string) bool {
	msg := strings.ToLower(stderr)
	if !strings.Contains(msg, "bwrap:") {
		return false
	}
	for _, needle := range []string{
		"namespace failed", "creating new namespace", "permission denied",
		"operation not permitted", "rtm_newaddr", "loopback", "uid map",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// harnessLikelyRanInCage is true when stderr shows the target already executed
// (sanitizer / libFuzzer banners). Blocks forged "bwrap:" lines from triggering
// a bare re-exec after the harness ran inside the cage.
func harnessLikelyRanInCage(stderr string) bool {
	msg := strings.ToLower(stderr)
	for _, needle := range []string{
		"addresssanitizer", "asan:", "undefinedbehavior", "ubsan",
		"libfuzzer", "memorysanitizer", "leakSanitizer", "summary:",
		"==ERROR:", "runtime error:",
	} {
		if strings.Contains(msg, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

// bareIsolatedCommand execs the harness with no_new_privs when setpriv(1) is available
// (Go 1.26+ removed syscall.SysProcAttr.NoNewPrivs).
func bareIsolatedCommand(ctx context.Context, bin string, args []string) *exec.Cmd {
	argv := append([]string{bin}, args...)
	if sp, err := exec.LookPath("setpriv"); err == nil {
		argv = append([]string{sp, "--nnp", "--"}, argv...)
	}
	if len(argv) == 0 {
		return exec.CommandContext(ctx, "/bin/false")
	}
	out := make([]string, 0, len(argv))
	for i, raw := range argv {
		a := strings.TrimSpace(raw)
		if a == "" {
			return exec.CommandContext(ctx, "/bin/false")
		}
		needAbs := i == 0 || filepath.IsAbs(a) || strings.HasPrefix(a, "/")
		if needAbs {
			cleaned := filepath.Clean(a)
			if i == 0 {
				if abs, e := filepath.Abs(cleaned); e == nil {
					cleaned = abs
				}
			}
			if !reSafeAbsPath.MatchString(cleaned) {
				slash := filepath.ToSlash(cleaned)
				if !reSafeAbsPathWin.MatchString(slash) {
					return exec.CommandContext(ctx, "/bin/false")
				}
				out = append(out, cleaned)
				continue
			}
			out = append(out, reSafeAbsPath.FindString(cleaned))
			continue
		}
		if !reIsolateArgToken.MatchString(a) {
			return exec.CommandContext(ctx, "/bin/false")
		}
		out = append(out, reIsolateArgToken.FindString(a))
	}
	return exec.CommandContext(ctx, out[0], out[1:]...)
}

func runBareIsolated(runCtx context.Context, opts IsolateOpts, env []string, wall time.Duration) error {
	// Defense in depth when called directly (RunIsolated already checks).
	if err := refuseRootIsolated(opts.AllowBare); err != nil {
		return err
	}
	cmd := bareIsolatedCommand(runCtx, opts.Bin, opts.Args)
	cmd.Env = env
	if opts.Dir != "" {
		cmd.Dir = opts.Dir
	}
	if opts.Stdin != nil {
		cmd.Stdin = opts.Stdin
	}
	// Bare path: still serialize when stdout==stderr share one writer.
	attachIsolatedStdio(cmd, opts.Stdout, opts.Stderr, nil)
	setProcessGroup(cmd)
	cmd.Cancel = func() error { return killProcessGroup(cmd) }
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Start(); err != nil {
		return err
	}
	if opts.RSSLimitMB > 0 && cmd.Process != nil {
		_ = setRSSLimitMB(cmd.Process.Pid, opts.RSSLimitMB)
	}
	err := cmd.Wait()
	if runCtx.Err() != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("hunt isolate: wall exceeded (%s)", wall)
		}
		return fmt.Errorf("hunt isolate: cancelled: %w", runCtx.Err())
	}
	return err
}

// attachIsolatedStdio wires stdout/stderr. When both point at the same writer,
// route through syncWriter so concurrent pipe pumps cannot race a bytes.Buffer.
// When stderrTee is non-nil, stderr is also copied there (bwrap setup diagnostics).
func attachIsolatedStdio(cmd *exec.Cmd, stdout, stderr io.Writer, stderrTee *bytes.Buffer) {
	if cmd == nil {
		return
	}
	same := stdout != nil && stderr != nil && stdout == stderr
	if same {
		sw, ok := stdout.(*syncWriter)
		if !ok {
			sw = newSyncWriter(stdout)
		}
		cmd.Stdout = sw
		if stderrTee != nil {
			cmd.Stderr = io.MultiWriter(sw, stderrTee)
		} else {
			cmd.Stderr = sw
		}
		return
	}
	if stdout != nil {
		cmd.Stdout = stdout
	}
	if stderr != nil {
		if stderrTee != nil {
			cmd.Stderr = io.MultiWriter(stderr, stderrTee)
		} else {
			cmd.Stderr = stderr
		}
		return
	}
	if stderrTee != nil {
		cmd.Stderr = stderrTee
	}
}

// reIsolateArgToken is the CodeQL MatchString/FindString barrier for non-absolute
// argv tokens (flags, relative sources). Absolute paths use reSafeAbsPath.
var reIsolateArgToken = regexp.MustCompile(`^[A-Za-z0-9._+=,@%/~:-]+$`)

func wrapBubblewrap(bin string, args []string, dir string, extraBind, extraRo []string) ([]string, bool) {
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		out := append([]string{bin}, args...)
		return out, false
	}
	argv := []string{
		bwrap,
		"--die-with-parent",
		"--new-session",
		"--unshare-net",
		"--unshare-pid",
		"--unshare-ipc",
		"--unshare-uts",
		"--cap-drop", "ALL",
		"--proc", "/proc",
		"--dev", "/dev",
		"--ro-bind", "/usr", "/usr",
		"--ro-bind", "/lib", "/lib",
		"--ro-bind-try", "/lib64", "/lib64",
		"--ro-bind-try", "/bin", "/bin",
		"--ro-bind-try", "/sbin", "/sbin",
		"--ro-bind-try", "/etc/ld.so.cache", "/etc/ld.so.cache",
		"--ro-bind-try", "/etc/alternatives", "/etc/alternatives",
		"--tmpfs", "/tmp",
	}
	if dir = sanitizeCageDir(dir); dir != "" {
		abs, err := filepath.Abs(dir)
		if err == nil {
			if kept := filterSecretPrefixRoots([]string{abs}); len(kept) == 1 {
				argv = append(argv, "--bind", kept[0], kept[0], "--chdir", kept[0])
			}
		}
	}
	for _, p := range filterSecretPrefixRoots(extraBind) {
		if abs, err := filepath.Abs(strings.TrimSpace(p)); err == nil && abs != "" {
			if kept := filterSecretPrefixRoots([]string{abs}); len(kept) == 1 {
				argv = append(argv, "--bind", kept[0], kept[0])
			}
		}
	}
	for _, p := range filterSecretPrefixRoots(extraRo) {
		if abs, err := filepath.Abs(strings.TrimSpace(p)); err == nil && abs != "" {
			if kept := filterSecretPrefixRoots([]string{abs}); len(kept) == 1 {
				argv = append(argv, "--ro-bind-try", kept[0], kept[0])
			}
		}
	}
	if absBin, err := filepath.Abs(bin); err == nil {
		binDir := filepath.Dir(absBin)
		// Never RO-bind the operator prefix root as binDir (misplaced tool under /opt/hackme).
		if kept := filterSecretPrefixRoots([]string{binDir}); len(kept) == 1 {
			argv = append(argv, "--ro-bind", kept[0], kept[0])
		} else {
			argv = append(argv, "--ro-bind", absBin, absBin)
		}
		bin = absBin
	}
	argv = append(argv, "--", bin)
	argv = append(argv, args...)
	return argv, true
}

// ScrubBuildEnv drops admin/settlement/API tokens from a process environment.
// Use for customer harness builds (clang/cargo/afl-cc) so build.rs / proc-macros
// cannot inherit hub secrets. Alias of the isolate scrub list.
func ScrubBuildEnv(env []string) []string { return scrubIsolateEnv(env) }

// scrubIsolateEnv keeps a whitelist of compiler/fuzzer knobs and drops everything
// else (tokens, cloud keys, proxies, SSH agents). Prefer allowlist over denylist.
func scrubIsolateEnv(env []string) []string {
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
	// Fixed hint only — avoid len(env)/4+8 style size math CodeQL flags as overflow-prone.
	out := make([]string, 0, 8)
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

// LookAFLFuzz resolves afl-fuzz from PATH or hybrid bundle prefixes.
func LookAFLFuzz() (string, error) {
	if v := strings.TrimSpace(os.Getenv("HACKME_AFL_FUZZ")); v != "" {
		if st, err := os.Stat(v); err == nil && !st.IsDir() {
			return v, nil
		}
	}
	if p, err := exec.LookPath("afl-fuzz"); err == nil {
		return p, nil
	}
	prefixes := []string{
		strings.TrimSpace(os.Getenv("HACKME_PREFIX")),
		"/opt/hackme",
		filepath.Join(os.Getenv("HOME"), ".local/share/hackme/toolchains"),
	}
	for _, pref := range prefixes {
		if pref == "" {
			continue
		}
		for _, rel := range []string{
			"bin/afl-fuzz",
			"aflpp/bin/afl-fuzz",
			"toolchains/aflpp/bin/afl-fuzz",
		} {
			p := filepath.Join(pref, rel)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("afl-fuzz not found (install via scripts/ops/install_hunt_engines.sh)")
}

// AFLFuzzVersion returns a short version string for claim/submit reports.
func AFLFuzzVersion(bin string) string {
	if bin == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "-h")
	out, _ := cmd.CombinedOutput()
	s := string(out)
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(strings.ToLower(line), "afl++") || strings.HasPrefix(line, "afl-fuzz") {
			if len(line) > 80 {
				return line[:80]
			}
			return line
		}
	}
	return "afl++"
}

// LibFuzzerVersionHint is a stable report label (clang-linked LF has no --version).
func LibFuzzerVersionHint() string {
	if v := strings.TrimSpace(os.Getenv("HACKME_LIBFUZZER_VERSION")); v != "" {
		return v
	}
	return "libfuzzer"
}
