package hunt

import (
	"context"
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	maxCompanionSources = 24
	maxCompanionBytes   = 2 * 1024 * 1024
)

// SourceLanguage returns "cpp" or "c" for inventory compile driver selection.
func SourceLanguage(sourceRel string) string {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(sourceRel)))
	switch ext {
	case ".cc", ".cpp", ".cxx", ".c++":
		return "cpp"
	default:
		return "c"
	}
}

// inventoryCompilePlan describes one ASAN inventory harness build.
type inventoryCompilePlan struct {
	Language         string
	MainAbs          string
	CompanionAbs     []string
	IncludeDirs      []string
	Compiler         string
	StdFlag          string
	WrapperIsC       bool
}

func planInventoryCompile(pinRoot, sourceRel string) (*inventoryCompilePlan, error) {
	mainAbs, err := resolveSourceFile("", pinRoot, sourceRel)
	if err != nil {
		return nil, err
	}
	lang := SourceLanguage(sourceRel)
	companions, err := collectCompanionSources(pinRoot, sourceRel)
	if err != nil {
		return nil, err
	}
	compAbs := make([]string, 0, len(companions))
	for _, rel := range companions {
		abs, err := resolveSourceFile("", pinRoot, rel)
		if err != nil {
			continue
		}
		compAbs = append(compAbs, abs)
	}
	includes := collectIncludeDirs(pinRoot, sourceRel)
	compiler := "clang"
	std := "-std=c11"
	wrapperC := true
	if lang == "cpp" {
		compiler = "clang++"
		std = "-std=c++17"
		wrapperC = false
	}
	return &inventoryCompilePlan{
		Language:     lang,
		MainAbs:      mainAbs,
		CompanionAbs: compAbs,
		IncludeDirs:  includes,
		Compiler:     compiler,
		StdFlag:      std,
		WrapperIsC:   wrapperC,
	}, nil
}

func collectCompanionSources(pinRoot, mainRel string) ([]string, error) {
	sameDir, err := collectCompanionSourcesInDir(pinRoot, filepath.Dir(mainRel), filepath.Base(mainRel))
	if err != nil {
		return nil, err
	}
	parent, err := collectParentCompanions(pinRoot, mainRel)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(sameDir)+len(parent))
	for _, rel := range append(sameDir, parent...) {
		if _, ok := seen[rel]; ok {
			continue
		}
		seen[rel] = struct{}{}
		out = append(out, rel)
	}
	sort.Strings(out)
	return out, nil
}

func collectCompanionSourcesInDir(pinRoot, dirRel, skipBase string) ([]string, error) {
	searchDir := pinRoot
	if dirRel != "" && dirRel != "." {
		searchDir = filepath.Join(pinRoot, dirRel)
	}
	entries, err := os.ReadDir(searchDir)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, 4)
	var total int64
	for _, e := range entries {
		if e.IsDir() || len(out) >= maxCompanionSources {
			continue
		}
		name := e.Name()
		if name == skipBase {
			continue
		}
		if !isCompanionSourceFile(name) {
			continue
		}
		abs := filepath.Join(searchDir, name)
		st, err := e.Info()
		if err != nil {
			continue
		}
		if st.Size() > 512*1024 {
			continue
		}
		hit, err := fileHasFuzzEntry(abs)
		if err != nil || hit {
			continue
		}
		if mainHit, err := fileHasMain(abs); err != nil || mainHit {
			continue
		}
		rel := name
		if dirRel != "" && dirRel != "." {
			rel = filepath.Join(dirRel, name)
		}
		out = append(out, rel)
		total += st.Size()
		if total > maxCompanionBytes {
			break
		}
	}
	return out, nil
}

// collectParentCompanions links common library sources one level up (e.g. cJSON.c for fuzzing/cjson_read_fuzzer.c).
func collectParentCompanions(pinRoot, mainRel string) ([]string, error) {
	mainDir := filepath.Dir(mainRel)
	if mainDir == "" || mainDir == "." {
		return nil, nil
	}
	parentRel := filepath.Dir(mainDir)
	parentRoot := pinRoot
	if parentRel != "." {
		parentRoot = filepath.Join(pinRoot, parentRel)
	}
	entries, err := os.ReadDir(parentRoot)
	if err != nil {
		return nil, nil
	}
	out := make([]string, 0, 4)
	for _, e := range entries {
		if e.IsDir() || len(out) >= maxCompanionSources {
			continue
		}
		name := e.Name()
		if !isCompanionSourceFile(name) {
			continue
		}
		abs := filepath.Join(parentRoot, name)
		st, err := e.Info()
		if err != nil || st.Size() > 1024*1024 {
			continue
		}
		if hit, err := fileHasFuzzEntry(abs); err != nil || hit {
			continue
		}
		if mainHit, err := fileHasMain(abs); err != nil || mainHit {
			continue
		}
		rel := name
		if parentRel != "." {
			rel = filepath.Join(parentRel, name)
		}
		out = append(out, rel)
	}
	return out, nil
}

func isCompanionSourceFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".c", ".cc", ".cpp", ".cxx", ".c++":
		return true
	default:
		return false
	}
}

func collectIncludeDirs(pinRoot, sourceRel string) []string {
	seen := map[string]struct{}{}
	add := func(p string) {
		p = filepath.Clean(p)
		if p == "" {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			seen[p] = struct{}{}
		}
	}
	add(pinRoot)
	add(filepath.Dir(filepath.Join(pinRoot, sourceRel)))
	for _, rel := range []string{"include", "src", "lib", "public"} {
		add(filepath.Join(pinRoot, rel))
	}
	mainDir := filepath.Dir(filepath.Join(pinRoot, sourceRel))
	for _, rel := range []string{"include", "src", "../include"} {
		add(filepath.Join(mainDir, rel))
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func detectInventoryBuildHints(root string) []string {
	hints := make([]string, 0, 4)
	if fileExists(filepath.Join(root, "CMakeLists.txt")) {
		hints = append(hints, "cmake_present")
	}
	if fileExists(filepath.Join(root, "Makefile")) || fileExists(filepath.Join(root, "GNUmakefile")) {
		hints = append(hints, "makefile_present")
	}
	cpp := 0
	c := 0
	depthLimit := 5
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root {
				rel, _ := filepath.Rel(root, path)
				if strings.Count(rel, string(os.PathSeparator)) >= depthLimit {
					return filepath.SkipDir
				}
			}
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" || name == ".cache" {
				return filepath.SkipDir
			}
			return nil
		}
		if !isSourceFile(path) {
			return nil
		}
		if SourceLanguage(path) == "cpp" {
			cpp++
		} else {
			c++
		}
		return nil
	})
	if cpp > 0 {
		hints = append(hints, "cpp_sources")
	}
	if c > 0 {
		hints = append(hints, "c_sources")
	}
	if cpp > 0 && c > 0 {
		hints = append(hints, "mixed_c_cpp")
	}
	sort.Strings(hints)
	return hints
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func inventoryClangArgs(plan *inventoryCompilePlan, wrapperPath, outBin string) []string {
	// kept for tests / introspection; build uses compileInventoryObjects.
	return nil
}

func compileInventoryObjects(ctx context.Context, plan *inventoryCompilePlan, wrapperPath, tmpDir string) ([]string, error) {
	units := append([]string{wrapperPath}, append([]string{plan.MainAbs}, plan.CompanionAbs...)...)
	objs := make([]string, 0, len(units))
	for i, src := range units {
		obj := filepath.Join(tmpDir, fmt.Sprintf("unit%d.o", i))
		compiler, std, langX := compileUnitFlags(plan.Compiler, src)
		args := []string{
			"-fsanitize=address,undefined",
			"-fno-omit-frame-pointer",
			"-g", "-O1",
			"-std=" + std,
			"-c",
			"-o", obj,
		}
		for _, inc := range plan.IncludeDirs {
			args = append(args, "-I", inc)
		}
		if langX != "" {
			args = append(args, "-x", langX)
		}
		args = append(args, src)
		cmd := exec.CommandContext(ctx, compiler, args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("%s %s: %w (%s)", compiler, filepath.Base(src), err, strings.TrimSpace(stderr.String()))
		}
		objs = append(objs, obj)
	}
	return objs, nil
}

func linkInventoryObjects(ctx context.Context, plan *inventoryCompilePlan, objs []string, outBin string) error {
	args := []string{
		"-fsanitize=address,undefined",
		"-g", "-O1",
		"-o", outBin,
	}
	args = append(args, objs...)
	cmd := exec.CommandContext(ctx, plan.Compiler, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s link: %w (%s)", plan.Compiler, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func compileUnitFlags(driver, path string) (compiler, std, langX string) {
	ext := strings.ToLower(filepath.Ext(path))
	if driver == "clang++" {
		if ext == ".c" {
			return "clang", "c11", "c"
		}
		return "clang++", "c++17", ""
	}
	return "clang", "c11", ""
}
