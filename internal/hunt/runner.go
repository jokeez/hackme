package hunt

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"hackme/internal/fuzzupstream"
)

// LocalRunOptions configures a single-target Hunt smoke on the node.
type LocalRunOptions struct {
	RepoRoot         string
	TargetID         string
	OutDir           string
	BudgetIterations int
	TimeLimitSec     int
}

// LocalRun executes one catalog OSS target hunt (node-local, no pool shards yet).
func LocalRun(ctx context.Context, opts LocalRunOptions) (*fuzzupstream.HuntReport, error) {
	if opts.RepoRoot == "" {
		opts.RepoRoot = "."
	}
	if strings.TrimSpace(opts.TargetID) == "" {
		return nil, fmt.Errorf("hunt run: target_id required")
	}
	if opts.BudgetIterations <= 0 {
		opts.BudgetIterations = 2000
	}
	if opts.TimeLimitSec <= 0 {
		opts.TimeLimitSec = 120
	}
	if opts.OutDir == "" {
		opts.OutDir = filepath.Join(opts.RepoRoot, "reports", "hunt-local", time.Now().UTC().Format("20060102T150405Z"))
	}
	manifest, err := fuzzupstream.LoadManifest(opts.RepoRoot)
	if err != nil {
		return nil, err
	}
	t, err := manifest.TargetByID(opts.TargetID)
	if err != nil {
		return nil, err
	}
	binPath, _, err := fuzzupstream.BuildTarget(ctx, opts.RepoRoot, t)
	if err != nil {
		return nil, err
	}
	maxInput := manifest.Defaults.MaxInputBytes
	if maxInput <= 0 {
		maxInput = 65536
	}
	return fuzzupstream.Hunt(ctx, opts.RepoRoot, t, binPath, nil, opts.BudgetIterations, maxInput, opts.TimeLimitSec)
}
