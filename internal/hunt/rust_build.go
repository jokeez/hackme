package hunt

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const rustStdinBin = "hunt_stdin"

var reFuzzTargetBody = regexp.MustCompile(`(?s)fuzz_target!\s*\(\s*\|\s*(\w+)\s*:\s*&\[u8\]\s*\|\s*\{(.*)\}\s*\)\s*;`)

type rustHarnessPlan struct {
	Mode        string // cargo_fuzz | stdin_fuzz_target | stdin_package
	FuzzTarget  string
	PackageName string
	CargoRoot   string
}

// BuildInventoryRustHarness compiles a Rust ASAN stdin harness from pinned inventory (Phase B).
func BuildInventoryRustHarness(ctx context.Context, repoRoot string, req HarnessBuildRequest) (*HarnessBuildResult, error) {
	if req.Pin == nil || strings.TrimSpace(req.Pin.Path) == "" {
		return nil, fmt.Errorf("hunt rust build: pin required")
	}
	sourceRel := strings.TrimSpace(req.SourceRel)
	if sourceRel == "" {
		return nil, fmt.Errorf("hunt rust build: source_rel required")
	}
	if repoRoot == "" {
		repoRoot = RepoRoot()
	}
	srcPath, err := resolveSourceFile(repoRoot, req.Pin.Path, sourceRel)
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(srcPath)
	if err != nil {
		return nil, err
	}
	if err := requireRustNightlyASAN(); err != nil {
		return nil, err
	}
	plan, err := planRustHarness(req.Pin.Path, sourceRel, content)
	if err != nil {
		return nil, err
	}
	hash := InventoryHarnessHash(req.Pin.CommitSHA, sourceRel, content)
	cachePath := filepath.Join(repoRoot, ".cache", "hunt-harness", hash+".bin")
	if st, err := os.Stat(cachePath); err == nil && st.Mode().IsRegular() {
		harnessCache.Store(hash, cachePath)
		return &HarnessBuildResult{
			HarnessHash: hash,
			BinaryPath:  cachePath,
			SourceRel:   sourceRel,
			Language:    "rust",
			PinSHA:      req.Pin.CommitSHA,
			BuildOK:     true,
			Note:        "cached rust harness",
		}, nil
	}
	var binPath string
	var note string
	switch plan.Mode {
	case "cargo_fuzz":
		binPath, note, err = buildCargoFuzzHarness(ctx, plan)
	case "stdin_fuzz_target":
		binPath, note, err = buildRustStdinFromFuzzTarget(ctx, req.Pin.Path, sourceRel, content, plan)
	default:
		if !req.TemplateAccept {
			return nil, fmt.Errorf("hunt rust build: no fuzz_target!/cargo-fuzz — set template_accept=true for stdin package driver")
		}
		binPath, note, err = buildRustStdinPackageDriver(ctx, req.Pin.Path, sourceRel, plan)
	}
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return nil, err
	}
	in, err := os.ReadFile(binPath)
	if err != nil {
		return nil, err
	}
	tmp := cachePath + ".tmp"
	if err := os.WriteFile(tmp, in, 0o755); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, cachePath); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	harnessCache.Store(hash, cachePath)
	return &HarnessBuildResult{
		HarnessHash: hash,
		BinaryPath:  cachePath,
		SourceRel:   sourceRel,
		Language:    "rust",
		PinSHA:      req.Pin.CommitSHA,
		BuildOK:     true,
		Note:        note,
	}, nil
}

func planRustHarness(pinPath, sourceRel string, content []byte) (*rustHarnessPlan, error) {
	src := string(content)
	base := strings.TrimSuffix(filepath.Base(sourceRel), filepath.Ext(sourceRel))
	cargoRoot := findCargoRoot(pinPath, sourceRel)
	plan := &rustHarnessPlan{
		FuzzTarget:  base,
		CargoRoot:   cargoRoot,
		PackageName: readCargoPackageName(cargoRoot),
	}
	fuzzDir := filepath.Join(pinPath, "fuzz")
	if st, err := os.Stat(filepath.Join(fuzzDir, "Cargo.toml")); err == nil && !st.IsDir() {
		plan.Mode = "cargo_fuzz"
		if target := cargoFuzzTargetName(sourceRel); target != "" {
			plan.FuzzTarget = target
		}
		return plan, nil
	}
	if strings.Contains(src, inventoryMarkerRust) || strings.Contains(src, "libfuzzer_sys::fuzz_target") {
		plan.Mode = "stdin_fuzz_target"
		return plan, nil
	}
	plan.Mode = "stdin_package"
	return plan, nil
}

