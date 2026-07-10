package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
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
	req, err := http.NewRequest(http.MethodPost, nodeLoopbackBase()+"/api/worker/start", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", admin)
	cl := &http.Client{Timeout: 90 * time.Second}
	res, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("worker/start HTTP %d", res.StatusCode)
	}
	var out struct {
		OK bool `json:"ok"`
	}
	_ = json.NewDecoder(res.Body).Decode(&out)
	if !out.OK {
		return fmt.Errorf("worker/start rejected")
	}
	return nil
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
		time.Sleep(12 * time.Second)
		log.Printf("pool worker watchdog: enabled (every %s)", interval)
		var lastRestartUnix int64
		for {
			if miningPaused() {
				time.Sleep(interval)
				continue
			}
			if !a.workerProcessRunning() {
				now := time.Now().Unix()
				if now-lastRestartUnix < 30 {
					time.Sleep(interval)
					continue
				}
				if err := a.restartPoolWorkerViaAPI(); err != nil {
					log.Printf("pool worker watchdog: restart failed: %v", err)
				} else {
					lastRestartUnix = now
					a.workerMu.Lock()
					wid := a.workerID
					a.workerMu.Unlock()
					log.Printf("pool worker watchdog: restarted pool worker (%s)", wid)
				}
			}
			time.Sleep(interval)
		}
	}()
}
