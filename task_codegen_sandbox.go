package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

var fromCodeHostCompileWarned sync.Once

func fromCodeRequireSandbox() bool {
	return envBool("HACKME_FROM_CODE_REQUIRE_SANDBOX", false)
}

// fromCodeEnabled gates host compile for POST /api/tasks/from_code and
// security-audit language+code. Explicit HACKME_FROM_CODE overrides;
// unset defaults to enabled only on loopback bind (local lab), disabled on
// public bind addresses.
func fromCodeEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_FROM_CODE")))
	switch v {
	case "0", "false", "no", "off":
		return false
	case "1", "true", "yes", "on":
		return true
	default:
		return bindAddrAllowsBeginnerSolo(effectiveHTTPBindAddr())
	}
}

func lookPathCompilerSandbox() (bin, kind string) {
	if p, err := exec.LookPath("bwrap"); err == nil {
		return p, "bwrap"
	}
	if p, err := exec.LookPath("nsjail"); err == nil {
		return p, "nsjail"
	}
	return "", ""
}

// wrapCompilerCmd runs from_code compilers under bwrap/nsjail when available.
// HACKME_FROM_CODE_REQUIRE_SANDBOX=1 fails closed if neither binary is on PATH.
// Without a sandbox helper, host compile is lab-only (see docs/SECURITY.md).
//
// Sandbox mounts are narrow (toolchain + workdir): never whole-root RO bind,
// which would allow include_str!("/etc/...") exfil into compile_log.
func wrapCompilerCmd(ctx context.Context, workDir, outPath string, inner *exec.Cmd) (*exec.Cmd, error) {
	if inner == nil {
		return nil, errors.New("from_code: nil compiler command")
	}
	bin, kind := lookPathCompilerSandbox()
	require := fromCodeRequireSandbox()
	if bin == "" {
		if require {
			return nil, errors.New("from_code: HACKME_FROM_CODE_REQUIRE_SANDBOX=1 but neither bwrap nor nsjail found on PATH (host compile is lab-only)")
		}
		fromCodeHostCompileWarned.Do(func() {
			fmt.Fprintln(os.Stderr, "hackme: from_code host compile without bwrap/nsjail — lab-only; set HACKME_FROM_CODE_REQUIRE_SANDBOX=1 to fail-closed")
		})
		return inner, nil
	}
	workDir = filepath.Clean(workDir)
	outParent := filepath.Clean(filepath.Dir(outPath))
	chdir := inner.Dir
	if chdir == "" {
		chdir = workDir
	}
	path := inner.Path
	if path == "" && len(inner.Args) > 0 {
		path = inner.Args[0]
	}
	argsTail := []string(nil)
	if len(inner.Args) > 1 {
		argsTail = append(argsTail, inner.Args[1:]...)
	}

	extraBinds := uniqueCleanDirs(workDir, outParent)
	for _, kv := range inner.Env {
		if i := strings.IndexByte(kv, '='); i > 0 {
			k, v := kv[:i], filepath.Clean(kv[i+1:])
			if v == "" || v == "/" || v == "." {
				continue
			}
			switch k {
			case "GOCACHE", "XDG_CACHE_HOME", "ZIG_GLOBAL_CACHE_DIR", "ZIG_LOCAL_CACHE_DIR":
				extraBinds = append(extraBinds, v)
			case "HOME":
				// Only RW-bind HOME when it is the compile workdir (isolated).
				// Never mount a real operator HOME (secrets) into the sandbox.
				if v == workDir || strings.HasPrefix(v, workDir+string(os.PathSeparator)) {
					extraBinds = append(extraBinds, v)
				}
			}
		}
	}
	extraBinds = uniqueCleanDirs(extraBinds...)
	roBinds := compilerSandboxROBinds(inner, path)

	var wrapped *exec.Cmd
	switch kind {
	case "bwrap":
		bargs := []string{
			"--die-with-parent",
			"--unshare-net",
			"--dev", "/dev",
			"--proc", "/proc",
			"--tmpfs", "/tmp",
			"--dir", "/etc",
		}
		for _, d := range roBinds {
			bargs = append(bargs, "--ro-bind-try", d, d)
		}
		for _, d := range extraBinds {
			bargs = append(bargs, "--bind", d, d)
		}
		bargs = append(bargs, "--chdir", chdir, "--", path)
		bargs = append(bargs, argsTail...)
		wrapped = exec.CommandContext(ctx, bin, bargs...)
	case "nsjail":
		// Default nsjail clones a new net namespace (no host net). Keep that.
		// Narrow RO mounts only — never whole-root bindmount_ro /.
		nargs := []string{"-q", "--cwd", chdir, "--time_limit", "120"}
		for _, d := range roBinds {
			nargs = append(nargs, "--bindmount_ro", d)
		}
		for _, d := range extraBinds {
			nargs = append(nargs, "--bindmount", d)
		}
		nargs = append(nargs, "--", path)
		nargs = append(nargs, argsTail...)
		wrapped = exec.CommandContext(ctx, bin, nargs...)
	default:
		return inner, nil
	}
	wrapped.Env = inner.Env
	return wrapped, nil
}

