package hunt

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

// BuildSandboxEnabled reports whether harness compiles (clang/cargo/afl-cc) run
// under RunIsolated (bwrap + scrubbed env). Default ON.
//
// Desk opt-out when userns/bwrap breaks local builds:
//
//	HACKME_HUNT_BUILD_SANDBOX=0
//
// Token scrubbing still applies when the cage is off (ScrubBuildEnv / scrubIsolateEnv).
func BuildSandboxEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_HUNT_BUILD_SANDBOX")))
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

// BuildIsolateOpts configures a sandboxed compiler / cargo / afl-cc invocation.
type BuildIsolateOpts struct {
	Bin         string
	Args        []string
	Dir         string
	Env         []string // full env before scrub; empty → os.Environ()
	ExtraBind   []string // RW binds (pin, tmp, cargo target, …)
	ExtraRoBind []string
	Wall        time.Duration
	Stdout      io.Writer
	Stderr      io.Writer
}

// RunBuildIsolated runs a harness build under RunIsolated when BuildSandboxEnabled.
// Env is always scrubbed. With sandbox off (desk), scrubbed bare exec is used.
// Hub/production (sandbox ON): fail closed when bwrap cannot cage — never bare-retry
// after Wait, never AllowBare.
func RunBuildIsolated(ctx context.Context, opts BuildIsolateOpts) error {
	if strings.TrimSpace(opts.Bin) == "" {
		return fmt.Errorf("hunt build isolate: empty bin")
	}
	// Resolve PATH names before bwrap (Abs("cargo") would wrongly become $PWD/cargo).
	if !filepath.IsAbs(opts.Bin) {
		if p, err := exec.LookPath(opts.Bin); err == nil {
			opts.Bin = p
		}
	}
	env := opts.Env
	if len(env) == 0 {
		env = os.Environ()
	}
	env = scrubIsolateEnv(env)
	wall := opts.Wall
	if wall <= 0 {
		wall = 180 * time.Second
	}
	// Never RW/RO-bind the operator prefix root (or /home,/root,…) — ExtraRo alone
	// was insufficient after e72f730a (catalog repoRoot + absolute pins remounted it).
	extraRo := filterSecretPrefixRoots(append(append([]string{}, opts.ExtraRoBind...), buildToolchainRoBinds()...))
	extraBind := append([]string{}, opts.ExtraBind...)
	dir := sanitizeCageDir(opts.Dir)
	// Ephemeral HOME / CARGO_HOME / XDG_* — strip host values first so scrub
	// allowlist + Go/environ first-wins cannot keep operator ~/.cargo credentials.
	env = stripEnvKeys(env, "CARGO_HOME", "HOME", "XDG_CACHE_HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME")
	cargoHome, cacheHome, homeBinds, homeEnv := buildEphemeralHomeBinds(dir)
	extraBind = append(extraBind, homeBinds...)
	env = append(env, homeEnv...)
	_ = cargoHome
	_ = cacheHome
	extraBind = filterSecretPrefixRoots(uniqueBindPaths(extraBind...))
	extraRo = uniqueBindPaths(extraRo...)

	if !BuildSandboxEnabled() {
		return runBareBuild(ctx, BuildIsolateOpts{
			Bin: opts.Bin, Args: opts.Args, Dir: dir,
			Stdout: opts.Stdout, Stderr: opts.Stderr,
		}, env, wall)
	}
	if !BubblewrapAvailable() || !BubblewrapWorking() {
		// Hub (STRICT default ON): fail closed — never bare-compile customer code.
		// Desk/CI with HACKME_HUNT_ISOLATE_STRICT=0: scrubbed bare build only.
		if IsolateStrict() {
			return fmt.Errorf("hunt build isolate: bwrap required for harness builds (probe failed)")
		}
		return runBareBuild(ctx, BuildIsolateOpts{
			Bin: opts.Bin, Args: opts.Args, Dir: dir,
			Stdout: opts.Stdout, Stderr: opts.Stderr,
		}, env, wall)
	}
	return RunIsolated(ctx, IsolateOpts{
		Bin:         opts.Bin,
		Args:        opts.Args,
		Dir:         dir,
		Env:         env,
		Wall:        wall,
		Stdout:      opts.Stdout,
		Stderr:      opts.Stderr,
		ExtraBind:   extraBind,
		ExtraRoBind: extraRo,
		AllowBare:   false,
	})
}

