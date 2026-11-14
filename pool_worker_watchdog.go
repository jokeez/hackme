package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"hackme/internal/logsafe"
)

func envPoolWorkerWatchdogEnabled() bool {
	if v := strings.TrimSpace(os.Getenv("HACKME_WORKER_WATCHDOG")); v != "" {
		return envBool("HACKME_WORKER_WATCHDOG", false)
	}
	// Default on for desktop + public pool (Windows/Linux installers).
	return envBool("HACKME_DESKTOP_MODE", false) && strings.TrimSpace(os.Getenv("HACKME_POOL_COORDINATOR_URL")) != ""
}

func poolWorkerWatchdogInterval() time.Duration {
	sec := 45
	if v := strings.TrimSpace(os.Getenv("HACKME_WORKER_WATCHDOG_SEC")); v != "" {
		if x, err := strconv.Atoi(v); err == nil && x >= 20 && x <= 600 {
			sec = x
		}
	}
	return time.Duration(sec) * time.Second
}

func nodeLoopbackBase() string {
	addr := strings.TrimSpace(os.Getenv("HACKME_BIND_ADDR"))
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}
	return strings.TrimRight(addr, "/")
}

// restartPoolWorkerViaAPI starts the pool worker using an in-process handler call.
// Avoids Windows loopback HTTP races and keeps the same auth path as the dashboard.
func (a *app) restartPoolWorkerViaAPI() error {
	admin := strings.TrimSpace(os.Getenv("HACKME_ADMIN_TOKEN"))
	if admin == "" {
		return fmt.Errorf("HACKME_ADMIN_TOKEN missing")
	}
	coord := a.coordinatorBaseURL()
	if coord == "" {
		return fmt.Errorf("pool coordinator URL not configured")
	}
	body := map[string]any{"coord_url": coord}
	if wid := strings.TrimSpace(os.Getenv("WORKER_ID")); wid != "" {
		body["worker_id"] = wid
	} else if wid := strings.TrimSpace(a.workerID); wid != "" {
		body["worker_id"] = wid
	}
	if v := strings.TrimSpace(os.Getenv("HACKME_GPU_BACKEND")); v != "" {
		body["gpu_backend"] = v
	}
	if v := strings.TrimSpace(os.Getenv("HACKME_WORKER_BATCH_SIZE")); v != "" {
		if x, err := strconv.ParseUint(v, 10, 64); err == nil && x > 0 {
			body["batch_size"] = x
		}
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/worker/start", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", admin)
	// Loopback Host so desktop CSRF / Host checks stay satisfied if added later.
	req.Host = "127.0.0.1:8080"
	req.RemoteAddr = "127.0.0.1:0"
	rec := httptest.NewRecorder()
	a.handleWorkerStart(rec, req)
	res := rec.Result()
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	if res.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(b))
		if msg == "" {
			msg = res.Status
		}
		return fmt.Errorf("worker/start HTTP %d: %s (admin_token_len=%d)", res.StatusCode, msg, len(admin))
	}
	var out struct {
		OK bool `json:"ok"`
	}
	_ = json.Unmarshal(b, &out)
	if !out.OK {
		return fmt.Errorf("worker/start rejected: %s", strings.TrimSpace(string(b)))
	}
	return nil
}

