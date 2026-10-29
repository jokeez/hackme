package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"hackme/internal/logsafe"
	"hackme/internal/workerlock"
)

func workerLockDir(logDir string) string {
	if v := strings.TrimSpace(os.Getenv("HACKME_WORKER_LOCK_DIR")); v != "" {
		return v
	}
	if strings.TrimSpace(logDir) != "" {
		return logDir
	}
	return "logs"
}

// absoluteWorkerLockDir returns a stable absolute lock directory for spawn + reap.
// Critical on Linux apt/desktop: node may chdir to ~/.local/share/hackme while
// worker cmd.Dir is /opt/hackme — relative "logs" would split and leave orphans.
func absoluteWorkerLockDir(logDir string) string {
	dir := workerLockDir(logDir)
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	return abs
}

func reapLockDir(dir string, workerID string) {
	if strings.TrimSpace(dir) == "" {
		return
	}
	_ = os.MkdirAll(dir, 0o755)
	// ReapAll covers fleet suffixes (worker-id-gpu0, …) left after a crash.
	results, err := workerlock.ReapAll(dir)
	for _, res := range results {
		if res.Killed {
			log.Printf("pool worker: reaped orphan %s worker_id=%s pid=%d dir=%s", res.Kind, logsafe.ID(res.Worker), res.PID, logsafe.ID(dir))
		}
	}
	if err != nil {
		log.Printf("pool worker: orphan reap dir=%s: %v", logsafe.ID(dir), err)
	}
	wid := strings.TrimSpace(workerID)
	if wid == "" {
		return
	}
	for _, kind := range []string{"workerpoh", "workerfuzz"} {
		res, err := workerlock.Reap(kind, wid, dir)
		if res.Killed {
			log.Printf("pool worker: reaped orphan %s worker_id=%s pid=%d", kind, logsafe.ID(wid), res.PID)
		} else if res.WasHeld && err != nil {
			log.Printf("pool worker: orphan reap %s worker_id=%s: %s", kind, logsafe.ID(wid), logsafe.Err(err))
		}
	}
}

// reapOrphanPoolWorkers kills leftover workerpoh/workerfuzz that survive a node crash
// and still hold per-worker locks. Must run BEFORE truncating worker_participant.log.
// Without this, POST /api/worker/start after node restart returns ok+pid then the child
// exits immediately on ErrAlreadyRunning — leaving the rig unstartable via official paths.
func reapOrphanPoolWorkers(logDir, workerID, repoRoot string) {
	lockDir := absoluteWorkerLockDir(logDir)
	reapLockDir(lockDir, workerID)

	// Linux apt/desktop: also clear locks under repoRoot/logs when workers ran with
	// cmd.Dir=/opt/hackme and default relative lock path.
	if repoRoot = strings.TrimSpace(repoRoot); repoRoot != "" {
		alt := filepath.Join(repoRoot, "logs")
		if absAlt, err := filepath.Abs(alt); err == nil {
			alt = absAlt
		}
		if alt != lockDir {
			reapLockDir(alt, workerID)
		}
	}

	// Belt-and-suspenders: same fleet kill used by /api/worker/stop (Windows IM + Unix pkill).
	killExternalWorkerFleet(repoRoot)
	killExternalWorkerfuzzFleet()

	wid := strings.TrimSpace(workerID)
	if wid == "" {
		wid = strings.TrimSpace(os.Getenv("WORKER_ID"))
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		held := false
		if wid != "" {
			held = workerlock.Held("workerpoh", wid, lockDir) || workerlock.Held("workerfuzz", wid, lockDir)
		}
		if !held {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	if wid != "" && (workerlock.Held("workerpoh", wid, lockDir) || workerlock.Held("workerfuzz", wid, lockDir)) {
		log.Printf("pool worker: warning: lock still held after orphan reap worker_id=%s dir=%s", logsafe.ID(wid), logsafe.ID(lockDir))
	}
}

// reapOrphanPoolWorkersAtBoot clears any workerlock holders left from a previous node process.
// Skip when the GPU worker is owned by a separate supervisor (systemd hackme-kapa-worker,
// WORKER_AUTOSTART=0): boot reap was killing live mining on every desktop-node restart.
func reapOrphanPoolWorkersAtBoot(repoRoot string) {
	if strings.TrimSpace(os.Getenv("HACKME_SKIP_ORPHAN_REAP")) == "1" {
		log.Printf("pool worker: skip boot orphan reap (HACKME_SKIP_ORPHAN_REAP=1)")
		return
	}
	if strings.TrimSpace(os.Getenv("WORKER_AUTOSTART")) == "0" ||
		strings.TrimSpace(os.Getenv("HACKME_WORKER_WATCHDOG")) == "0" {
		log.Printf("pool worker: skip boot orphan reap (external worker supervisor)")
		return
	}
	logDir := filepath.Join(".", "logs")
	reapOrphanPoolWorkers(logDir, strings.TrimSpace(os.Getenv("WORKER_ID")), repoRoot)
}

func killExternalWorkerfuzzFleet() {
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/F", "/IM", "workerfuzz.exe").Run()
		return
	}
	killWorkerfuzzProcs(nil)
}

// rotateWorkerParticipantLog renames an existing participant log so O_TRUNC cannot
// create sparse holes against an orphan still writing at an old offset.
func rotateWorkerParticipantLog(logPath string) {
	st, err := os.Stat(logPath)
	if err != nil || st.Size() == 0 {
		return
	}
	prev := logPath + ".prev"
	_ = os.Remove(prev)
	if err := os.Rename(logPath, prev); err != nil {
		log.Printf("pool worker: could not rotate %s: %v", logPath, err)
	}
}
