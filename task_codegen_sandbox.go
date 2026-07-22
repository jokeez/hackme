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
			switch k {
			case "HOME", "GOCACHE", "XDG_CACHE_HOME", "ZIG_GLOBAL_CACHE_DIR", "ZIG_LOCAL_CACHE_DIR":
				if v != "" && v != "/" && v != "." {
					extraBinds = append(extraBinds, v)
				}
			}
		}
	}
	extraBinds = uniqueCleanDirs(extraBinds...)

	var wrapped *exec.Cmd
	switch kind {
	case "bwrap":
		bargs := []string{
			"--die-with-parent",
			"--unshare-net",
			"--ro-bind", "/", "/",
			"--dev", "/dev",
			"--proc", "/proc",
			"--tmpfs", "/tmp",
		}
		for _, d := range extraBinds {
			bargs = append(bargs, "--bind", d, d)
		}
		bargs = append(bargs, "--chdir", chdir, "--", path)
		bargs = append(bargs, argsTail...)
		wrapped = exec.CommandContext(ctx, bin, bargs...)
	case "nsjail":
		// Default nsjail clones a new net namespace (no host net). Keep that.
		nargs := []string{"-q", "--cwd", chdir, "--bindmount_ro", "/", "--time_limit", "120"}
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
