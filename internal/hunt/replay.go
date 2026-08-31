package hunt

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"hackme/internal/fuzzupstream"
)

var harnessCache sync.Map // harnessHash -> bin path

// RepoRoot returns HACKME_REPO_ROOT or discovers go.mod parent.
func RepoRoot() string {
	if r := strings.TrimSpace(os.Getenv("HACKME_REPO_ROOT")); r != "" {
		return r
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	dir := wd
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return wd
}

// ReplayShardOpts runs one frozen Hunt shard input on the catalog harness.
type ReplayShardOpts struct {
	RepoRoot    string
	Spec        HarnessSpec
	TargetID    string
	HarnessHash string
	Input       []byte
	MaxInput    int
	ExecPer     int
}

// ReplayShardResult is coordinator/worker replay output for one shard.
type ReplayShardResult struct {
	Crash     bool
	Sanitizer string
	Trap      string
	ExecDone  int
}

// EnsureHarnessBinary returns a pinned ASAN harness binary for targetID/harnessHash.
// Binaries are cached under .cache/hunt-harness/{harnessHash}.bin for reuse across workers.
func EnsureHarnessBinary(ctx context.Context, repoRoot, targetID, harnessHash string) (string, error) {
	if repoRoot == "" {
		repoRoot = RepoRoot()
	}
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return "", fmt.Errorf("hunt: target_id required")
	}
	wantHash := strings.TrimSpace(harnessHash)
	if wantHash == "" {
		var err error
		wantHash, err = CatalogHarnessHash(repoRoot, targetID)
		if err != nil {
			return "", err
		}
	}
	if v, ok := harnessCache.Load(wantHash); ok {
		if p, ok := v.(string); ok && p != "" {
			if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
				return p, nil
			}
		}
	}
	cacheDir := filepath.Join(repoRoot, ".cache", "hunt-harness")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}
	cachePath := filepath.Join(cacheDir, wantHash+".bin")
	if st, err := os.Stat(cachePath); err == nil && st.Mode().IsRegular() {
		harnessCache.Store(wantHash, cachePath)
		return cachePath, nil
	}
	t, err := CatalogTarget(repoRoot, targetID)
	if err != nil {
		return "", err
	}
	gotHash, err := CatalogHarnessHash(repoRoot, targetID)
	if err != nil {
		return "", err
	}
	if gotHash != wantHash {
		return "", fmt.Errorf("hunt: harness_hash mismatch for %s", targetID)
	}
	binPath, _, err := fuzzupstream.BuildTarget(ctx, repoRoot, t)
	if err != nil {
		return "", err
	}
	in, err := os.ReadFile(binPath)
	if err != nil {
		return "", err
	}
	tmp := cachePath + ".tmp"
	if err := os.WriteFile(tmp, in, 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, cachePath); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	harnessCache.Store(wantHash, cachePath)
	return cachePath, nil
}

// ReplayShard executes execPer ASAN runs on frozen input (coordinator challenge).
func ReplayShard(ctx context.Context, opts ReplayShardOpts) (ReplayShardResult, error) {
	out := ReplayShardResult{}
	if len(opts.Input) == 0 {
		return out, fmt.Errorf("hunt replay: empty input")
	}
	execPer := opts.ExecPer
	if execPer < 1 {
		execPer = 1
	}
	maxB := opts.MaxInput
	if maxB <= 0 {
		maxB = 4096
	}
	binPath, err := resolveHarnessBinary(ctx, opts)
	if err != nil {
		return out, err
	}
	for i := 0; i < execPer; i++ {
		crash, san, _, runErr := fuzzupstream.RunInput(ctx, binPath, opts.Input, maxB)
		out.ExecDone = i + 1
		if runErr != nil && !crash {
			return out, fmt.Errorf("hunt replay run: %w", runErr)
		}
		if crash {
			out.Crash = true
			out.Sanitizer = strings.TrimSpace(san)
			if out.Sanitizer == "" {
				out.Sanitizer = "asan"
			}
			out.Trap = "hunt_crash:" + out.Sanitizer
			return out, nil
		}
	}
	return out, nil
}

func resolveHarnessBinary(ctx context.Context, opts ReplayShardOpts) (string, error) {
	spec := opts.Spec
	if spec.Source == "" {
		spec = HarnessSpec{
			Source:      "catalog",
			TargetID:    opts.TargetID,
			HarnessHash: opts.HarnessHash,
		}
	}
	if spec.TargetID == "" {
		spec.TargetID = opts.TargetID
	}
	if spec.HarnessHash == "" {
		spec.HarnessHash = opts.HarnessHash
	}
	return EnsureHarness(ctx, opts.RepoRoot, spec)
}
