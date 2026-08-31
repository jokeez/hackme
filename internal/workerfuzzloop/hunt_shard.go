package workerfuzzloop

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"hackme/internal/fuzzupstream"
	"hackme/internal/hunt"
)

var huntBinCache sync.Map // key: targetID+"\x00"+harnessHash -> binPath

// HuntShardsEnabled is false when HACKME_WORKER_HUNT_SHARDS=0.
func HuntShardsEnabled() bool {
	return !Falsy(os.Getenv("HACKME_WORKER_HUNT_SHARDS"))
}

// RunHuntShard executes one Hunt pool shard (ASAN catalog harness, frozen input).
func RunHuntShard(ctx context.Context, cr ClaimResp, timeoutMS int) (checkResult int32, durationMS int, trap string, execDone int) {
	if !HuntShardsEnabled() {
		return 0, 0, "hunt disabled", 0
	}
	start := time.Now()
	inputB, err := hex.DecodeString(strings.TrimSpace(cr.InputBytesHex))
	if err != nil || len(inputB) == 0 {
		return 0, 0, "missing input_bytes", 0
	}
	targetID := strings.TrimSpace(cr.UpstreamTargetID)
	if targetID == "" {
		return 0, 0, "missing upstream_target_id", 0
	}
	repoRoot := huntRepoRoot()
	t, err := hunt.CatalogTarget(repoRoot, targetID)
	if err != nil {
		return 0, 0, "catalog: " + err.Error(), 0
	}
	binPath, err := huntBinaryCached(ctx, repoRoot, t, strings.TrimSpace(cr.HarnessHash))
	if err != nil {
		return 0, 0, "build: " + err.Error(), 0
	}
	maxB := cr.MaxInputBytes
	if maxB <= 0 {
		maxB = 4096
	}
	execPer := cr.ExecPerUnit
	if execPer < 1 {
		execPer = 1
	}
	runCtx := ctx
	if timeoutMS > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
		defer cancel()
	}
	for i := 0; i < execPer; i++ {
		crash, san, _, runErr := fuzzupstream.RunInput(runCtx, binPath, inputB, maxB)
		if runErr != nil && !crash {
			return 0, int(time.Since(start).Milliseconds()), runErr.Error(), i
		}
		if crash {
			if san == "" {
				san = "asan"
			}
			return 1, int(time.Since(start).Milliseconds()), "hunt_crash:" + san, execPer
		}
	}
	return 1, int(time.Since(start).Milliseconds()), "", execPer
}

func huntBinaryCached(ctx context.Context, repoRoot string, t fuzzupstream.Target, harnessHash string) (string, error) {
	key := t.ID + "\x00" + harnessHash
	if v, ok := huntBinCache.Load(key); ok {
		if p, ok := v.(string); ok && p != "" {
			return p, nil
		}
	}
	binPath, _, err := fuzzupstream.BuildTarget(ctx, repoRoot, t)
	if err != nil {
		return "", err
	}
	huntBinCache.Store(key, binPath)
	return binPath, nil
}

func huntRepoRoot() string {
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

// IsHuntClaim reports Hunt shard work from coordinator claim JSON.
func IsHuntClaim(cr ClaimResp) bool {
	if strings.EqualFold(strings.TrimSpace(cr.TaskClass), "hunt") {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(cr.WorkKind), "hunt_shard")
}

// HuntClaimMissingFields returns an error if required Hunt fields are absent.
func HuntClaimMissingFields(cr ClaimResp) error {
	if strings.TrimSpace(cr.UpstreamTargetID) == "" {
		return fmt.Errorf("hunt claim missing upstream_target_id")
	}
	if strings.TrimSpace(cr.InputBytesHex) == "" {
		return fmt.Errorf("hunt claim missing input_bytes_hex")
	}
	return nil
}
