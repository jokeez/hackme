//go:build !linux && !windows

package main

import "os/exec"

// Non-Linux Unix (macOS): exact process-name match (-x), never a substring of the full command
// line (-f), so unrelated processes that merely mention a worker name are left alone.
func killPoolWorkerProcs(_ func(pid int) bool) {
	for _, n := range []string{"workerpoh", "workerpoh-cuda", "workerpoh-opencl", "workerpoh-cpu"} {
		_ = exec.Command("pkill", "-x", n).Run()
	}
}

func killWorkerfuzzProcs(_ func(pid int) bool) {
	_ = exec.Command("pkill", "-x", "workerfuzz").Run()
}

// ownedByWorkerIDs: no per-process worker-id inspection on this platform; returns a non-nil
// allow-all only when the node knows its own worker id (so an id-less node signals nothing).
func ownedByWorkerIDs(ids []string) func(pid int) bool {
	for _, id := range ids {
		if id != "" {
			return func(int) bool { return true }
		}
	}
	return nil
}
