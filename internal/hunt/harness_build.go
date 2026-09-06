package hunt

import (
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
	HarnessHash       string   `json:"harness_hash"`
	BinaryPath        string   `json:"binary_path"`
	SourceRel         string   `json:"source_rel"`
	Language          string   `json:"language,omitempty"`
	CompanionSources  []string `json:"companion_sources,omitempty"`
	IncludeDirs       []string `json:"include_dirs,omitempty"`
	PinSHA            string   `json:"pin_sha,omitempty"`
	BuildOK           bool     `json:"build_ok"`
	Note              string   `json:"note,omitempty"`
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
	if SourceLanguage(sourceRel) == "rust" {
		return nil, fmt.Errorf("hunt build: Rust inventory auto-compile is catalog-only in Phase A — use OSS target language=rust (cargo +nightly ASAN) or build with cargo fuzz manually")
	}
	hasEntry := strings.Contains(string(content), inventoryMarker)
	if !hasEntry && !req.TemplateAccept {
		return nil, fmt.Errorf("hunt build: LLVMFuzzerTestOneInput missing — set template_accept=true (Hunt Standard)")
	}
	hash := InventoryHarnessHash(req.Pin.CommitSHA, sourceRel, content)
	cachePath := filepath.Join(repoRoot, ".cache", "hunt-harness", hash+".bin")
	if st, err := os.Stat(cachePath); err == nil && st.Mode().IsRegular() {
		harnessCache.Store(hash, cachePath)
		plan, _ := planInventoryCompile(req.Pin.Path, sourceRel)
		res := &HarnessBuildResult{
			HarnessHash: hash,
			BinaryPath:  cachePath,
			SourceRel:   sourceRel,
			PinSHA:      req.Pin.CommitSHA,
			BuildOK:     true,
			Note:        "cached harness",
		}
		if plan != nil {
			res.Language = plan.Language
			res.CompanionSources = companionRels(req.Pin.Path, plan.CompanionAbs)
			res.IncludeDirs = includeRels(req.Pin.Path, plan.IncludeDirs)
		} else {
			res.Language = SourceLanguage(sourceRel)
		}
		return res, nil
	}
	if _, err := exec.LookPath("clang"); err != nil {
		return nil, fmt.Errorf("hunt build: clang required")
	}
	plan, err := planInventoryCompile(req.Pin.Path, sourceRel)
	if err != nil {
		return nil, err
	}
	if plan.Language == "cpp" {
		if _, err := exec.LookPath("clang++"); err != nil {
			return nil, fmt.Errorf("hunt build: clang++ required for C++ inventory")
		}
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
	buildCtx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	outBin := filepath.Join(tmpDir, "harness.bin")
	objs, err := compileInventoryObjects(buildCtx, plan, wrapperPath, tmpDir)
	if err != nil {
		return nil, err
	}
	if err := linkInventoryObjects(buildCtx, plan, objs, outBin); err != nil {
		return nil, err
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
	if plan.Language == "cpp" {
		note = "inventory C++ ASAN harness built"
	}
	if len(plan.CompanionAbs) > 0 {
		note += fmt.Sprintf(" (+ %d companion source(s))", len(plan.CompanionAbs))
	}
	if !hasEntry {
		note = "template Accept wrapper + inventory source"
	}
	compRel := companionRels(req.Pin.Path, plan.CompanionAbs)
	incRel := includeRels(req.Pin.Path, plan.IncludeDirs)
	return &HarnessBuildResult{
		HarnessHash:      hash,
		BinaryPath:       cachePath,
		SourceRel:        sourceRel,
		Language:         plan.Language,
		CompanionSources: compRel,
		IncludeDirs:      incRel,
		PinSHA:           req.Pin.CommitSHA,
		BuildOK:          true,
		Note:             note,
	}, nil
}

func companionRels(pinRoot string, abs []string) []string {
	out := make([]string, 0, len(abs))
	pinRoot = filepath.Clean(pinRoot)
	for _, a := range abs {
		rel, err := filepath.Rel(pinRoot, a)
		if err == nil && rel != "" && !strings.HasPrefix(rel, "..") {
			out = append(out, rel)
		}
	}
	return out
}

func includeRels(pinRoot string, abs []string) []string {
	out := make([]string, 0, len(abs))
	pinRoot = filepath.Clean(pinRoot)
	for _, a := range abs {
		rel, err := filepath.Rel(pinRoot, a)
		if err == nil && rel != "" && !strings.HasPrefix(rel, "..") {
			out = append(out, rel)
		} else {
			out = append(out, a)
		}
	}
	return out
}
