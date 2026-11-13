package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkerIDHasMinedOnlyOnAcceptedSubmit(t *testing.T) {
	dir := t.TempDir()
	w := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	w("workerpoh-worker-box-20261113T155332.log", `claim rejected: http 403 reason="payout_address_locked:locked=HMC-a:submitted=HMC-b"`)
	if workerIDHasMined(dir, "worker-box") {
		t.Fatal("a locked-out id has never mined; it must not be kept")
	}
	w("workerpoh-worker-box-abc123-20261113T160000.log", "submit ok found=false batch=1")
	if workerIDHasMined(dir, "worker-box") {
		t.Fatal("logs of worker-box-abc123 must not count for worker-box")
	}
	w("workerpoh-worker-box-20261113T170000.log", "submit ok found=false batch=1")
	if !workerIDHasMined(dir, "worker-box") {
		t.Fatal("an id that already mined must be kept")
	}
}
