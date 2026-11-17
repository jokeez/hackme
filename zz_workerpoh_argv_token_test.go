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
