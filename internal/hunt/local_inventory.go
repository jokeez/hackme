package hunt

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"hackme/internal/fuzzupstream"
)

// localInventoryTickWallSec is one libFuzzer session per autorun tick for a customer's own
// harness: long enough to make progress, short enough to report crashes as they come.
const localInventoryTickWallSec = 60

// IsInventoryHunt is a Hunt on the customer's own repo (hunt inventory / --repo-path), as
// opposed to a catalog OSS target.
func IsInventoryHunt(cfg map[string]any) bool {
	if strings.EqualFold(strings.TrimSpace(cfgString(cfg, "hunt_source")), "inventory") {
		return true
	}
	return strings.HasPrefix(strings.TrimSpace(cfgString(cfg, "upstream_target_id")), "inv_")
}

// localInventoryTick runs libFuzzer (ASAN+UBSan, in-process) on the customer's harness for one
// session and returns the crashes it found, each replayed and classified. The local runner
// used to resolve every target in the OSS catalog and failed with "unknown target inv_…", so a
// Hunt on your own code could only ever run on the pool (customer e2e 2026-11-13).
func localInventoryTick(ctx context.Context, repoRoot string, cfg map[string]any, wallSec int) (*fuzzupstream.HuntReport, error) {
	pin := strings.TrimSpace(cfgString(cfg, "hunt_pin_path"))
	if pin == "" {
		pin = strings.TrimSpace(cfgString(cfg, "hunt_inventory_root"))
	}
	src := strings.TrimSpace(cfgString(cfg, "hunt_source_rel"))
	targetID := strings.TrimSpace(cfgString(cfg, "upstream_target_id"))
	if pin == "" || src == "" {
		return nil, fmt.Errorf("hunt local: inventory campaign needs hunt_pin_path and hunt_source_rel")
	}
	if wallSec <= 0 || wallSec > localInventoryTickWallSec {
		wallSec = localInventoryTickWallSec
	}
	if wallSec < MinEngineLeaseSec {
		wallSec = MinEngineLeaseSec
	}
	started := time.Now()
	res := RunEngineShard(ctx, EngineShardOpts{
		RepoRoot:    repoRoot,
		TargetID:    targetID,
		Engine:      EngineLibFuzzer,
		WallSec:     wallSec,
		HarnessHash: strings.TrimSpace(cfgString(cfg, "harness_hash")),
		PinPath:     pin,
		SourceRel:   src,
	})
	if !res.Ran && len(res.CrashFiles) == 0 && res.SkippedReason != "" && res.SkippedReason != "lf_wall_short" {
		msg := res.SkippedReason
		if res.Err != "" {
			msg += ": " + res.Err
		}
		return nil, fmt.Errorf("hunt local: libFuzzer did not run (%s) — run install_hunt_engines.sh --desktop", msg)
	}
	rep := &fuzzupstream.HuntReport{
		TargetID:   targetID,
		Title:      src,
		Language:   "c",
		Iterations: int(time.Since(started).Seconds()) + 1, // progress = real engine seconds, never the requested wall
		ElapsedSec: time.Since(started).Seconds(),
		CorpusSize: res.CorpusAfter,
		CorpusDir:  res.CorpusDir,
		Driver:     "libfuzzer_inprocess",
		Verdict:    "CLEAN",
	}
	bin := ""
	if b, err := BuildInventoryInProcessLibFuzzer(ctx, repoRoot, &RepoPinResult{Path: pin, CommitSHA: "engine-shard"}, src); err == nil {
		bin = b
	}
	for _, f := range res.CrashFiles {
		data, err := os.ReadFile(f)
		if err != nil || len(data) == 0 {
			continue
		}
		cf := fuzzupstream.CrashFinding{
			TargetID: targetID,
			Title:    src,
			InputHex: hex.EncodeToString(data),
			InputLen: len(data),
		}
		if bin != "" {
			info, tail := replayInventoryCrash(ctx, bin, f)
			cf.Sanitizer = info.Class
			cf.SanitizerClass = info.Class
			cf.SanitizerSubtype = info.Subtype
			cf.SanitizerLabel = info.Label
			cf.Tail = tail
		}
		rep.Crashes = append(rep.Crashes, cf)
	}
	if len(rep.Crashes) > 0 {
		rep.Verdict = "FOUND"
	}
	return rep, nil
}

// replayInventoryCrash runs the libFuzzer binary on one crash input and classifies the
// sanitizer report (the crash file alone does not say heap overflow vs UAF).
func replayInventoryCrash(ctx context.Context, bin, input string) (fuzzupstream.SanitizerInfo, string) {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, "-runs=1", input)
	cmd.Env = append(os.Environ(), "ASAN_OPTIONS=detect_leaks=1:symbolize=1", "UBSAN_OPTIONS=print_stacktrace=1:halt_on_error=1")
	out, _ := cmd.CombinedOutput()
	blob := string(out)
	info := fuzzupstream.ClassifySanitizer(blob)
	tail := blob
	if i := strings.Index(blob, "==ERROR:"); i > 0 {
		if j := strings.LastIndex(blob[:i], "\n"); j >= 0 {
			tail = blob[j+1:]
		}
	}
	if len(tail) > 4000 {
		tail = tail[:4000]
	}
	return info, tail
}