// compilerSandboxROBinds lists host paths needed by compilers without exposing
// the full filesystem (esp. /etc secrets readable via include_str!).
func compilerSandboxROBinds(inner *exec.Cmd, compilerPath string) []string {
	var out []string
	for _, p := range []string{"/usr", "/lib", "/lib64", "/bin", "/sbin"} {
		if pathExists(p) {
			out = append(out, p)
		}
	}
	for _, p := range []string{"/etc/ld.so.cache", "/etc/alternatives"} {
		if pathExists(p) {
			out = append(out, p)
		}
	}

	envMap := envMapFromSlice(nil)
	if inner != nil && len(inner.Env) > 0 {
		envMap = envMapFromSlice(inner.Env)
	}
	for _, key := range []string{
		"RUSTUP_HOME", "CARGO_HOME", "TINYGOROOT", "WASMTIME_HOME",
		"ZIG_GLOBAL_CACHE_DIR", "ZIG_LOCAL_CACHE_DIR",
	} {
		if v := strings.TrimSpace(envMap[key]); v != "" {
			out = append(out, filepath.Clean(v))
		}
	}
	// Portable toolchains under HACKME_PREFIX (never bind the whole prefix —
	// it may hold .env.vps secrets).
	prefix := strings.TrimSpace(envMap["HACKME_PREFIX"])
	if prefix == "" {
		prefix = "/opt/hackme"
	}
	prefix = filepath.Clean(prefix)
	if prefix != "/" && pathExists(prefix) {
		for _, sub := range []string{
			".rustup", ".cargo", "tinygo", "wabt", "nodejs", "bin",
			".npm-global", "zig", "toolchains",
		} {
			p := filepath.Join(prefix, sub)
			if pathExists(p) {
				out = append(out, p)
			}
		}
	}

	if compilerPath != "" {
		if abs, err := exec.LookPath(compilerPath); err == nil {
			out = append(out, abs)
			out = append(out, filepath.Dir(abs))
		} else if filepath.IsAbs(compilerPath) && pathExists(compilerPath) {
			out = append(out, compilerPath)
			out = append(out, filepath.Dir(compilerPath))
		}
	}

	// PATH entries that look like toolchain installs (skip broad roots).
	pathEnv := envMap["PATH"]
	if pathEnv == "" {
		pathEnv = os.Getenv("PATH")
	}
	for _, dir := range filepath.SplitList(pathEnv) {
		dir = filepath.Clean(strings.TrimSpace(dir))
		if dir == "" || dir == "." || dir == "/" {
			continue
		}
		if !pathExists(dir) {
			continue
		}
		if isBroadFSRoot(dir) {
			continue
		}
		if looksLikeToolchainDir(dir) {
			out = append(out, dir)
		}
	}

	return uniqueCleanDirs(out...)
}

func envMapFromSlice(env []string) map[string]string {
	m := make(map[string]string, 32)
	src := env
	if len(src) == 0 {
		src = os.Environ()
	}
	for _, kv := range src {
		if i := strings.IndexByte(kv, '='); i > 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	return m
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func isBroadFSRoot(dir string) bool {
	switch dir {
	case "/", "/etc", "/home", "/root", "/var", "/opt", "/tmp", "/dev", "/proc", "/sys":
		return true
	default:
		return false
	}
}

func looksLikeToolchainDir(dir string) bool {
	base := strings.ToLower(filepath.Base(dir))
	switch base {
	case "bin", "sbin", "lib", "lib64", "include", "tinygo", "wabt", "nodejs",
		"node", "zig", "rustup", "cargo", "toolchains", ".cargo", ".rustup",
		".npm-global", "npm-global":
		return true
	}
	low := strings.ToLower(dir)
	for _, tip := range []string{
		"/rustup", "/.rustup", "/cargo", "/.cargo", "/tinygo", "/wabt",
		"/nodejs", "/zig", "/npm-global", "/.npm-global", "/llvm", "/clang",
		"/toolchains",
	} {
		if strings.Contains(low, tip) {
			return true
		}
	}
	return false
}

func uniqueCleanDirs(dirs ...string) []string {
	seen := make(map[string]struct{}, len(dirs))
	out := make([]string, 0, len(dirs))
	for _, d := range dirs {
		d = filepath.Clean(strings.TrimSpace(d))
		if d == "" || d == "." || d == "/" {
			continue
		}
		if _, ok := seen[d]; ok {
			continue
		}
		seen[d] = struct{}{}
		out = append(out, d)
	}
	return out
}
