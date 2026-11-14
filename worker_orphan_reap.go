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
	wid := strings.TrimSpace(workerID)
	if wid == "" {
		return // unknown own id: never reap other rigs' locks in a shared dir
	}
	// Own id + fleet suffixes (worker-id-cuda0, …) left after a crash; other ids in the same
	// dir belong to other nodes / systemd miners and stay alone.
	results, err := workerlock.ReapAllMatching(dir, func(w string) bool { return workerIDOwned(w, []string{wid}) })
	for _, res := range results {
		if res.Killed {
			log.Printf("pool worker: reaped orphan %s worker_id=%s pid=%d dir=%s", res.Kind, logsafe.ID(res.Worker), res.PID, logsafe.ID(dir))
		}
	}
	if err != nil {
		log.Printf("pool worker: orphan reap dir=%s: %v", logsafe.ID(dir), err)
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
	if strings.TrimSpace(workerID) == "" {
		workerID = strings.TrimSpace(os.Getenv("WORKER_ID"))
	}
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
	wid := strings.TrimSpace(workerID)
	if wid == "" {
		wid = strings.TrimSpace(os.Getenv("WORKER_ID"))
	}
	// Only this node's worker id — never every miner on the machine.
	killExternalWorkerFleet(repoRoot, []string{wid})
	killExternalWorkerfuzzFleet([]string{wid})
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
func reapOrphanPoolWorkersAtBoot(repoRoot string) {
	logDir := filepath.Join(".", "logs")
	reapOrphanPoolWorkers(logDir, strings.TrimSpace(os.Getenv("WORKER_ID")), repoRoot)
}

func killExternalWorkerfuzzFleet(ids []string) {
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/F", "/IM", "workerfuzz.exe").Run()
		return
	}
	if allow := ownedByWorkerIDs(ids); allow != nil {
		killWorkerfuzzProcs(allow)
	}
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