func runBareBuild(ctx context.Context, opts BuildIsolateOpts, env []string, wall time.Duration) error {
	// Desk/opt-out path for HACKME_HUNT_BUILD_SANDBOX=0: must NOT enter bwrap.
	// RunIsolated still prefers a working cage when IsolateRequired (default ON),
	// which drops ephemeral /tmp/hunt-inv-build-* unless every path is bound —
	// clang then fails with "no such file or directory: …/hunt_wrapper.c".
	return runBareIsolated(ctx, IsolateOpts{
		Bin:    opts.Bin,
		Args:   opts.Args,
		Dir:    opts.Dir,
		Env:    env,
		Wall:   wall,
		Stdout: opts.Stdout,
		Stderr: opts.Stderr,
	}, env, wall)
}

// buildToolchainRoBinds lists RO mounts for clang/cargo/afl-cc cages.
// Never bind the whole HACKME_PREFIX / /opt/hackme — that tree holds .env.vps
// secrets (HACKME_ADMIN_TOKEN, …). Mirror task_codegen_sandbox: only toolchain
// subdirs under the prefix. System roots (/usr,/lib,…) stay for host compilers.
func buildToolchainRoBinds() []string {
	prefix := hackmePrefixRoot()
	// When HACKME_PREFIX lives under /usr (e.g. /usr/local/hackme), binding
	// whole /usr would put .env.vps inside the cage — use leaf tool dirs instead.
	var out []string
	if pathIsAncestorOf(filepath.Clean("/usr"), prefix) {
		out = []string{"/usr/bin", "/usr/lib", "/usr/lib64", "/usr/include", "/lib", "/lib64", "/bin", "/sbin"}
	} else {
		out = []string{"/usr", "/lib", "/lib64", "/bin", "/sbin"}
	}
	if prefix != "/" && prefix != "." && buildPathExists(prefix) {
		for _, sub := range []string{
			".rustup", ".cargo", "tinygo", "wabt", "nodejs", "bin",
			".npm-global", "zig", "toolchains",
		} {
			p := filepath.Join(prefix, sub)
			if buildPathExists(p) {
				out = append(out, p)
			}
		}
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		out = append(out, filepath.Join(home, ".local/share/hackme/toolchains"))
		// rustup toolchains are read-only; cargo registry stays ephemeral.
		out = append(out, filepath.Join(home, ".rustup"))
	}
	return filterSecretPrefixRoots(out)
}

func buildPathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// hackmePrefixRoot returns the cleaned operator prefix that must never be
// RO-bound wholesale into customer build cages.
func hackmePrefixRoot() string {
	prefix := strings.TrimSpace(os.Getenv("HACKME_PREFIX"))
	if prefix == "" {
		prefix = "/opt/hackme"
	}
	return filepath.Clean(prefix)
}

// filterSecretPrefixRoots drops the operator prefix root (and a few broad FS
// roots) so ExtraRoBind / ExtraBind / Dir / binDir lists cannot reintroduce
// .env.vps into the Hunt cage. Toolchain *subdirs* under the prefix are kept.
// Symlinks are resolved when possible so a plant like /tmp/x → /opt/hackme
// cannot reintroduce the prefix root under a non-prefix path string.
func filterSecretPrefixRoots(paths []string) []string {
	prefix := hackmePrefixRoot()
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = filepath.Clean(strings.TrimSpace(p))
		if p == "" || p == "." || p == "/" {
			continue
		}
		check := p
		if resolved, err := filepath.EvalSymlinks(p); err == nil && strings.TrimSpace(resolved) != "" {
			check = filepath.Clean(resolved)
		}
		if check == prefix || check == "/opt/hackme" || check == "/" {
			continue
		}
		// Ancestor of the operator prefix (e.g. /usr when prefix=/usr/local/hackme,
		// or /home/user when prefix=$HOME/.local/share/hackme) would expose .env.vps.
		if pathIsAncestorOf(check, prefix) || pathIsAncestorOf(check, "/opt/hackme") {
			continue
		}
		switch check {
		case "/etc", "/home", "/root", "/var", "/opt", "/tmp", "/dev", "/proc", "/sys":
			continue
		}
		// Bind the resolved path when available so bwrap cannot follow a
		// leftover symlink into a filtered root after the string check.
		out = append(out, check)
	}
	return out
}

