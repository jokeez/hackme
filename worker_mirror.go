package main

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hackme/internal/workerid"
)

const coordinatorWorkerFreshSec int64 = 300

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

func pruneWorkerCoordinatorMirror(workerIDs []string) {
	if len(workerIDs) == 0 {
		return
	}
	mirror := loadWorkerCoordinatorMirror()
	changed := false
	for _, id := range workerIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := mirror.Workers[id]; ok {
			delete(mirror.Workers, id)
			changed = true
		}
	}
	if !changed {
		return
	}
	path := workerCoordinatorMirrorPath()
	if b, err := json.MarshalIndent(mirror, "", "  "); err == nil {
		_ = os.WriteFile(path, b, 0o600)
	}
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
		wid = workerid.DefaultDesktop()
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

func coordinatorWorkerRowFromStats(ws map[string]any, workerID string) (map[string]any, bool) {
	if ws == nil {
		return nil, false
	}
	row, ok := coordinatorWorkersMap(ws)[strings.TrimSpace(workerID)]
	if !ok {
		return nil, false
	}
	m := mapFromAny(row)
	if len(m) == 0 {
		return nil, false
	}
	return m, true
}

func coordinatorRowOnline(row map[string]any) bool {
	if row == nil {
		return false
	}
	if v, ok := row["online"]; ok {
		switch x := v.(type) {
		case bool:
			return x
		case string:
			return strings.EqualFold(x, "true") || x == "1"
		}
	}
	lastSeen := int64(parseAnyFloat(row["last_seen_unix"]))
	if lastSeen <= 0 {
		return false
	}
	return time.Now().Unix()-lastSeen <= coordinatorWorkerFreshSec
}

func poolWorkerWallSessionSec(logRoot string) float64 {
	wp := latestWorkerpohWorkerLogPath(logRoot)
	if wp == "" {
		wp = latestWorkerpohLogPath(logRoot)
	}
	ux := workerLogStartedUnix(wp)
	if ux <= 0 {
		return 0
	}
	sec := float64(time.Now().Unix() - ux)
	if sec < 0 {
		return 0
	}
	return math.Round(sec*100) / 100
}

func poolWorkerLogStartedUnix(logRoot string) int64 {
	wp := latestWorkerpohWorkerLogPath(logRoot)
	if wp == "" {
		wp = latestWorkerpohLogPath(logRoot)
	}
	return workerLogStartedUnix(wp)
}

// cachedCoordinatorWorkerRow returns the live coordinator row for workerID (cache-first, then fetch).
func (a *app) cachedCoordinatorWorkerRow(workerID string) (map[string]any, bool) {
	wid := strings.TrimSpace(workerID)
	if wid == "" {
		wid = a.desktopWorkerID()
	}
	if cached, _, ok := copyCachedWorkStats(workStatsCacheStaleMaxSec); ok {
		if row, found := coordinatorWorkerRowFromStats(cached, wid); found {
			return row, true
		}
	}
	base := strings.TrimRight(a.coordinatorBaseURL(), "/")
	if base == "" {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ws, err := fetchCoordinatorWorkStats(ctx, base, false)
	if err != nil {
		return nil, false
	}
	return coordinatorWorkerRowFromStats(ws, wid)
}

// overlayPoolWorkerMetrics aligns dashboard /api/metrics with coordinator + worker log (pool follower mode).
func (a *app) overlayPoolWorkerMetrics(s *MetricsSnapshot) {
	if a == nil || s == nil {
		return
	}
	logRoot := filepath.Join(resolveWorkerRepoRoot(strings.TrimSpace(a.dataDir)), "logs")
	running, _, wid := a.desktopWorkerLiveStatus()
	if !running && !a.workerProcessRunning() {
		return
	}
	if sec := poolWorkerWallSessionSec(logRoot); sec > 0 {
		s.MiningSessionSec = sec
	}
	if row, ok := a.cachedCoordinatorWorkerRow(wid); ok {
		gh := parseAnyFloat(row["hashrate_gh_s"])
		if gh > 0 {
			s.PoolWorkerHashrateGHS = gh
			if s.MiningGPUTotalGHS <= 0 {
				s.MiningGPUTotalGHS = gh
			}
		}
	}
}
