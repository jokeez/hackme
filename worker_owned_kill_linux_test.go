//go:build linux

package main

import (
	"os/exec"
	"testing"
	"time"
)

// A node must only signal workers running as its own id (2026-11-14: a test node's watchdog
// killed the operator's GPU miner and named fleet every ~45 s).
func TestOwnedByWorkerIDsReadsCommandLine(t *testing.T) {
	mine := exec.Command("bash", "-c", "sleep 30; true", "x", "-worker", "worker-node-a")
	other := exec.Command("bash", "-c", "sleep 30; true", "x", "-worker", "worker-operator-gpu")
	for _, c := range []*exec.Cmd{mine, other} {
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		defer func(c *exec.Cmd) { _ = c.Process.Kill(); _ = c.Wait() }(c)
	}
	allow := ownedByWorkerIDs([]string{"worker-node-a"})
	// Right after fork the child still shows the test binary's argv; wait for exec.
	for i := 0; i < 100 && (procWorkerID(mine.Process.Pid) == "" || procWorkerID(other.Process.Pid) == ""); i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if allow == nil || !allow(mine.Process.Pid) {
		t.Fatal("own worker not matched")
	}
	if allow(other.Process.Pid) {
		t.Fatal("foreign worker matched")
	}
}
