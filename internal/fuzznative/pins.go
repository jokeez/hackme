package fuzznative

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// PinsFile is the default upstream pin manifest path.
const PinsFile = "upstream/pins.json"

// PinManifest mirrors upstream/pins.json.
type PinManifest struct {
	Repos map[string]PinRepo `json:"repos"`
}

// PinRepo describes one pinned upstream.
type PinRepo struct {
	Remote      string            `json:"remote"`
	Tag         string            `json:"tag"`
	Commit      string            `json:"commit"`
	FuzzHarness string            `json:"fuzz_harness"`
	CorpusPath  string            `json:"corpus_path"`
	CorpusPaths map[string]string `json:"corpus_paths"`
	SourceFiles []string          `json:"source_files"`
}

// LoadPins reads upstream/pins.json from repoRoot (or HACKME_REPO_ROOT).
func LoadPins(repoRoot string) (*PinManifest, error) {
	root := strings.TrimSpace(repoRoot)
	if root == "" {
		root = strings.TrimSpace(os.Getenv("HACKME_REPO_ROOT"))
	}
	if root == "" {
		if wd, err := os.Getwd(); err == nil {
			root = wd
		}
	}
	path := filepath.Join(root, PinsFile)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m PinManifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// ResolveTarget returns pin repo key for campaign upstream_target / guard name.
func ResolveTarget(upstreamTarget, guardName string) string {
	t := strings.TrimSpace(strings.ToLower(upstreamTarget))
	if t != "" {
		return t
	}
	g := strings.TrimSpace(strings.ToLower(guardName))
	switch {
	case strings.Contains(g, "bitcoin"):
		return "bitcoin"
	case strings.Contains(g, "ethereum"):
		return "ethereum"
	case strings.Contains(g, "dogecoin"):
		return "dogecoin"
	case strings.Contains(g, "litecoin"):
		return "litecoin"
	default:
		return ""
	}
}
