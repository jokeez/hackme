package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"hackme/internal/fuzzingcli"
	"hackme/internal/sandbox"
)

// packBuildTimeout caps a rustc build of a shipped guard pack source.
const packBuildTimeout = 3 * time.Minute

// resolveUnder resolves p (relative to root, or absolute) through symlinks and
// refuses anything that lands outside root. Returns the resolved absolute path.
func resolveUnder(root, p string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", errors.New("empty root")
	}
	if strings.TrimSpace(p) == "" {
		return "", errors.New("empty path")
	}
	if strings.ContainsRune(p, 0) {
		return "", errors.New("path contains NUL")
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}
	realRoot, err = filepath.Abs(realRoot)
	if err != nil {
		return "", err
	}
	full := p
	if !filepath.IsAbs(full) {
		full = filepath.Join(realRoot, full)
	}
	real, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", err
	}
	real, err = filepath.Abs(real)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(realRoot, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%s is outside %s", p, root)
	}
	return real, nil
}

// readWasmUnder reads a regular file confined to root, refusing anything larger
// than the pool's wasm cap so a huge artifact cannot balloon the runner.
func readWasmUnder(root, p string) ([]byte, error) {
	real, err := resolveUnder(root, p)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(real)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", p)
	}
	limit := int64(sandbox.MaxCheckWasmBytes())
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("%s is larger than the pool wasm cap (%d bytes)", p, limit)
	}
	return raw, nil
}

// loadPackWasm returns the shipped wasm for a guard pack. Packs that ship only
// Rust source are compiled with rustc into tmpDir (the hackme tree is not touched).
func loadPackWasm(ctx context.Context, hackmeRoot, tmpDir string, pack fuzzingcli.GuardPack) ([]byte, error) {
	if raw, err := readWasmUnder(hackmeRoot, pack.WasmRelPath); err == nil {
		return raw, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("pack %s: %w", pack.ID, err)
	}
	if strings.TrimSpace(pack.SourceRelPath) == "" {
		return nil, fmt.Errorf("pack %s: no shipped wasm and no source to build", pack.ID)
	}
	src, err := resolveUnder(hackmeRoot, pack.SourceRelPath)
	if err != nil {
		return nil, fmt.Errorf("pack %s source: %w", pack.ID, err)
	}
	if filepath.Ext(src) != ".rs" {
		return nil, fmt.Errorf("pack %s: only Rust pack sources are built here (%s)", pack.ID, filepath.Base(src))
	}
	out := filepath.Join(tmpDir, pack.ID+".wasm")
	bctx, cancel := context.WithTimeout(ctx, packBuildTimeout)
	defer cancel()
	cmd := exec.CommandContext(bctx, "rustc", "--target", "wasm32-unknown-unknown", "-O",
		"--crate-type=cdylib", src, "-o", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("pack %s: rustc: %v: %s", pack.ID, err, strings.TrimSpace(string(b)))
	}
	return readWasmUnder(tmpDir, out)
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
