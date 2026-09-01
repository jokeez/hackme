package hunt

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"hackme/internal/fuzzengine"
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

// ReplayShardOpts runs one Hunt pool shard input chain on the catalog harness.
type ReplayShardOpts struct {
	RepoRoot        string
	Spec            HarnessSpec
	TargetID        string
	HarnessHash     string
	HarnessFetchURL string
	CampaignID      string
	InputN          uint64
	Config          map[string]any
	CorpusSeeds     []fuzzengine.PoolCorpusSeed
	Input           []byte
	MaxInput        int
	ExecPer         int
}

// ReplayShardResult is coordinator/worker replay output for one shard.
type ReplayShardResult struct {
	Crash                 bool
	Sanitizer             string
	SanitizerInfo         fuzzupstream.SanitizerInfo
	Trap                  string
	ExecDone              int
	CrashInput            []byte
	CrashInputOriginalLen int
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

// ReplayShard executes execPer ASAN runs on a Hunt shard (anchor + deterministic mutations).
func ReplayShard(ctx context.Context, opts ReplayShardOpts) (ReplayShardResult, error) {
	out := ReplayShardResult{}
	execPer := opts.ExecPer
	if execPer < 1 {
		execPer = 1
	}
	maxB := opts.MaxInput
	if maxB <= 0 {
		maxB = 4096
	}
	cfg := opts.Config
	if cfg == nil {
		cfg = map[string]any{}
	}
	binPath, err := resolveHarnessBinary(ctx, opts)
	if err != nil {
		return out, err
	}
	runOpts := RunInputOptsFromConfig(cfg)
	if opts.MaxInput > 0 {
		runOpts.MaxInput = opts.MaxInput
	}
	for execIdx := 0; execIdx < execPer; execIdx++ {
		inputB := replayInputForExec(opts, uint64(execIdx), cfg)
		if len(inputB) == 0 {
			return out, fmt.Errorf("hunt replay: empty input exec=%d", execIdx)
		}
		crash, info, _, runErr := fuzzupstream.RunInputDetailed(ctx, binPath, inputB, runOpts)
		out.ExecDone = execIdx + 1
		if runErr != nil && !crash {
			return out, fmt.Errorf("hunt replay run: %w", runErr)
		}
		if crash {
			out.Crash = true
			out.SanitizerInfo = info
			out.Sanitizer = strings.TrimSpace(info.Raw)
			if out.Sanitizer == "" {
				out.Sanitizer = info.Subtype
			}
			if out.Sanitizer == "" {
				out.Sanitizer = "asan"
			}
			out.Trap = fuzzupstream.FormatHuntTrap(info)
			out.CrashInputOriginalLen = len(inputB)
			out.CrashInput = append([]byte(nil), inputB...)
			if HuntTrimEnabled(cfg) && len(out.CrashInput) > 1 {
				tr := fuzzupstream.TrimCrashInput(ctx, binPath, out.CrashInput, runOpts, info)
				if len(tr.Input) > 0 {
					out.CrashInput = tr.Input
				}
			}
			return out, nil
		}
	}
	return out, nil
}

func replayInputForExec(opts ReplayShardOpts, execIdx uint64, cfg map[string]any) []byte {
	if opts.CampaignID != "" && (ShardSegmentMutating(cfg) || HuntCorpusGuided(cfg)) {
		return ShardSegmentExecInput(opts.CampaignID, opts.InputN, execIdx, cfg, opts.CorpusSeeds)
	}
	if len(opts.Input) > 0 {
		return opts.Input
	}
	if opts.CampaignID != "" {
		return ShardSegmentExecInput(opts.CampaignID, opts.InputN, execIdx, cfg, opts.CorpusSeeds)
	}
	return nil
}

func resolveHarnessBinary(ctx context.Context, opts ReplayShardOpts) (string, error) {
	spec := opts.Spec
	hash := strings.TrimSpace(spec.HarnessHash)
	if hash == "" {
		hash = strings.TrimSpace(opts.HarnessHash)
	}
	if hash != "" {
		if p, err := MaterializeHarness(ctx, opts.RepoRoot, hash, opts.HarnessFetchURL, nil); err == nil && p != "" {
			return p, nil
		}
	}
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
