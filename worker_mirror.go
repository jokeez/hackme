package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// workerCoordinatorMirror persists last-known per-worker rows from coordinator work/stats
// so desktop UIs still show worker-kapa-pc after coordinator prune (idle rigs).
type workerCoordinatorMirror struct {
	Workers map[string]map[string]any `json:"workers"`
}

func workerCoordinatorMirrorPath() string {
	if dd := strings.TrimSpace(os.Getenv("HACKME_DATA_DIR")); dd != "" {
		return filepath.Join(dd, "worker_coordinator_mirror.json")
	}
	return filepath.Join("data", "worker_coordinator_mirror.json")
}

func loadWorkerCoordinatorMirror() workerCoordinatorMirror {
	out := workerCoordinatorMirror{Workers: map[string]map[string]any{}}
	raw, err := os.ReadFile(workerCoordinatorMirrorPath())
	if err != nil {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	if out.Workers == nil {
		out.Workers = map[string]map[string]any{}
	}
	return out
}

func persistWorkerCoordinatorMirrorFromStats(ws map[string]any) {
	workers := coordinatorWorkersMap(ws)
	if len(workers) == 0 {
		return
	}
	mirror := loadWorkerCoordinatorMirror()
	now := time.Now().Unix()
	for id, v := range workers {
		row := mapFromAny(v)
		if len(row) == 0 {
			continue
		}
		row["mirror_snapshot_unix"] = now
		mirror.Workers[id] = row
	}
	path := workerCoordinatorMirrorPath()
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	if b, err := json.MarshalIndent(mirror, "", "  "); err == nil {
		_ = os.WriteFile(path, b, 0o600)
	}
}

func (a *app) desktopWorkerID() string {
	wid := strings.TrimSpace(a.workerID)
	if wid == "" {
		wid = strings.TrimSpace(os.Getenv("WORKER_ID"))
	}
	if wid == "" {
		wid = "worker-kapa-pc"
	}
	return wid
}

func (a *app) desktopWorkerLiveStatus() (running bool, hashrateGHS float64, workerID string) {
	a.workerMu.Lock()
	defer a.workerMu.Unlock()
	workerID = strings.TrimSpace(a.workerID)
	running = a.workerCmd != nil && a.workerCmd.Process != nil && a.workerCmd.ProcessState == nil
	hashrateGHS = a.workerHashrate
	if !running {
		logRoot := filepath.Join(resolveWorkerRepoRoot(strings.TrimSpace(a.dataDir)), "logs")
		measuredGH := parseWorkerpohMeasuredGHs(logRoot)
		if workerLogFresh(logRoot, 120) && measuredGH > 0 {
			running = true
			hashrateGHS = measuredGH
			if workerID == "" {
				workerID = workerIDFromLatestWorkerpohLog(logRoot)
			}
		}
	}
	if workerID == "" {
		workerID = a.desktopWorkerID()
	}
	return running, hashrateGHS, workerID
}

// enrichWorkStatsDesktopWorker injects the desktop worker row when coordinator omitted it (prune / not yet registered).
func (a *app) enrichWorkStatsDesktopWorker(ws map[string]any) {
	if ws == nil {
		return
	}
	ensureCoordinatorWorkersMap(ws)
	workers := coordinatorWorkersMap(ws)
	wid := a.desktopWorkerID()
	if row, ok := workers[wid]; ok {
		persistWorkerCoordinatorMirrorFromStats(map[string]any{"workers": map[string]any{wid: row}})
		return
	}
	running, gh, liveWid := a.desktopWorkerLiveStatus()
	// Do not replace configured WORKER_ID with log-inferred id (e.g. mirror tests, multi-rig env).
	if liveWid != "" && liveWid == wid {
		// same id
	} else if liveWid != "" && strings.TrimSpace(a.workerID) == "" && strings.TrimSpace(os.Getenv("WORKER_ID")) == "" {
		wid = liveWid
	}
	injected := map[string]any{}
	if mirrorRow, ok := loadWorkerCoordinatorMirror().Workers[wid]; ok && len(mirrorRow) > 0 {
		for k, v := range mirrorRow {
			injected[k] = v
		}
	}
	if len(injected) == 0 && !running {
		ws["desktop_worker_id"] = wid
		ws["desktop_worker_on_coordinator"] = false
		return
	}
	if running {
		injected["online"] = true
		injected["coordinator_pruned"] = false
		injected["local_pool_worker"] = true
		injected["last_seen_unix"] = time.Now().Unix()
		if gh > 0 {
			injected["hashrate_gh_s"] = gh
		}
	} else {
		injected["online"] = false
		injected["coordinator_pruned"] = true
	}
	workers[wid] = injected
	ws["workers"] = workers
	if asUint64(ws["workers_count"]) < uint64(len(workers)) {
		ws["workers_count"] = uint64(len(workers))
	}
	ws["desktop_worker_id"] = wid
	ws["desktop_worker_on_coordinator"] = false
	ws["desktop_worker_injected"] = true
}
