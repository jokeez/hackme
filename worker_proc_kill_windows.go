//go:build windows

package main

// Windows stops workers with taskkill /IM (exact image name); see killExternalWorkerFleet.
func killPoolWorkerProcs(_ func(pid int) bool) {}

func killWorkerfuzzProcs(_ func(pid int) bool) {}

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
