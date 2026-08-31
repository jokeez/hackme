package fuzzupstream

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Manifest is upstream/oss_cve_targets.json.
type Manifest struct {
	Updated  string    `json:"updated"`
	Note     string    `json:"note"`
	Defaults Defaults  `json:"defaults"`
	Seeds    []string  `json:"seeds"`
	Targets  []Target  `json:"targets"`
	Rotation *Rotation `json:"rotation,omitempty"`
}

// Rotation config for nightly target selection.
type Rotation struct {
	Note  string   `json:"note"`
	Queue []string `json:"queue"`
}

// Defaults for OSS CVE hunts.
type Defaults struct {
	BudgetIterations int    `json:"budget_iterations"`
	TimeLimitSec     int    `json:"time_limit_sec"`
	MaxInputBytes    int    `json:"max_input_bytes"`
	Workers          int    `json:"workers"`
	DepthTier        string `json:"depth_tier"`
}

// Target is one real upstream OSS fuzz profile.
type Target struct {
	ID          string   `json:"id"`
	Repo        string   `json:"repo"`
	Ref         string   `json:"ref"`
	Title       string   `json:"title"`
	Driver      string   `json:"driver"`
	UpstreamSrc []string `json:"upstream_src"`
	IncludeDirs []string `json:"include_dirs"`
	WasmGuard   string   `json:"wasm_guard"`
	CWE         []string `json:"cwe"`
	Priority    int      `json:"priority"`
	BuildFlags  []string `json:"build_flags,omitempty"`
}

// CrashFinding is a sanitizer crash on real upstream code.
type CrashFinding struct {
	TargetID     string   `json:"target_id"`
	Title        string   `json:"title"`
	Repo         string   `json:"repo"`
	InputHex     string   `json:"input_hex"`
	InputLen     int      `json:"input_len"`
	Sanitizer        string   `json:"sanitizer"`
	SanitizerClass   string   `json:"sanitizer_class,omitempty"`
	SanitizerSubtype string   `json:"sanitizer_subtype,omitempty"`
	SanitizerLabel   string   `json:"sanitizer_label,omitempty"`
	Tail             string   `json:"tail"`
	Iteration    int      `json:"iteration"`
	CWE          []string `json:"cwe,omitempty"`
	Disclosure   string   `json:"disclosure"`
	ArtifactPath string   `json:"artifact_path,omitempty"`
}

// HuntReport is written per target or rollup.
type HuntReport struct {
	TargetID   string         `json:"target_id"`
	Title      string         `json:"title"`
	Repo       string         `json:"repo"`
	Iterations int            `json:"iterations"`
	ElapsedSec float64        `json:"elapsed_sec"`
	Crashes    []CrashFinding `json:"crashes"`
	Verdict    string         `json:"verdict"`
	BinaryPath string         `json:"binary_path,omitempty"`
	ClonePath  string         `json:"clone_path,omitempty"`
}

// LoadManifest reads upstream/oss_cve_targets.json from repo root.
func LoadManifest(repoRoot string) (*Manifest, error) {
	if repoRoot == "" {
		repoRoot = "."
	}
	p := filepath.Join(repoRoot, "upstream", "oss_cve_targets.json")
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if m.Defaults.BudgetIterations <= 0 {
		m.Defaults.BudgetIterations = 60000
	}
	if m.Defaults.TimeLimitSec <= 0 {
		m.Defaults.TimeLimitSec = 600
	}
	if m.Defaults.MaxInputBytes <= 0 {
		m.Defaults.MaxInputBytes = 65536
	}
	return &m, nil
}

// TargetByID finds a target or error.
func (m *Manifest) TargetByID(id string) (Target, error) {
	id = strings.TrimSpace(id)
	for _, t := range m.Targets {
		if t.ID == id {
			return t, nil
		}
	}
	return Target{}, fmt.Errorf("fuzzupstream: unknown target %q", id)
}
