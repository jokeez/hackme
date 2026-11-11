package hunt

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	maxCompanionSources = 24
	maxCompanionBytes   = 2 * 1024 * 1024
)

// SourceLanguage returns "cpp", "rust", or "c" for inventory compile driver selection.
func SourceLanguage(sourceRel string) string {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(sourceRel)))
	switch ext {
	case ".cc", ".cpp", ".cxx", ".c++":
		return "cpp"
	case ".rs":
		return "rust"
	default:
		return "c"
	}
}

// inventoryCompilePlan describes one ASAN inventory harness build.
type inventoryCompilePlan struct {
	Language     string
	MainAbs      string
	CompanionAbs []string
	IncludeDirs  []string
	Compiler     string
	StdFlag      string
	WrapperIsC   bool
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
	common, err := collectCommonLibCompanions(pinRoot, mainRel)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(sameDir)+len(parent)+len(common))
	for _, rel := range append(append(sameDir, parent...), common...) {
		if _, ok := seen[rel]; ok {
			continue
		}
		seen[rel] = struct{}{}
		out = append(out, rel)
	}
	sort.Strings(out)
	return out, nil
}

// collectCommonLibCompanions picks non-fuzz sources under src/ lib/ source/
// (e.g. heavywire: fuzz/fuzz_parse.c + src/heavywire.c).
func collectCommonLibCompanions(pinRoot, mainRel string) ([]string, error) {
	mainBase := filepath.Base(mainRel)
	out := make([]string, 0, 8)
	for _, dirRel := range []string{"src", "lib", "source"} {
		got, err := collectCompanionSourcesInDir(pinRoot, dirRel, mainBase)
		if err != nil {
			continue
		}
		out = append(out, got...)
		if len(out) >= maxCompanionSources {
			break
		}
	}
	return out, nil
}

func collectCompanionSourcesInDir(pinRoot, dirRel, skipBase string) ([]string, error) {
	searchDir := pinRoot
	if dirRel != "" && dirRel != "." {
		var err error
		searchDir, err = SafeJoinUnder(pinRoot, dirRel)
		if err != nil {
			return nil, err
		}
	}
	entries, err := SafeReadDirUnder(pinRoot, searchDir)
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
		abs, err := SafeJoinUnder(searchDir, name)
		if err != nil {
			continue
		}
		st, err := e.Info()
		if err != nil {
			continue
		}
		if st.Size() > 512*1024 {
			continue
		}
		hit, err := fileHasFuzzEntry(pinRoot, abs)
		if err != nil || hit {
			continue
		}
		if mainHit, err := fileHasMain(pinRoot, abs); err != nil || mainHit {
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
		var err error
		parentRoot, err = SafeJoinUnder(pinRoot, parentRel)
		if err != nil {
			return nil, nil
		}
	}
	entries, err := SafeReadDirUnder(pinRoot, parentRoot)
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
		abs, err := SafeJoinUnder(parentRoot, name)
		if err != nil {
			continue
		}
		st, err := e.Info()
		if err != nil || st.Size() > 1024*1024 {
			continue
		}
		if hit, err := fileHasFuzzEntry(pinRoot, abs); err != nil || hit {
			continue
		}
		if mainHit, err := fileHasMain(pinRoot, abs); err != nil || mainHit {
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
		safe, err := MustUnderRoot(pinRoot, p)
		if err != nil {
			absPin, _ := filepath.Abs(filepath.Clean(pinRoot))
			absP, err2 := filepath.Abs(p)
			if err2 != nil || absP != absPin {
				return
			}
			safe = absPin
		}
		if _, ok := seen[safe]; ok {
			return
		}
		if st, err := SafeStatUnder(pinRoot, safe); err == nil && st.IsDir() {
			seen[safe] = struct{}{}
		}
	}
	add(pinRoot)
	if dir, err := SafeJoinUnder(pinRoot, filepath.Dir(sourceRel)); err == nil {
		add(dir)
	}
	for _, rel := range []string{"include", "src", "lib", "public"} {
		if p, err := SafeJoinUnder(pinRoot, rel); err == nil {
			add(p)
		}
	}
	mainDir, err := SafeJoinUnder(pinRoot, filepath.Dir(sourceRel))
	if err == nil {
		for _, rel := range []string{"include", "src"} {
			if p, err := SafeJoinUnder(mainDir, rel); err == nil {
				add(p)
			}
		}
		if p, err := SafeJoinUnder(pinRoot, filepath.Dir(sourceRel), "..", "include"); err == nil {
			add(p)
		}
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
	if fileExistsUnder(root, "CMakeLists.txt") {
		hints = append(hints, "cmake_present")
	}
	if fileExistsUnder(root, "Makefile") || fileExistsUnder(root, "GNUmakefile") {
		hints = append(hints, "makefile_present")
	}
	if fileExistsUnder(root, "Cargo.toml") {
		hints = append(hints, "cargo_present")
	}
	cpp := 0
	c := 0
	rust := 0
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
			if name == ".git" || name == "node_modules" || name == "vendor" || name == ".cache" || name == "target" {
				return filepath.SkipDir
			}
			return nil
		}
		if !isSourceFile(path) {
			return nil
		}
		switch SourceLanguage(path) {
		case "cpp":
			cpp++
		case "rust":
			rust++
		default:
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
	if rust > 0 {
		hints = append(hints, "rust_sources")
	}
	if cpp > 0 && c > 0 {
		hints = append(hints, "mixed_c_cpp")
	}
	sort.Strings(hints)
	return hints
}

func fileExistsUnder(root, rel string) bool {
	st, err := SafeStatUnder(root, rel)
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
		binds := filterSecretPrefixRoots(append([]string{tmpDir, filepath.Dir(src)}, plan.IncludeDirs...))
		stderr, err := runCompilerBuild(ctx, compiler, args, binds, 180*time.Second)
		if err != nil {
			return nil, fmt.Errorf("%s %s: %w (%s)", compiler, filepath.Base(src), err, strings.TrimSpace(stderr))
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
	binds := []string{filepath.Dir(outBin)}
	for _, o := range objs {
		binds = append(binds, filepath.Dir(o))
	}
	binds = filterSecretPrefixRoots(binds)
	stderr, err := runCompilerBuild(ctx, plan.Compiler, args, binds, 180*time.Second)
	if err != nil {
		return fmt.Errorf("%s link: %w (%s)", plan.Compiler, err, strings.TrimSpace(stderr))
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
