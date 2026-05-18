package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"hackme/internal/chain"
	"hackme/internal/sandbox"
)

const (
	maxPayerRefRunesLint    = 256
	maxTargetSolvesLint     = 10000
	defaultTaskKindLint     = "synthetic_poh_v1"
	minTargetSolvesLint     = 1
	defaultTargetSolvesLint = 1
	defaultDifficultyLint   = chain.MinDifficultyScore
)

type manifestLintResult struct {
	Path         string   `json:"path"`
	Valid        bool     `json:"valid"`
	Warnings     []string `json:"warnings,omitempty"`
	Errors       []string `json:"errors,omitempty"`
	ArtifactRoot string   `json:"artifact_root"`
}

type manifestInput struct {
	ID              string  `json:"id"`
	Kind            string  `json:"kind"`
	RewardHMC       float64 `json:"reward_hmc"`
	DifficultyScore int     `json:"difficulty_score"`
	TargetSolves    int     `json:"target_solves"`
	PayerRef        string  `json:"payer_ref"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./tools/task_manifest_lint <manifest.json> [more.json ...]")
		os.Exit(2)
	}
	root := chain.DefaultArtifactRoot()
	results := make([]manifestLintResult, 0, len(os.Args)-1)
	hasErrors := false

	for _, p := range os.Args[1:] {
		res := lintManifest(p, root)
		results = append(results, res)
		if !res.Valid {
			hasErrors = true
		}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]any{
		"ok":      !hasErrors,
		"results": results,
	})
	if hasErrors {
		os.Exit(1)
	}
}

func lintManifest(path, artifactRoot string) manifestLintResult {
	abs, _ := filepath.Abs(path)
	out := manifestLintResult{Path: abs, ArtifactRoot: artifactRoot}
	raw, err := os.ReadFile(path)
	if err != nil {
		out.Errors = append(out.Errors, err.Error())
		return out
	}

	var in manifestInput
	if err := json.Unmarshal(raw, &in); err != nil {
		out.Errors = append(out.Errors, fmt.Sprintf("invalid json: %v", err))
		return out
	}

	id := strings.TrimSpace(in.ID)
	if id == "" {
		out.Errors = append(out.Errors, "manifest id required")
	}

	kind := strings.TrimSpace(in.Kind)
	if kind == "" {
		kind = defaultTaskKindLint
		out.Warnings = append(out.Warnings, "kind is empty; default synthetic_poh_v1 will be used")
	}
	if kind != defaultTaskKindLint {
		out.Errors = append(out.Errors, fmt.Sprintf("unsupported kind %q (only %s)", kind, defaultTaskKindLint))
	}

	if in.RewardHMC <= 0 {
		out.Errors = append(out.Errors, "reward_hmc must be > 0")
	}

	diff := in.DifficultyScore
	if diff <= 0 {
		diff = defaultDifficultyLint
		out.Warnings = append(out.Warnings, "difficulty_score is empty/zero; default 1 will be used")
	}
	if diff < chain.MinDifficultyScore || diff > chain.MaxDifficultyScore {
		out.Errors = append(out.Errors, fmt.Sprintf("difficulty_score must be %d..%d", chain.MinDifficultyScore, chain.MaxDifficultyScore))
	}

	if in.RewardHMC > 0 && diff >= chain.MinDifficultyScore && diff <= chain.MaxDifficultyScore {
		minReward := float64(diff) * chain.RewardPerDifficultyUnit
		if in.RewardHMC+1e-12 < minReward {
			out.Errors = append(out.Errors, fmt.Sprintf("reward_hmc too low for difficulty_score=%d (min %.6f)", diff, minReward))
		}
	}

	target := in.TargetSolves
	if target < minTargetSolvesLint {
		target = defaultTargetSolvesLint
		out.Warnings = append(out.Warnings, "target_solves is empty/zero; default 1 will be used")
	}
	if target > maxTargetSolvesLint {
		out.Errors = append(out.Errors, fmt.Sprintf("target_solves too large (max %d)", maxTargetSolvesLint))
	}

	if utf8.RuneCountInString(strings.TrimSpace(in.PayerRef)) > maxPayerRefRunesLint {
		out.Errors = append(out.Errors, fmt.Sprintf("payer_ref too long (max %d runes)", maxPayerRefRunesLint))
	}

	wasm, werr := chain.ResolveWasmCheckFromManifest(raw, artifactRoot)
	if werr != nil {
		out.Errors = append(out.Errors, werr.Error())
	} else if len(wasm) > 0 {
		if err := sandbox.ValidateCheckWasm(context.Background(), wasm); err != nil {
			out.Errors = append(out.Errors, fmt.Sprintf("wasm check module: %v", err))
		}
	}

	out.Valid = len(out.Errors) == 0
	return out
}
