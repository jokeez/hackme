package hunt

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	inventoryMarker     = "LLVMFuzzerTestOneInput"
	inventoryMarkerRust = "fuzz_target!"
	defaultMaxFiles     = 400
	defaultMaxDepth     = 10
	maxInventoryFileLen = 512 * 1024
)

var blockedPathPrefixes = []string{
	"/etc", "/proc", "/sys", "/dev", "/run", "/var/run",
}

// blockedPinExact are absolute inventory/pin roots that must never be ExtraBind'd
// (hub secrets under /opt/hackme; whole home trees).
var blockedPinExact = []string{
	"/opt/hackme", "/home", "/root", "/",
}

// ScanInventory walks a local directory tree for libFuzzer entrypoints.
func ScanInventory(repoRoot, rawPath string, maxFiles, maxDepth int) (*InventoryResult, error) {
	root, err := resolveInventoryRoot(repoRoot, rawPath)
	if err != nil {
		return nil, err
	}
	if maxFiles <= 0 {
		maxFiles = defaultMaxFiles
	}
	if maxFiles > 2000 {
		maxFiles = 2000
	}
	if maxDepth <= 0 {
		maxDepth = defaultMaxDepth
	}
	if maxDepth > 16 {
		maxDepth = 16
	}

	targets := make([]TargetSummary, 0, 8)
	scanned := 0
	baseLen := len(root)

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root {
				rel, _ := filepath.Rel(root, path)
				depth := strings.Count(rel, string(os.PathSeparator)) + 1
				if depth > maxDepth {
					return filepath.SkipDir
				}
			}
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" || name == ".cache" || name == "target" {
				return filepath.SkipDir
			}
			return nil
		}
		if scanned >= maxFiles {
			return fs.SkipAll
		}
		if !isSourceFile(path) {
			return nil
		}
		scanned++
		hit, err := fileHasFuzzEntry(root, path)
		if err != nil || !hit {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		id := inventoryTargetID(rel)
		targets = append(targets, TargetSummary{
			ID:         id,
			Title:      filepath.Base(path),
			Path:       rel,
			Source:     "inventory",
			Language:   SourceLanguage(rel),
			Driver:     rel,
			ReuseReady: true,
			Disclosure: "Local inventory hit — verify harness builds with ASAN before Hunt escrow.",
		})
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, fs.SkipAll) {
		return nil, walkErr
	}

	sort.Slice(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
	_ = baseLen
	return &InventoryResult{
		Path:         root,
		ScannedFiles: scanned,
		Targets:      targets,
		BuildHints:   detectInventoryBuildHints(root),
		Disclaimer:   "Inventory lists files mentioning LLVMFuzzerTestOneInput / fuzz_target! — not a CVE guarantee.",
	}, nil
}

func resolveInventoryRoot(repoRoot, rawPath string) (string, error) {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return "", errors.New("hunt inventory: path required")
	}
	wasAbs := filepath.IsAbs(rawPath)
	var abs string
	var err error
	if wasAbs {
		abs = filepath.Clean(rawPath)
	} else {
		if repoRoot == "" {
			repoRoot = "."
		}
		abs, err = SafeJoinUnder(repoRoot, rawPath)
		if err != nil {
			return "", err
		}
	}
	abs, err = filepath.Abs(abs)
	if err != nil {
		return "", err
	}
	if err := rejectBlockedInventoryPin(abs); err != nil {
		return "", err
	}
	if wasAbs && !underInventoryWorkRoot(abs, repoRoot) {
		return "", fmt.Errorf("hunt inventory: absolute path outside allowlisted work roots: %s", abs)
	}
	safe, err := allowlistedAbs(abs)
	if err != nil {
		return "", err
	}
	st, err := fsStat(safe)
	if err != nil {
		return "", fmt.Errorf("hunt inventory: %w", err)
	}
	if !st.IsDir() {
		return "", errors.New("hunt inventory: path must be a directory")
	}
	return safe, nil
}