// stopPoolWorkerViaAPI force-stops the managed worker (and fleet) via the same path as the dashboard.
func (a *app) stopPoolWorkerViaAPI() error {
	admin := strings.TrimSpace(os.Getenv("HACKME_ADMIN_TOKEN"))
	if admin == "" {
		return fmt.Errorf("HACKME_ADMIN_TOKEN missing")
	}
	req := httptest.NewRequest(http.MethodPost, "/api/worker/stop", nil)
	req.Header.Set("X-Hackme-Admin-Token", admin)
	req.Host = "127.0.0.1:8080"
	req.RemoteAddr = "127.0.0.1:0"
	rec := httptest.NewRecorder()
	a.handleWorkerStop(rec, req)
	res := rec.Result()
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return fmt.Errorf("worker/stop HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

func (a *app) forceRestartPoolWorker(reason string) error {
	if err := a.stopPoolWorkerViaAPI(); err != nil {
		log.Printf("pool worker watchdog: stop before restart (%s): %s", logsafe.ID(reason), logsafe.Err(err))
		time.Sleep(200 * time.Millisecond)
		if a.workerProcessRunning() {
			return fmt.Errorf("stop failed and worker still running (%s): %w", reason, err)
		}
	}
	a.workerRunningCacheMu.Lock()
	a.workerRunningCached = false
	a.workerRunningCacheAt = 0
	a.workerRunningCacheMu.Unlock()
	// Brief settle so Windows releases exe/log handles before respawn.
	time.Sleep(800 * time.Millisecond)
	if err := a.restartPoolWorkerViaAPI(); err != nil {
		return err
	}
	return nil
}

func (a *app) poolWorkerWatchdogTick(nowUnix int64) (action string, detail string) {
	if miningPaused() {
		return "paused", ""
	}
	logRoot := filepath.Join(resolveWorkerRepoRoot(strings.TrimSpace(a.dataDir)), "logs")
	a.workerMu.Lock()
	wid := strings.TrimSpace(a.workerID)
	startedAt := a.workerStartedAt
	subprocess := a.workerCmd != nil && a.workerCmd.Process != nil && a.workerCmd.ProcessState == nil
	a.workerMu.Unlock()
	if wid == "" {
		wid = strings.TrimSpace(os.Getenv("WORKER_ID"))
	}

	alive := a.workerProcessRunning()
	if !alive {
		// Desktop CUDA/autostart often runs outside a.workerCmd. Detection can lag
		// briefly after submit; treating that as "missing" causes forceRestart →
		// killall → SIGTERM loops every watchdog interval (worker never stays up).
		if envBool("HACKME_DESKTOP_MODE", false) {
			hb := workerSubmitHeartbeatUnixSince(logRoot, wid, 0)
			if hb > 0 && nowUnix-hb < poolWorkerHeartbeatStaleSec() {
				return "ok", "desktop_recent_submit"
			}
			// Fresh workerpoh log with ghs= is enough even if lock/pidfile is mid-rotate.
			if workerActiveFromLog(logRoot, poolWorkerHeartbeatStaleSec()) {
				return "ok", "desktop_fresh_worker_log"
			}
		}
		return "restart_missing", "process_not_running"
	}

	// Process exists (or log looks live) — still restart if submit heartbeat went silent.
	// This catches Windows freezes where workerpoh is alive but blocked and never submits.
	staleSec := poolWorkerHeartbeatStaleSec()
	graceSec := poolWorkerHeartbeatGraceSec()
	if !subprocess {
		// External / desktop autostart path: only use heartbeat when we have a worker id
		// or nonce/log signal; avoid thrashing when detection is log-only and flaky.
		if wid == "" && workerSubmitHeartbeatUnixSince(logRoot, "", startedAt) <= 0 {
			return "ok", "external_no_heartbeat_signal"
		}
		// Without a known session start, do not force-restart external workers on stale
		// residue (would spawn a second managed worker via /api/worker/start).
		if startedAt <= 0 {
			return "ok", "external_no_started_at"
		}
	}
	if need, reason := workerHeartbeatNeedsRestart(logRoot, wid, startedAt, nowUnix, staleSec, graceSec); need {
		return "restart_frozen", reason
	}
	return "ok", ""
}

func (a *app) startPoolWorkerWatchdog() {
	if !envPoolWorkerWatchdogEnabled() {
		return
	}
	if strings.TrimSpace(a.coordinatorBaseURL()) == "" {
		return
	}
	interval := poolWorkerWatchdogInterval()
	go func() {
		// Fast first start so desktop miners do not need a manual Start Worker click.
		time.Sleep(3 * time.Second)
		log.Printf("pool worker watchdog: enabled (every %s, heartbeat_stale=%ds grace=%ds)",
			interval, poolWorkerHeartbeatStaleSec(), poolWorkerHeartbeatGraceSec())
		var lastRestartUnix int64
		first := true
		for {
			now := time.Now().Unix()
			action, detail := a.poolWorkerWatchdogTick(now)
			switch action {
			case "paused", "ok":
				// nothing
			case "restart_missing", "restart_frozen":
				// Desktop: avoid thrash — CUDA init + first submits need headroom.
				minGap := int64(30)
				if envBool("HACKME_DESKTOP_MODE", false) {
					minGap = 120
				}
				if !first && now-lastRestartUnix < minGap {
					time.Sleep(interval)
					continue
				}
				first = false
				if err := a.forceRestartPoolWorker(detail); err != nil {
					log.Printf("pool worker watchdog: %s restart failed (%s): %v", action, logsafe.ID(detail), err)
				} else {
					lastRestartUnix = now
					a.workerMu.Lock()
					wid := a.workerID
					a.workerMu.Unlock()
					log.Printf("pool worker watchdog: %s worker=%s reason=%s", action, logsafe.ID(wid), logsafe.ID(detail))
				}
			}
			time.Sleep(interval)
		}
	}()
}
