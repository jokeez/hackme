package main

import (
	"os"
	"strings"
	"testing"
)

// Regression for fimasw CWE-214 incomplete-fix: handleWorkerStart / fleet must not
// put the coordinator token on workerpoh argv (world-readable /proc cmdline).
func TestWorkerpohSpawnSourcesOmitTokenFlag(t *testing.T) {
	for _, path := range []string{"main.go", "worker_fleet.go"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		src := string(raw)
		// Direct spawn sites previously used: "-token", coordToken
		if strings.Contains(src, `"-token", coordToken`) || strings.Contains(src, `"-token",coordToken`) {
			t.Errorf("%s still passes coordinator token on argv (CWE-214)", path)
		}
	}
}

// The Go spawn sites were fixed, but the shipped units kept passing the flag: the hub
// digger unit and the rented-GPU runner both put the coordinator token on argv, where
// any local account reads it out of /proc/<pid>/cmdline. Both binaries already default
// -token from COORD_TOKEN, so the env is the only place it belongs.
func TestShippedWorkerScriptsOmitTokenFlag(t *testing.T) {
	for _, path := range []string{
		"scripts/ops/workerfuzz_digger.sh",
		"scripts/vast/01_run_pool_worker.sh",
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		for n, line := range strings.Split(string(raw), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") {
				continue
			}
			if strings.Contains(trimmed, "-token") {
				t.Errorf("%s:%d passes the coordinator token on argv (CWE-214): %s", path, n+1, trimmed)
			}
		}
	}
}
