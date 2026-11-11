package hunt

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"hackme/internal/fuzzupstream"
)

// CoverageHarnessOpts resolves showmap/AFL stdin harnesses for catalog or published inventory.
type CoverageHarnessOpts struct {
	HarnessHash          string
	HarnessFetchURL      string
	HarnessContentSHA256 string
	ArtifactDB           *sql.DB
	// CoverageHarnessHash is a published afl-cc inventory binary (coordinator showmap).
	CoverageHarnessHash string
	// PinPath + SourceRel enable afl-cc rebuild of inventory sources for showmap edges.
	PinPath        string
	SourceRel      string
	TemplateAccept bool
}

// CoverageHarnessHash derives the artifact id for an afl-cc inventory showmap binary
// bound to the published ASAN harness_hash (syncs to coordinator without pin path).
func CoverageHarnessHash(asanHash string) string {
	asanHash = strings.TrimSpace(strings.ToLower(asanHash))
	if asanHash == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("afl-showmap-inv|" + asanHash + "|" + stdinWrapperFingerprint))
	return hex.EncodeToString(sum[:16])
}

// LookAFLClangFast resolves afl-clang-fast / afl-cc for instrumented Hunt builds.
func LookAFLClangFast() (string, error) {
	for _, name := range []string{"afl-clang-fast", "afl-cc", "afl-clang"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
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
			"aflpp/bin/afl-clang-fast",
			"aflpp/bin/afl-cc",
			"bin/afl-clang-fast",
			"bin/afl-cc",
		} {
			p := filepath.Join(pref, rel)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("hunt: afl-clang-fast/afl-cc not found (install_hunt_engines.sh)")
}

// BuildAFLInstrumentedHarness compiles the catalog stdin driver with afl-cc + ASAN.
func BuildAFLInstrumentedHarness(ctx context.Context, repoRoot, targetID string) (binPath string, err error) {
	if repoRoot == "" {
		repoRoot = RepoRoot()
	}
	id, ok := fuzzupstream.SanitizeCatalogID(targetID)
	if !ok {
		return "", fmt.Errorf("hunt: invalid target id %q", targetID)
	}
	manifest, err := fuzzupstream.LoadManifest(repoRoot)
	if err != nil {
		return "", err
	}
	t, err := manifest.TargetByID(id)
	if err != nil {
		return "", err
	}
	binPath, _, err = fuzzupstream.BuildTargetAFL(ctx, repoRoot, t)
	return binPath, err
}

// BuildInventoryAFLHarness compiles inventory sources with afl-cc + AFL_USE_ASAN for showmap/AFL++.
// Cache key is harness_hash (same fingerprint as the published ASAN binary) + wrapper stamp.
func BuildInventoryAFLHarness(ctx context.Context, repoRoot string, req HarnessBuildRequest) (binPath string, err error) {
	if req.Pin == nil || strings.TrimSpace(req.Pin.Path) == "" {
		return "", fmt.Errorf("hunt afl inventory: pin required")
	}
	sourceRel := strings.TrimSpace(req.SourceRel)
	if sourceRel == "" {
		return "", fmt.Errorf("hunt afl inventory: source_rel required")
	}
	if SourceLanguage(sourceRel) == "rust" {
		return "", fmt.Errorf("hunt afl inventory: C/C++ only")
	}
	if repoRoot == "" {
		repoRoot = RepoRoot()
	}
	aflCC, err := LookAFLClangFast()
	if err != nil {
		return "", err
	}
	srcPath, err := resolveSourceFile(repoRoot, req.Pin.Path, sourceRel)
	if err != nil {
		return "", err
	}
	content, err := SafeReadFileUnder(req.Pin.Path, srcPath)
	if err != nil {
		return "", err
	}
	hasEntry := strings.Contains(string(content), inventoryMarker)
	if !hasEntry && !req.TemplateAccept {
		return "", fmt.Errorf("hunt afl inventory: LLVMFuzzerTestOneInput missing — set template_accept")
	}
	hash := InventoryHarnessHash(req.Pin.CommitSHA, sourceRel, content)
	if err := ValidateHexHash(hash); err != nil {
		return "", err
	}
	outDir := filepath.Join(repoRoot, ".cache", "hunt-afl-bin")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte("inv-aflasan|" + hash + "|" + stdinWrapperFingerprint))
	binPath = filepath.Join(outDir, "inv-"+hash+"-aflasan-"+hex.EncodeToString(sum[:6])+".bin")
	if st, err := os.Stat(binPath); err == nil && st.Mode().IsRegular() {
		return binPath, nil
	}

	plan, err := planInventoryCompile(req.Pin.Path, sourceRel)
	if err != nil {
		return "", err
	}
	tmpDir, err := os.MkdirTemp("", "hunt-inv-afl-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)
	wrapperPath := filepath.Join(tmpDir, "hunt_wrapper.c")
	if err := os.WriteFile(wrapperPath, []byte(stdinFuzzerWrapper), 0o644); err != nil {
		return "", err
	}
	tmpBin := filepath.Join(tmpDir, "harness.bin")
	args := []string{
		"-fno-omit-frame-pointer",
		"-g", "-O1",
		"-o", tmpBin,
		wrapperPath,
		plan.MainAbs,
	}
	args = append(args, plan.CompanionAbs...)
	for _, inc := range plan.IncludeDirs {
		args = append(args, "-I", inc)
	}
	if plan.Language == "cpp" {
		// afl-clang-fast++ when available; else force C++ via driver + -x.
		if p, lerr := exec.LookPath("afl-clang-fast++"); lerr == nil {
			aflCC = p
		} else if p, lerr := exec.LookPath("afl-c++"); lerr == nil {
			aflCC = p
		}
		args = append([]string{"-std=c++17"}, args...)
	} else {
		args = append([]string{"-std=c11"}, args...)
	}

	buildCtx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	binds := []string{tmpDir, req.Pin.Path, filepath.Dir(aflCC)}
	binds = append(binds, plan.IncludeDirs...)
	binds = append(binds, plan.CompanionAbs...)
	var stderr bytes.Buffer
	err = RunBuildIsolated(buildCtx, BuildIsolateOpts{
		Bin:  aflCC,
		Args: args,
		Env: append(ScrubBuildEnv(os.Environ()),
			"AFL_USE_ASAN=1",
			"AFL_QUIET=1",
			"AFL_PATH="+filepath.Dir(aflCC),
		),
		// filterSecretPrefixRoots applied inside RunBuildIsolated (never RW-mount /opt/hackme).
		ExtraBind: uniqueBindPaths(binds...),
		Wall:      180 * time.Second,
		Stderr:    &stderr,
	})
	if err != nil {
		return "", fmt.Errorf("hunt afl inventory: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	in, err := os.ReadFile(tmpBin)
	if err != nil {
		return "", err
	}
	tmp := binPath + ".tmp"
	if err := os.WriteFile(tmp, in, 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, binPath); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return binPath, nil
}

// ResolveCoverageHarness prefers an afl-cc instrumented binary for showmap/AFL++.
// Catalog targets use BuildTargetAFL; inventory rebuilds from pin+source_rel when available
// (published ASAN alone yields empty showmap — honest floor).
func ResolveCoverageHarness(ctx context.Context, repoRoot, targetID string, opts ...CoverageHarnessOpts) (path string, instrumented bool, err error) {
	if repoRoot == "" {
		repoRoot = RepoRoot()
	}
	var o CoverageHarnessOpts
	if len(opts) > 0 {
		o = opts[0]
	}
	// Catalog AFL instrumentation when target is in the OSS manifest.
	if bin, berr := BuildAFLInstrumentedHarness(ctx, repoRoot, targetID); berr == nil && bin != "" {
		return bin, true, nil
	} else if berr != nil {
		err = berr
	}
	// Published inventory afl-cc binary (synced to coordinator with campaign).
	covHash := strings.TrimSpace(o.CoverageHarnessHash)
	if covHash == "" && strings.TrimSpace(o.HarnessHash) != "" {
		covHash = CoverageHarnessHash(o.HarnessHash)
	}
	if covHash != "" {
		fetch := o.HarnessFetchURL
		if fetch == "" || !strings.Contains(fetch, covHash) {
			fetch = HarnessFetchURL(covHash)
		}
		if p, merr := MaterializeHarness(ctx, repoRoot, covHash, fetch, "", o.ArtifactDB); merr == nil && p != "" {
			return p, true, nil
		} else if merr != nil && err == nil {
			err = merr
		}
	}
	// Inventory: afl-cc rebuild from pinned sources (real edges for pay prove).
	pinPath := strings.TrimSpace(o.PinPath)
	sourceRel := strings.TrimSpace(o.SourceRel)
	if pinPath != "" && sourceRel != "" {
		if bin, berr := BuildInventoryAFLHarness(ctx, repoRoot, HarnessBuildRequest{
			Pin:            &RepoPinResult{Path: pinPath},
			SourceRel:      sourceRel,
			TemplateAccept: true, // showmap rebuild may lack LLVMFuzzer marker on stdin drivers
		}); berr == nil && bin != "" {
			return bin, true, nil
		} else if berr != nil && err == nil {
			err = berr
		}
	}
	// Published inventory / pool harness (content_sha256-bound) — not AFL-instrumented.
	hash := strings.TrimSpace(o.HarnessHash)
	if hash != "" {
		if p, merr := MaterializeHarness(ctx, repoRoot, hash, o.HarnessFetchURL, o.HarnessContentSHA256, o.ArtifactDB); merr == nil && p != "" {
			return p, false, nil
		} else if merr != nil && err == nil {
			err = merr
		}
	}
	// ASAN stdin cache (weak / no compile-time edges).
	pattern := filepath.Join(repoRoot, ".cache", "oss-cve-bin", catalogTargetSegment(targetID)+"-*.bin")
	matches, _ := filepath.Glob(pattern)
	var best string
	var bestMod time.Time
	for _, m := range matches {
		if strings.Contains(m, "msan") {
			continue
		}
		st, e := os.Stat(m)
		if e != nil || st.IsDir() {
			continue
		}
		if best == "" || st.ModTime().After(bestMod) {
			best = m
			bestMod = st.ModTime()
		}
	}
	if best != "" {
		return best, false, nil
	}
	if err != nil {
		return "", false, err
	}
	return "", false, fmt.Errorf("hunt: no coverage harness for %s", targetID)
}
