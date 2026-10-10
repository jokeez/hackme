package hunt

import (
	"context"
	"database/sql"
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
	RepoRoot             string
	Spec                 HarnessSpec
	TargetID             string
	HarnessHash          string
	HarnessFetchURL      string
	HarnessContentSHA256 string  // sha256 hex of published binary (required for HTTP/cache)
	ArtifactDB           *sql.DB // optional: load published harness without HTTP
	CampaignID           string
	InputN               uint64
	Config               map[string]any
	CorpusSeeds          []fuzzengine.PoolCorpusSeed
	Input                []byte
	MaxInput             int
	ExecPer              int
	// OnExecProgress is invoked after each ASAN exec (and before/after crash trim).
	// Coordinators use this to heartbeat async replay jobs so stale reclaim cannot
	// steal a still-running verifier under load.
	OnExecProgress func(execDone int)
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
			if safe, err := MustUnderRoot(repoRoot, p); err == nil {
				if _, _, verr := readVerifiedHarnessCache(safe, ""); verr == nil {
					return safe, nil
				}
			}
		}
		harnessCache.Delete(wantHash)
	}
	cacheDir, err := SafeJoinUnder(repoRoot, ".cache", "hunt-harness")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}
	if err := ValidateHexHash(wantHash); err != nil {
		return "", err
	}
	cachePath, err := SafeCacheFile(repoRoot, "hunt-harness", wantHash, "bin")
	if err != nil {
		return "", err
	}
	if _, _, err := readVerifiedHarnessCache(cachePath, ""); err == nil {
		harnessCache.Store(wantHash, cachePath)
		return cachePath, nil
	}
	quarantineHarnessCache(cachePath)
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
	safeBin, err := fuzzupstream.ValidateBinPath(binPath)
	if err != nil {
		return "", err
	}
	in, err := SafeReadFileUnder(repoRoot, safeBin)
	if err != nil {
		return "", err
	}
	tmp, err := SafeCacheFile(repoRoot, "hunt-harness", wantHash, "bin.tmp")
	if err != nil {
		return "", err
	}
	if err := SafeWriteFileUnder(repoRoot, tmp, in, 0o755); err != nil {
		return "", err
	}
	if err := SafeRenameUnder(repoRoot, tmp, cachePath); err != nil {
		if reSafeAbsPath.MatchString(tmp) {
			_ = os.Remove(tmp)
		}
		return "", err
	}
	if err := writeHarnessCacheAttestation(cachePath, contentSHA256Hex(in)); err != nil {
		quarantineHarnessCache(cachePath)
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
		if err := ctx.Err(); err != nil {
			return out, err
		}
		inputB := replayInputForExec(opts, uint64(execIdx), cfg)
		if len(inputB) == 0 {
			return out, fmt.Errorf("hunt replay: empty input exec=%d", execIdx)
		}
		crash, info, _, runErr := fuzzupstream.RunInputDetailed(ctx, binPath, inputB, runOpts)
		out.ExecDone = execIdx + 1
		if opts.OnExecProgress != nil {
			opts.OnExecProgress(out.ExecDone)
		}
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
				if opts.OnExecProgress != nil {
					opts.OnExecProgress(out.ExecDone)
				}
				tr := fuzzupstream.TrimCrashInput(ctx, binPath, out.CrashInput, runOpts, info)
				if len(tr.Input) > 0 {
					out.CrashInput = tr.Input
				}
				if opts.OnExecProgress != nil {
					opts.OnExecProgress(out.ExecDone)
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
		p, err := MaterializeHarness(ctx, opts.RepoRoot, hash, opts.HarnessFetchURL, opts.HarnessContentSHA256, opts.ArtifactDB)
		if err == nil && p != "" {
			return p, nil
		}
		// Attested / absolute remote fetch: fail closed (do not rebuild a different binary).
		requireRemote := ValidContentSHA256(opts.HarnessContentSHA256) ||
			strings.HasPrefix(strings.TrimSpace(opts.HarnessFetchURL), "http://") ||
			strings.HasPrefix(strings.TrimSpace(opts.HarnessFetchURL), "https://")
		if requireRemote {
			if err != nil {
				return "", fmt.Errorf("hunt harness fetch failed for %s: %w", hash, err)
			}
			return "", fmt.Errorf("hunt harness fetch failed for %s: empty path", hash)
		}
		// Relative default path without attestation → local catalog/inventory rebuild (tests/dev).
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

func poolHarnessFetchConfigured() bool {
	for _, k := range []string{"HACKME_POOL_COORDINATOR_URL", "HACKME_COORDINATOR_URL", "COORD_URL"} {
		if strings.TrimSpace(os.Getenv(k)) != "" {
			return true
		}
	}
	return false
}
