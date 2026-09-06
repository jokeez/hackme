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
		hit, err := fileHasFuzzEntry(path)
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
	var abs string
	if filepath.IsAbs(rawPath) {
		abs = filepath.Clean(rawPath)
	} else {
		if repoRoot == "" {
			repoRoot = "."
		}
		abs = filepath.Clean(filepath.Join(repoRoot, rawPath))
	}
	for _, prefix := range blockedPathPrefixes {
		if abs == prefix || strings.HasPrefix(abs, prefix+string(os.PathSeparator)) {
			return "", fmt.Errorf("hunt inventory: path blocked: %s", abs)
		}
	}
	st, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("hunt inventory: %w", err)
	}
	if !st.IsDir() {
		return "", errors.New("hunt inventory: path must be a directory")
	}
	return abs, nil
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

func fileHasFuzzEntry(path string) (bool, error) {
	b, err := os.ReadFile(path)
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
func fileHasMain(path string) (bool, error) {
	b, err := os.ReadFile(path)
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