func rejectBlockedInventoryPin(abs string) error {
	abs = filepath.Clean(strings.TrimSpace(abs))
	if abs == "" || abs == "." {
		return errors.New("hunt inventory: path required")
	}
	if abs == hackmePrefixRoot() {
		return fmt.Errorf("hunt inventory: path blocked: %s", abs)
	}
	for _, exact := range blockedPinExact {
		if abs == filepath.Clean(exact) {
			return fmt.Errorf("hunt inventory: path blocked: %s", abs)
		}
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		if abs == filepath.Clean(home) {
			return fmt.Errorf("hunt inventory: path blocked: %s", abs)
		}
	}
	for _, prefix := range blockedPathPrefixes {
		if abs == prefix || strings.HasPrefix(abs, prefix+string(os.PathSeparator)) {
			return fmt.Errorf("hunt inventory: path blocked: %s", abs)
		}
	}
	switch abs {
	case "/var", "/opt", "/tmp", "/dev", "/proc", "/sys", "/etc":
		return fmt.Errorf("hunt inventory: path blocked: %s", abs)
	}
	return nil
}

// inventoryWorkRoots lists directories under which absolute inventory pins are allowed.
// Relative pins still resolve under repoRoot via SafeJoinUnder.
func inventoryWorkRoots(repoRoot string) []string {
	add := func(dst *[]string, seen map[string]struct{}, p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		abs, err := filepath.Abs(filepath.Clean(p))
		if err != nil || abs == "" || abs == "/" {
			return
		}
		if _, ok := seen[abs]; ok {
			return
		}
		seen[abs] = struct{}{}
		*dst = append(*dst, abs)
	}
	seen := map[string]struct{}{}
	var roots []string
	add(&roots, seen, repoRoot)
	add(&roots, seen, os.TempDir())
	add(&roots, seen, os.Getenv("HACKME_DATA_DIR"))
	add(&roots, seen, os.Getenv("HOME"))
	for _, part := range filepath.SplitList(os.Getenv("HACKME_HUNT_WORK_ROOTS")) {
		add(&roots, seen, part)
	}
	return roots
}

func underInventoryWorkRoot(abs, repoRoot string) bool {
	abs = filepath.Clean(abs)
	for _, root := range inventoryWorkRoots(repoRoot) {
		if abs == root || strings.HasPrefix(abs, root+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

func isSourceFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".c", ".cc", ".cpp", ".cxx", ".c++", ".rs":
		return true
	default:
		return false
	}
}

func fileHasFuzzEntry(root, path string) (bool, error) {
	b, err := SafeReadFileUnder(root, path)
	if err != nil {
		return false, err
	}
	if len(b) > maxInventoryFileLen {
		b = b[:maxInventoryFileLen]
	}
	s := string(b)
	for _, marker := range []string{inventoryMarker, inventoryMarkerRust, "libfuzzer_sys::fuzz_target", "libfuzzer_sys"} {
		if strings.Contains(s, marker) {
			return true, nil
		}
	}
	return false, nil
}

// fileHasMain reports standalone programs that must not be linked as companions.
func fileHasMain(root, path string) (bool, error) {
	b, err := SafeReadFileUnder(root, path)
	if err != nil {
		return false, err
	}
	if len(b) > maxInventoryFileLen {
		b = b[:maxInventoryFileLen]
	}
	s := string(b)
	for _, marker := range []string{"int main(", "void main(", "int MAIN(", "CJSON_CDECL main("} {
		if strings.Contains(s, marker) {
			return true, nil
		}
	}
	return false, nil
}

func inventoryTargetID(rel string) string {
	s := strings.ToLower(strings.TrimSpace(rel))
	s = strings.ReplaceAll(s, string(os.PathSeparator), "_")
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r == '_' || r == '-' || r == '.':
			return '_'
		default:
			return '_'
		}
	}, s)
	s = strings.Trim(s, "_")
	if s == "" {
		return "inventory_target"
	}
	if len(s) > 80 {
		s = s[:80]
	}
	return "inv_" + s
}
