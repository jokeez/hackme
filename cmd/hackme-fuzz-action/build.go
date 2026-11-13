package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const buildTimeout = 5 * time.Minute

// buildGuard compiles a single-file guard (WASM ABI v1, see
// docs/TASK_LANGUAGES.md) into tmpDir. Flags mirror scripts/build_task_wasm.sh.
// The compiler is exec'd directly with fixed arguments — no shell — and the
// source must live inside the workspace.
func buildGuard(ctx context.Context, lang, workspace, source, tmpDir string) (string, error) {
	lang = strings.TrimSpace(strings.ToLower(lang))
	src, err := resolveUnder(workspace, source)
	if err != nil {
		return "", usageErr("source: %v", err)
	}
	st, err := os.Stat(src)
	if err != nil {
		return "", usageErr("source: %v", err)
	}
	if !st.Mode().IsRegular() {
		return "", usageErr("source %s is not a regular file", source)
	}
	out := filepath.Join(tmpDir, "guard.wasm")

	var name string
	var args []string
	clangArgs := []string{"--target=wasm32", "-O3", "-nostdlib",
		"-Wl,--no-entry", "-Wl,--export=check", "-Wl,--export-if-defined=check_bytes", "-Wl,--strip-all",
		"-o", out}
	switch lang {
	case "rust", "rs":
		// strip + panic=abort: a std guard is ~1.4 MiB otherwise, far over the pool cap.
		name, args = "rustc", []string{"--target", "wasm32-unknown-unknown", "-O",
			"-C", "strip=symbols", "-C", "panic=abort", "--crate-type=cdylib", "-o", out, "--", src}
	case "c", "gcc":
		name, args = "clang", append(clangArgs, "-x", "c", "--", src)
	case "cpp", "c++", "cxx":
		name, args = "clang", append(clangArgs, "-x", "c++", "--", src)
	default:
		return "", usageErr("language %q: use rust, c or cpp", lang)
	}

	bctx, cancel := context.WithTimeout(ctx, buildTimeout)
	defer cancel()
	cmd := exec.CommandContext(bctx, name, args...)
	cmd.Dir = tmpDir
	b, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(b))
		if len(msg) > 4000 {
			msg = msg[:4000] + "…"
		}
		return "", fmt.Errorf("build %s: %v\n%s", lang, err, msg)
	}
	return out, nil
}
