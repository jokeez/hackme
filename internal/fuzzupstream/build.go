package fuzzupstream

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

var buildMu sync.Mutex

// BuildTarget clones upstream (if needed) and compiles ASAN stdin fuzz driver.
func BuildTarget(ctx context.Context, repoRoot string, t Target) (binPath, clonePath string, err error) {
	if _, err := exec.LookPath("clang"); err != nil {
		return "", "", fmt.Errorf("fuzzupstream: clang required")
	}
	if repoRoot == "" {
		repoRoot = "."
	}
	cacheDir := filepath.Join(repoRoot, ".cache", "oss-cve-clones")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", "", err
	}
	clonePath = filepath.Join(cacheDir, t.ID)
	if err := cloneRepo(ctx, t.Repo, t.Ref, clonePath); err != nil {
		return "", "", err
	}
	driverSrc := filepath.Join(repoRoot, "tasks", "sources", "fuzz", "oss", t.Driver+".c")
	if _, err := os.Stat(driverSrc); err != nil {
		return "", "", fmt.Errorf("fuzzupstream: driver %s: %w", driverSrc, err)
	}
	outDir := filepath.Join(repoRoot, ".cache", "oss-cve-bin")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(t.ID + t.Repo + t.Ref + t.Driver))
	binPath = filepath.Join(outDir, t.ID+"-"+hex.EncodeToString(sum[:6])+".bin")

	buildMu.Lock()
	defer buildMu.Unlock()
	if st, err := os.Stat(binPath); err == nil && st.Mode().IsRegular() {
		return binPath, clonePath, nil
	}

	tmpDir, err := os.MkdirTemp("", "oss-cve-build-*")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(tmpDir)
	tmpBin := filepath.Join(tmpDir, "fuzz")

	args := []string{
		"-fsanitize=address,undefined",
		"-fno-omit-frame-pointer",
		"-g", "-O1",
		"-o", tmpBin,
		driverSrc,
	}
	for _, inc := range t.IncludeDirs {
		args = append(args, "-I", filepath.Join(clonePath, inc))
	}
	for _, src := range t.UpstreamSrc {
		args = append(args, filepath.Join(clonePath, src))
	}

	buildCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(buildCtx, "clang", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", "", fmt.Errorf("clang build %s: %w (%s)", t.ID, err, strings.TrimSpace(stderr.String()))
	}
	in, err := os.ReadFile(tmpBin)
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(binPath, in, 0o755); err != nil {
		return "", "", err
	}
	return binPath, clonePath, nil
}

func cloneRepo(ctx context.Context, repo, ref, dest string) error {
	if _, err := os.Stat(filepath.Join(dest, ".git")); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	cloneCtx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cloneCtx, "git", "clone", "--depth", "1", "--branch", ref, repo, dest)
	if err := cmd.Run(); err != nil {
		// fallback: clone default branch
		_ = os.RemoveAll(dest)
		cmd2 := exec.CommandContext(cloneCtx, "git", "clone", "--depth", "1", repo, dest)
		if err2 := cmd2.Run(); err2 != nil {
			return fmt.Errorf("git clone %s: %w", repo, err)
		}
	}
	return nil
}
