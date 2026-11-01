package hunt

import (
	"context"
	"fmt"
	"strings"
)

// AttachPublishedInventoryHarness binds a pool-distributed inventory campaign to an
// already-published coordinator harness_hash without a local ASAN/bwrap build.
//
// Trust chain:
//  1. Recompute InventoryHarnessHash from pinned source (same formula as build).
//  2. Require req.HarnessHash to match.
//  3. Caller must verify the coordinator still serves that hash (probe).
func AttachPublishedInventoryHarness(repoRoot string, pin *RepoPinResult, invRoot, sourceRel, wantHash string) (hash string, pinSHA string, err error) {
	wantHash = strings.TrimSpace(strings.ToLower(wantHash))
	if !ValidHarnessHash(wantHash) {
		return "", "", fmt.Errorf("hunt: invalid harness_hash %q", wantHash)
	}
	sourceRel = strings.TrimSpace(sourceRel)
	if sourceRel == "" {
		return "", "", fmt.Errorf("hunt: inventory source_rel required to attach harness_hash")
	}
	pinPath := ""
	pinSHA = ""
	if pin != nil {
		pinPath = strings.TrimSpace(pin.Path)
		pinSHA = strings.TrimSpace(pin.CommitSHA)
	}
	if pinPath == "" {
		pinPath = strings.TrimSpace(invRoot)
	}
	if pinPath == "" {
		return "", "", fmt.Errorf("hunt: pin or inventory root required to attach harness_hash")
	}
	if repoRoot == "" {
		repoRoot = RepoRoot()
	}
	srcPath, err := resolveSourceFile(repoRoot, pinPath, sourceRel)
	if err != nil {
		return "", "", err
	}
	content, err := SafeReadFileUnder(pinPath, srcPath)
	if err != nil {
		return "", "", err
	}
	got := strings.ToLower(InventoryHarnessHash(pinSHA, sourceRel, content))
	if got != wantHash {
		return "", "", fmt.Errorf("hunt: harness_hash mismatch (want %s from published artifact, got %s from pin/source — rebuild or re-publish)", wantHash, got)
	}
	return wantHash, pinSHA, nil
}

// SkipLocalHarnessBuild reports whether CreateRequest asks to reuse a published hash.
func (req CreateRequest) SkipLocalHarnessBuild() bool {
	return strings.TrimSpace(req.HarnessHash) != ""
}

// ProbeCoordinatorHarness is set by the node (or tests) to HEAD/GET the pool blob.
// Returns ok + optional content SHA-256 from X-Hackme-Content-SHA256.
var ProbeCoordinatorHarness = func(ctx context.Context, hash string) (ok bool, contentSHA256 string) {
	return false, ""
}
