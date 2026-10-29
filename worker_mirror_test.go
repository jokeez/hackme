package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestEnrichWorkStatsDesktopWorkerFromMirror(t *testing.T) {
	if out, err := exec.Command("pgrep", "-f", "workerpoh").Output(); err == nil && len(out) > 0 {
		t.Skip("workerpoh running — skip mirror/pruned branch test")
	}
	dir := t.TempDir()
	work := filepath.Join(dir, "work")
	_ = os.MkdirAll(filepath.Join(work, "logs"), 0o755)
	// Isolate resolveWorkerRepoRoot / log freshness from the live checkout logs.
	t.Setenv("HACKME_DATA_DIR", filepath.Join(dir, "data"))
	t.Setenv("HACKME_REPO_ROOT", work)
	t.Setenv("HACKME_WORKING_DIR", work)
	_ = os.MkdirAll(filepath.Join(dir, "data"), 0o755)
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	mirrorID := "worker-mirror-gate-test"
	mirror := workerCoordinatorMirror{
		Workers: map[string]map[string]any{
			mirrorID: {
				"accepted_ranges":   uint64(71),
				"accepted_attempts": uint64(68681728),
				"payout_hmc":        0.371377,
			},
		},
	}
	raw, err := json.Marshal(mirror)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "data", "worker_coordinator_mirror.json"), raw, 0o600)

	t.Setenv("WORKER_ID", mirrorID)
	a := &app{dataDir: filepath.Join(dir, "data")}
	ws := map[string]any{
		"workers": map[string]any{
			"worker-vps-62-01": map[string]any{"payout_hmc": 0.1},
		},
		"workers_count": uint64(1),
	}
	a.enrichWorkStatsDesktopWorker(ws)
	workers := coordinatorWorkersMap(ws)
	if _, ok := workers[mirrorID]; !ok {
		t.Fatal("expected desktop worker injected")
	}
	row := mapFromAny(workers[mirrorID])
	if row["coordinator_pruned"] != true {
		t.Fatalf("expected pruned flag, got %#v", row["coordinator_pruned"])
	}
	if parseAnyFloat(row["payout_hmc"]) != 0.371377 {
		t.Fatalf("payout %v", row["payout_hmc"])
	}
}

func TestEnrichWorkStatsDesktopWorkerAliasesPublicPseudonym(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HACKME_DATA_DIR", filepath.Join(dir, "data"))
	t.Setenv("WORKER_ID", "worker-kapa-pc")
	_ = os.MkdirAll(filepath.Join(dir, "data"), 0o755)
	a := &app{dataDir: filepath.Join(dir, "data"), workerID: "worker-kapa-pc"}
	pseudo := publicWorkerPseudoKey("worker-kapa-pc")
	if pseudo == "" {
		t.Fatal("expected publicWorkerPseudoKey")
	}
	ws := map[string]any{
		"workers": map[string]any{
			pseudo: map[string]any{
				"online":            true,
				"accepted_attempts": uint64(108835977176711),
				"payout_hmc":        441.07,
				"hashrate_gh_s":     6.5,
			},
			"other-rig": map[string]any{"payout_hmc": 1.0},
		},
		"workers_count":   uint64(2),
		"workers_keys":    "pseudonym_v1",
	}
	a.enrichWorkStatsDesktopWorker(ws)
	workers := coordinatorWorkersMap(ws)
	if _, ok := workers[pseudo]; ok {
		t.Fatal("expected public hash twin removed after alias")
	}
	row, ok := workers["worker-kapa-pc"]
	if !ok {
		t.Fatal("expected friendly desktop id aliased from public key")
	}
	m := mapFromAny(row)
	if m["local_pool_worker"] == true {
		t.Fatal("aliased row must not be local_pool_worker (registering)")
	}
	if ws["desktop_worker_on_coordinator"] != true {
		t.Fatalf("on_coordinator=%v", ws["desktop_worker_on_coordinator"])
	}
	if parseAnyFloat(m["payout_hmc"]) != 441.07 {
		t.Fatalf("payout %v", m["payout_hmc"])
	}
}