func findCargoRoot(pinPath, sourceRel string) string {
	dir := filepath.Dir(filepath.Join(pinPath, sourceRel))
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "Cargo.toml")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return pinPath
}

func readCargoPackageName(cargoRoot string) string {
	b, err := os.ReadFile(filepath.Join(cargoRoot, "Cargo.toml"))
	if err != nil {
		return "hunt_crate"
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name") && strings.Contains(line, "=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				return strings.Trim(strings.TrimSpace(parts[1]), `"`)
			}
		}
	}
	return "hunt_crate"
}

func cargoFuzzTargetName(sourceRel string) string {
	base := filepath.Base(sourceRel)
	if strings.HasPrefix(filepath.ToSlash(sourceRel), "fuzz/fuzz_targets/") {
		return strings.TrimSuffix(base, filepath.Ext(base))
	}
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func buildCargoFuzzHarness(ctx context.Context, plan *rustHarnessPlan) (binPath, note string, err error) {
	if plan.CargoRoot == "" {
		return "", "", fmt.Errorf("hunt rust: cargo root missing")
	}
	buildCtx, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()
	cmd := exec.CommandContext(buildCtx, "cargo", "+nightly", "fuzz", "build", plan.FuzzTarget, "--sanitizer=address")
	cmd.Dir = plan.CargoRoot
	cmd.Env = append(os.Environ(), "RUSTFLAGS=-Zsanitizer=address", "CARGO_TERM_COLOR=never")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stderr
	if err := cmd.Run(); err != nil {
		return "", "", fmt.Errorf("hunt rust cargo fuzz build %s: %w (%s)", plan.FuzzTarget, err, strings.TrimSpace(stderr.String()))
	}
	candidates := []string{
		filepath.Join(plan.CargoRoot, "fuzz", "target", plan.FuzzTarget+"/release", plan.FuzzTarget),
		filepath.Join(plan.CargoRoot, "fuzz", "target", "x86_64-unknown-linux-gnu", "release", plan.FuzzTarget),
		filepath.Join(plan.CargoRoot, "target", "release", plan.FuzzTarget),
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, fmt.Sprintf("cargo-fuzz ASAN harness (%s)", plan.FuzzTarget), nil
		}
	}
	return "", "", fmt.Errorf("hunt rust: cargo fuzz binary not found for %s", plan.FuzzTarget)
}

func buildRustStdinFromFuzzTarget(ctx context.Context, pinPath, sourceRel string, content []byte, plan *rustHarnessPlan) (binPath, note string, err error) {
	body, ok := extractFuzzTargetBody(string(content))
	if !ok {
		return "", "", fmt.Errorf("hunt rust: could not parse fuzz_target! body in %s", sourceRel)
	}
	tmpDir, err := os.MkdirTemp("", "hunt-rust-fuzz-*")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(tmpDir)
	crateDir := filepath.Join(tmpDir, "crate")
	if err := os.MkdirAll(crateDir, 0o755); err != nil {
		return "", "", err
	}
	mainRS := fmt.Sprintf(`use std::io::Read;

fn main() {
	let mut data = Vec::new();
	let _ = std::io::stdin().take(65536).read_to_end(&mut data);
	if data.is_empty() {
		return;
	}
	fuzz_body(&data);
}

fn fuzz_body(data: &[u8]) {
%s
}
`, body)
	manifest := fmt.Sprintf(`[package]
name = "hunt_inv_rust"
version = "0.0.0"
edition = "2021"
publish = false

[[bin]]
name = "%s"
path = "main.rs"
`, rustStdinBin)
	if plan.PackageName != "" && plan.CargoRoot != "" && plan.CargoRoot != pinPath {
		abs, _ := filepath.Abs(plan.CargoRoot)
		manifest += fmt.Sprintf("\n[dependencies]\n%s = { path = %q }\n", plan.PackageName, abs)
	}
	if err := os.WriteFile(filepath.Join(crateDir, "Cargo.toml"), []byte(manifest), 0o644); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(filepath.Join(crateDir, "main.rs"), []byte(mainRS), 0o644); err != nil {
		return "", "", err
	}
	return compileAndStageRustBin(ctx, crateDir, "stdin fuzz_target ASAN harness")
}

