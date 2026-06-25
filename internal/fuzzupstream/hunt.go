package fuzzupstream

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// HuntOptions configures a CVE hunt run.
type HuntOptions struct {
	RepoRoot         string
	OutDir           string
	TargetIDs        []string
	BudgetIterations int
	TimeLimitSec     int
	MaxInputBytes    int
	PriorityMax      int
}

// RunHunt builds and fuzzes OSS targets; writes per-target + rollup JSON.
func RunHunt(ctx context.Context, opts HuntOptions) (rollup *RollupReport, err error) {
	if opts.RepoRoot == "" {
		opts.RepoRoot = "."
	}
	manifest, err := LoadManifest(opts.RepoRoot)
	if err != nil {
		return nil, err
	}
	if opts.BudgetIterations <= 0 {
		opts.BudgetIterations = manifest.Defaults.BudgetIterations
	}
	if opts.TimeLimitSec <= 0 {
		opts.TimeLimitSec = manifest.Defaults.TimeLimitSec
	}
	if opts.MaxInputBytes <= 0 {
		opts.MaxInputBytes = manifest.Defaults.MaxInputBytes
	}
	if opts.OutDir == "" {
		opts.OutDir = filepath.Join(opts.RepoRoot, "reports", "oss-cve", time.Now().UTC().Format("20060102T150405Z"))
	}
	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(opts.TimeLimitSec)*time.Second)
	defer cancel()

	targets := selectTargets(manifest, opts)
	seeds := seedsFromManifest(manifest)

	rollup = &RollupReport{
		StartedAt: time.Now().UTC().Format(time.RFC3339),
		OutDir:    opts.OutDir,
		Targets:   []HuntReport{},
		Verdict:   "CLEAN",
	}

	for _, t := range targets {
		bin, clone, berr := BuildTarget(ctx, opts.RepoRoot, t)
		if berr != nil {
			rollup.BuildErrors = append(rollup.BuildErrors, fmt.Sprintf("%s: %v", t.ID, berr))
			continue
		}
		perTarget := opts.TimeLimitSec
		if perTarget <= 0 {
			perTarget = 600
		}
		if len(targets) > 1 {
			perTarget = opts.TimeLimitSec / len(targets)
			if perTarget < 30 {
				perTarget = 30
			}
		}
		rep, herr := Hunt(ctx, opts.RepoRoot, t, bin, seeds, opts.BudgetIterations, opts.MaxInputBytes, perTarget)
		if herr != nil {
			rollup.BuildErrors = append(rollup.BuildErrors, fmt.Sprintf("%s hunt: %v", t.ID, herr))
			continue
		}
		rep.ClonePath = clone
		rep.BinaryPath = bin
		crashDir := filepath.Join(opts.OutDir, t.ID, "crashes")
		for i := range rep.Crashes {
			p, _ := SaveCrashArtifact(crashDir, rep.Crashes[i])
			rep.Crashes[i].ArtifactPath = p
		}
		targetPath := filepath.Join(opts.OutDir, t.ID, "HUNT_REPORT.json")
		b, _ := json.MarshalIndent(rep, "", "  ")
		_ = os.WriteFile(targetPath, append(b, '\n'), 0o644)
		rollup.Targets = append(rollup.Targets, *rep)
		if len(rep.Crashes) > 0 {
			rollup.Verdict = "CVE_CANDIDATE"
			rollup.CVECandidates = append(rollup.CVECandidates, t.ID)
		} else {
			rollup.CleanTargets = append(rollup.CleanTargets, t.ID)
		}
	}
	rollup.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	if rollup.Verdict == "CVE_CANDIDATE" {
		rollup.Summary = "HOLD — native CVE candidate(s). Responsible disclosure required before publish."
	} else {
		rollup.Summary = "CLEAN — no ASAN crash in budget. Publish as methodology case study."
	}
	rb, _ := json.MarshalIndent(rollup, "", "  ")
	_ = os.WriteFile(filepath.Join(opts.OutDir, "ROLLUP.json"), append(rb, '\n'), 0o644)
	return rollup, nil
}

// RollupReport aggregates multi-target hunt.
type RollupReport struct {
	StartedAt     string       `json:"started_at"`
	FinishedAt    string       `json:"finished_at"`
	OutDir        string       `json:"out_dir"`
	Verdict       string       `json:"verdict"`
	Summary       string       `json:"summary"`
	CVECandidates []string     `json:"cve_candidates,omitempty"`
	CleanTargets  []string     `json:"clean_targets,omitempty"`
	BuildErrors   []string     `json:"build_errors,omitempty"`
	Targets       []HuntReport `json:"targets"`
}

func selectTargets(m *Manifest, opts HuntOptions) []Target {
	if len(opts.TargetIDs) > 0 {
		var out []Target
		for _, id := range opts.TargetIDs {
			t, err := m.TargetByID(id)
			if err == nil {
				out = append(out, t)
			}
		}
		return out
	}
	var out []Target
	for _, t := range m.Targets {
		if opts.PriorityMax > 0 && t.Priority > opts.PriorityMax {
			continue
		}
		out = append(out, t)
	}
	return out
}

func seedsFromManifest(m *Manifest) [][]byte {
	var seeds [][]byte
	for _, s := range m.Seeds {
		seeds = append(seeds, []byte(s))
	}
	return seeds
}

// TargetIDsFromFlags parses comma-separated target list.
func TargetIDsFromFlags(s string) []string {
	if strings.TrimSpace(s) == "" || strings.EqualFold(s, "all") {
		return nil
	}
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
