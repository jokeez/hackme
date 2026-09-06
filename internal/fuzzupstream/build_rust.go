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
	"time"
)

const rustStdinBinName = "hunt_stdin"

// buildTargetRust clones upstream and builds a nightly AddressSanitizer stdin binary via cargo.
func buildTargetRust(ctx context.Context, repoRoot string, t Target) (binPath, clonePath string, err error) {
	if err := requireRustNightlyASAN(); err != nil {
		return "", "", err
	}
	cacheDir := filepath.Join(repoRoot, ".cache", "oss-cve-clones")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", "", err
	}
	clonePath = filepath.Join(cacheDir, t.ID)
	if err := cloneRepo(ctx, t.Repo, t.Ref, clonePath); err != nil {
		return "", "", err
	}
	driverSrc := DriverSourcePath(repoRoot, t)
	driverBytes, err := os.ReadFile(driverSrc)
	if err != nil {
		return "", "", fmt.Errorf("fuzzupstream: driver %s: %w", driverSrc, err)
	}
	if _, err := os.Stat(filepath.Join(clonePath, "Cargo.toml")); err != nil {
		return "", "", fmt.Errorf("fuzzupstream: rust target %s missing Cargo.toml in clone", t.ID)
	}

	outDir := filepath.Join(repoRoot, ".cache", "oss-cve-bin")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", "", err
	}
	pkg := strings.TrimSpace(t.CargoPackage)
	if pkg == "" {
		pkg = t.ID
	}
	sumInput := "rust|" + t.ID + "|" + t.Repo + "|" + t.Ref + "|" + t.Driver + "|" + pkg + "|" + hex.EncodeToString(sha256Sum(driverBytes))
	sum := sha256.Sum256([]byte(sumInput))
	binPath = filepath.Join(outDir, t.ID+"-"+hex.EncodeToString(sum[:6])+".bin")

	buildMu.Lock()
	defer buildMu.Unlock()
	if st, err := os.Stat(binPath); err == nil && st.Mode().IsRegular() {
		return binPath, clonePath, nil
	}

	tmpDir, err := os.MkdirTemp("", "oss-cve-rust-*")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(tmpDir)

	crateDir := filepath.Join(tmpDir, "crate")
	if err := os.MkdirAll(crateDir, 0o755); err != nil {
		return "", "", err
	}
	absClone, err := filepath.Abs(clonePath)
	if err != nil {
		return "", "", err
	}
	manifest := fmt.Sprintf(`[package]
name = "hunt_oss_%s_stdin"
version = "0.0.0"
edition = "2021"
publish = false

[[bin]]
name = "%s"
path = "main.rs"

[dependencies]
%s = { path = %q }
`, sanitizeCargoName(t.ID), rustStdinBinName, pkg, absClone)
	if err := os.WriteFile(filepath.Join(crateDir, "Cargo.toml"), []byte(manifest), 0o644); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(filepath.Join(crateDir, "main.rs"), driverBytes, 0o644); err != nil {
		return "", "", err
	}

	targetDir := filepath.Join(tmpDir, "target")
	buildCtx, cancel := context.WithTimeout(ctx, buildTimeout(t))
	defer cancel()
	cmd := exec.CommandContext(buildCtx, "cargo", "+nightly", "build", "--release", "--manifest-path", filepath.Join(crateDir, "Cargo.toml"))
	cmd.Dir = crateDir
	cmd.Env = append(os.Environ(),
		"CARGO_TERM_COLOR=never",
		"CARGO_TARGET_DIR="+targetDir,
		"RUSTFLAGS=-Zsanitizer=address",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stderr
	if err := cmd.Run(); err != nil {
		return "", "", fmt.Errorf("cargo rust build %s: %w (%s)", t.ID, err, strings.TrimSpace(stderr.String()))
	}
	tmpBin := filepath.Join(targetDir, "release", rustStdinBinName)
	in, err := os.ReadFile(tmpBin)
	if err != nil {
		return "", "", fmt.Errorf("cargo rust build %s: missing binary %s: %w", t.ID, tmpBin, err)
	}
	if err := os.WriteFile(binPath, in, 0o755); err != nil {
		return "", "", err
	}
	return binPath, clonePath, nil
}

func requireRustNightlyASAN() error {
	if _, err := exec.LookPath("cargo"); err != nil {
		return fmt.Errorf("fuzzupstream: cargo required for rust targets")
	}
	if _, err := exec.LookPath("rustc"); err != nil {
		return fmt.Errorf("fuzzupstream: rustc required for rust targets")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "rustc", "+nightly", "--version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("fuzzupstream: rustc +nightly required for rust ASAN (rustup toolchain install nightly): %w", err)
	}
	return nil
}

// RustNightlyASANAvailable reports whether cargo +nightly is usable for ASAN builds.
func RustNightlyASANAvailable() bool {
	return requireRustNightlyASAN() == nil
}

func sha256Sum(b []byte) []byte {
	sum := sha256.Sum256(b)
	return sum[:]
}

func sanitizeCargoName(id string) string {
	s := strings.ToLower(strings.TrimSpace(id))
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		default:
			return '_'
		}
	}, s)
	s = strings.Trim(s, "_")
	if s == "" {
		return "target"
	}
	return s
}
