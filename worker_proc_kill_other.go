//go:build !linux && !windows

package main

// Non-Linux Unix (macOS): we cannot inspect per-process worker ids the way Linux /proc
// does. A bare `pkill -x workerpoh` would kill every same-named miner on the box —
// including another node's fleet — which is exactly the cross-rig kill 938e385 claimed
// to eliminate. Prefer leaving orphans over unscoped signal storms (same tradeoff as
// Windows no-op kills).

func killPoolWorkerProcs(_ func(pid int) bool) {}

func killWorkerfuzzProcs(_ func(pid int) bool) {}

// ownedByWorkerIDs returns nil so callers skip kill when we cannot scope by worker id.
func ownedByWorkerIDs(ids []string) func(pid int) bool {
	_ = ids
	return nil
}