// pathIsAncestorOf reports whether ancestor contains child (or equals it).
func pathIsAncestorOf(ancestor, child string) bool {
	ancestor = filepath.Clean(strings.TrimSpace(ancestor))
	child = filepath.Clean(strings.TrimSpace(child))
	if ancestor == "" || child == "" || ancestor == "." || child == "." {
		return false
	}
	rel, err := filepath.Rel(ancestor, child)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false
	}
	return true
}

// sanitizeCageDir returns dir only when it is safe to RW-bind / chdir into.
func sanitizeCageDir(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	kept := filterSecretPrefixRoots([]string{dir})
	if len(kept) == 0 {
		return ""
	}
	return kept[0]
}

// stripEnvKeys removes KEY=… entries so later exclusive values always win.
func stripEnvKeys(env []string, keys ...string) []string {
	drop := make(map[string]bool, len(keys))
	for _, k := range keys {
		drop[k] = true
	}
	out := make([]string, 0, len(env))
	for _, e := range env {
		key, _, _ := strings.Cut(e, "=")
		if drop[key] {
			continue
		}
		out = append(out, e)
	}
	return out
}

// buildEphemeralHomeBinds places HOME / CARGO_HOME / XDG_* under workDir (or
// a temp dir) so the cage never gets a writable bind of the operator ~/.cargo.
func buildEphemeralHomeBinds(workDir string) (cargoHome, cacheHome string, binds []string, env []string) {
	base := strings.TrimSpace(workDir)
	if base == "" {
		base = os.TempDir()
	}
	homeDir, err := SafeJoinUnder(base, ".hackme-build-home")
	if err != nil {
		return "", "", nil, nil
	}
	cargoHome, err = SafeJoinUnder(homeDir, ".cargo")
	if err != nil {
		return "", "", nil, nil
	}
	cacheHome, err = SafeJoinUnder(homeDir, ".cache")
	if err != nil {
		return "", "", nil, nil
	}
	xdgConfig, err := SafeJoinUnder(homeDir, ".config")
	if err != nil {
		return "", "", nil, nil
	}
	xdgData, err := SafeJoinUnder(homeDir, ".local", "share")
	if err != nil {
		return "", "", nil, nil
	}
	// CodeQL path-injection barrier: only MkdirAll allowlisted absolute paths.
	for _, d := range []string{homeDir, cargoHome, cacheHome, xdgConfig, xdgData} {
		if err := fsMkdirAll(d, 0o700); err != nil {
			return "", "", nil, nil
		}
	}
	binds = []string{homeDir}
	env = []string{
		"HOME=" + homeDir,
		"CARGO_HOME=" + cargoHome,
		"XDG_CACHE_HOME=" + cacheHome,
		"XDG_CONFIG_HOME=" + xdgConfig,
		"XDG_DATA_HOME=" + xdgData,
		"CARGO_NET_OFFLINE=false",
	}
	return cargoHome, cacheHome, binds, env
}

// uniqueBindPaths dedupes absolute paths for ExtraBind.
func uniqueBindPaths(paths ...string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		abs, err := filepath.Abs(p)
		if err != nil || abs == "" || abs == "/" {
			continue
		}
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}
		out = append(out, abs)
	}
	return out
}

// runCompilerBuild is a convenience for clang/afl-cc with stderr capture.
func runCompilerBuild(ctx context.Context, bin string, args []string, binds []string, wall time.Duration) (stderr string, err error) {
	var buf bytes.Buffer
	err = RunBuildIsolated(ctx, BuildIsolateOpts{
		Bin:       bin,
		Args:      args,
		ExtraBind: uniqueBindPaths(binds...),
		Wall:      wall,
		Stderr:    &buf,
	})
	return buf.String(), err
}
