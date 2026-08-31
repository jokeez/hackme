package hunt

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

// HarnessBuildRequest builds an ASAN harness from pinned repo inventory.
type HarnessBuildRequest struct {
	Pin            *RepoPinResult `json:"pin,omitempty"`
	SourceRel      string         `json:"source_rel"`
	TemplateAccept bool           `json:"template_accept,omitempty"`
}

// HarnessBuildResult is output from inventory harness build + optional smoke.
type HarnessBuildResult struct {
	HarnessHash string `json:"harness_hash"`
	BinaryPath  string `json:"binary_path"`
	SourceRel   string `json:"source_rel"`
	PinSHA      string `json:"pin_sha,omitempty"`
	BuildOK     bool   `json:"build_ok"`
	Note        string `json:"note,omitempty"`
}

// InventoryHarnessHash fingerprints a pinned inventory harness.
func InventoryHarnessHash(pinSHA, sourceRel string, content []byte) string {
	limit := len(content)
	if limit > 4096 {
		limit = 4096
	}
	sumInput := strings.TrimSpace(pinSHA) + "\x00" + strings.TrimSpace(sourceRel) + "\x00" + string(content[:limit])
	sum := sha256.Sum256([]byte(sumInput))
	return hex.EncodeToString(sum[:16])
}

// BuildInventoryHarness compiles an ASAN stdin harness for one inventory target.
func BuildInventoryHarness(ctx context.Context, repoRoot string, req HarnessBuildRequest) (*HarnessBuildResult, error) {
	if req.Pin == nil || strings.TrimSpace(req.Pin.Path) == "" {
		return nil, fmt.Errorf("hunt build: pin required")
	}
	sourceRel := strings.TrimSpace(req.SourceRel)
	if sourceRel == "" {
		return nil, fmt.Errorf("hunt build: source_rel required")
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
	hasEntry := strings.Contains(string(content), inventoryMarker)
	if !hasEntry && !req.TemplateAccept {
		return nil, fmt.Errorf("hunt build: LLVMFuzzerTestOneInput missing — set template_accept=true (Hunt Standard)")
	}
	hash := InventoryHarnessHash(req.Pin.CommitSHA, sourceRel, content)
	cachePath := filepath.Join(repoRoot, ".cache", "hunt-harness", hash+".bin")
	if st, err := os.Stat(cachePath); err == nil && st.Mode().IsRegular() {
		harnessCache.Store(hash, cachePath)
		return &HarnessBuildResult{
			HarnessHash: hash,
			BinaryPath:  cachePath,
			SourceRel:   sourceRel,
			PinSHA:      req.Pin.CommitSHA,
			BuildOK:     true,
			Note:        "cached harness",
		}, nil
	}
	if _, err := exec.LookPath("clang"); err != nil {
		return nil, fmt.Errorf("hunt build: clang required")
	}
	tmpDir, err := os.MkdirTemp("", "hunt-inv-build-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)
	wrapperPath := filepath.Join(tmpDir, "hunt_wrapper.c")
	if err := os.WriteFile(wrapperPath, []byte(stdinFuzzerWrapper), 0o644); err != nil {
		return nil, err
	}
	outBin := filepath.Join(tmpDir, "harness.bin")
	args := []string{
		"-fsanitize=address,undefined",
		"-fno-omit-frame-pointer",
		"-g", "-O1",
		"-I", filepath.Dir(srcPath),
		"-o", outBin,
		wrapperPath,
		srcPath,
	}
	buildCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(buildCtx, "clang", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("hunt build clang: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return nil, err
	}
	in, err := os.ReadFile(outBin)
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
	note := "inventory ASAN harness built"
	if !hasEntry {
		note = "template Accept wrapper + inventory source"
	}
	return &HarnessBuildResult{
		HarnessHash: hash,
		BinaryPath:  cachePath,
		SourceRel:   sourceRel,
		PinSHA:      req.Pin.CommitSHA,
		BuildOK:     true,
		Note:        note,
	}, nil
}