func buildRustStdinPackageDriver(ctx context.Context, pinPath, sourceRel string, plan *rustHarnessPlan) (binPath, note string, err error) {
	tmpDir, err := os.MkdirTemp("", "hunt-rust-pkg-*")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(tmpDir)
	crateDir := filepath.Join(tmpDir, "crate")
	if err := os.MkdirAll(crateDir, 0o755); err != nil {
		return "", "", err
	}
	absPin, _ := filepath.Abs(plan.CargoRoot)
	mainRS := fmt.Sprintf(`use std::io::Read;

fn main() {
	let mut buf = Vec::new();
	let _ = std::io::stdin().take(65536).read_to_end(&mut buf);
	if buf.is_empty() {
		return;
	}
	// Phase B package driver — link pinned crate; extend with target-specific hooks as needed.
	let _ = %s;
	let _ = buf.len();
}
`, plan.PackageName)
	manifest := fmt.Sprintf(`[package]
name = "hunt_inv_pkg"
version = "0.0.0"
edition = "2021"
publish = false

[[bin]]
name = "%s"
path = "main.rs"

[dependencies]
%s = { path = %q }
`, rustStdinBin, plan.PackageName, absPin)
	if err := os.WriteFile(filepath.Join(crateDir, "Cargo.toml"), []byte(manifest), 0o644); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(filepath.Join(crateDir, "main.rs"), []byte(mainRS), 0o644); err != nil {
		return "", "", err
	}
	_ = sourceRel
	_ = pinPath
	return compileAndStageRustBin(ctx, crateDir, "stdin package ASAN driver (Phase B)")
}

func compileAndStageRustBin(ctx context.Context, crateDir, note string) (binPath, noteOut string, err error) {
	bin, noteOut, err := compileRustStdinCrate(ctx, crateDir, note)
	if err != nil {
		return "", "", err
	}
	staged, err := stageRustBinary(bin)
	if err != nil {
		return "", "", err
	}
	return staged, noteOut, nil
}

func stageRustBinary(srcBin string) (string, error) {
	data, err := os.ReadFile(srcBin)
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "hunt-rust-bin-")
	if err != nil {
		return "", err
	}
	path := f.Name()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if err := os.Chmod(path, 0o755); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func compileRustStdinCrate(ctx context.Context, crateDir, note string) (binPath, noteOut string, err error) {
	targetDir := filepath.Join(filepath.Dir(crateDir), "target")
	buildCtx, cancel := context.WithTimeout(ctx, 300*time.Second)
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
		return "", "", fmt.Errorf("hunt rust cargo build: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	bin := filepath.Join(targetDir, "release", rustStdinBin)
	if st, err := os.Stat(bin); err != nil || st.IsDir() {
		return "", "", fmt.Errorf("hunt rust: missing binary %s", bin)
	}
	return bin, note, nil
}

func extractFuzzTargetBody(src string) (string, bool) {
	m := reFuzzTargetBody.FindStringSubmatch(src)
	if len(m) < 3 {
		return "", false
	}
	body := strings.TrimSpace(m[2])
	if body == "" {
		return "", false
	}
	return body, true
}

func requireRustNightlyASAN() error {
	if _, err := exec.LookPath("cargo"); err != nil {
		return fmt.Errorf("hunt rust: cargo required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "rustc", "+nightly", "--version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("hunt rust: rustc +nightly required (rustup toolchain install nightly): %w", err)
	}
	return nil
}
