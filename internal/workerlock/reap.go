package workerlock

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ReapResult describes one attempt to clear a held worker lock by killing its PID.
type ReapResult struct {
	Kind    string
	Worker  string
	Path    string
	PID     int
	WasHeld bool
	Killed  bool
}

// LockPath returns the pidfile path for kind+workerID under dir.
func LockPath(kind, workerID, dir string) string {
	kind = sanitize(kind)
	workerID = sanitize(workerID)
	if kind == "" {
		kind = "worker"
	}
	if workerID == "" {
		workerID = "default"
	}
	if dir == "" {
		dir = "logs"
	}
	return filepath.Join(dir, fmt.Sprintf("workerlock-%s-%s.pid", kind, workerID))
}

// ReadPID reads the PID stored in the lock file (best-effort; readable while locked).
func ReadPID(kind, workerID, dir string) (int, error) {
	path := LockPath(kind, workerID, dir)
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	line := strings.TrimSpace(string(b))
	if line == "" {
		return 0, fmt.Errorf("empty pidfile: %s", path)
	}
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	pid, err := strconv.Atoi(line)
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid pid in %s: %q", path, line)
	}
	return pid, nil
}

// Reap kills the process holding kind+workerID (if any) and waits briefly for the lock to free.
// Never kills the calling process. Safe to call when the lock is free (no-op).
func Reap(kind, workerID, dir string) (ReapResult, error) {
	res := ReapResult{
		Kind:   sanitize(kind),
		Worker: sanitize(workerID),
		Path:   LockPath(kind, workerID, dir),
	}
	if res.Kind == "" {
		res.Kind = "worker"
	}
	if res.Worker == "" {
		res.Worker = "default"
	}
	res.WasHeld = Held(kind, workerID, dir)
	pid, err := ReadPID(kind, workerID, dir)
	if err == nil && pid > 0 {
		res.PID = pid
	}
	if !res.WasHeld {
		// The OS file lock dies with its owner, so an unheld lock has no live owner. The pid
		// file is stale and its PID may now belong to an unrelated process (PIDs are reused):
		// never signal it. (This SIGTERMed `go test` in multi-package runs.)
		return res, nil
	}
	if res.PID > 0 && res.PID != os.Getpid() && processAlive(res.PID) {
		if err := killPID(res.PID); err != nil {
			return res, fmt.Errorf("kill pid %d (%s): %w", res.PID, res.Path, err)
		}
		res.Killed = true
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !Held(kind, workerID, dir) {
			return res, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	if Held(kind, workerID, dir) {
		return res, fmt.Errorf("lock still held after reap: %s (pid=%d)", res.Path, res.PID)
	}
	return res, nil
}

// ReapAll scans dir for workerlock-*.pid files and reaps each held lock.
func ReapAll(dir string) ([]ReapResult, error) {
	return ReapAllMatching(dir, nil)
}

// ReapAllMatching is ReapAll restricted to lock files whose worker id passes keep (nil keeps
// all). Several nodes and systemd miners can share one lock dir: a node must only reap its
// own workers, never another rig's live miner.
func ReapAllMatching(dir string, keep func(workerID string) bool) ([]ReapResult, error) {
	if dir == "" {
		dir = "logs"
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []ReapResult
	var firstErr error
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		kind, wid, ok := parseLockFileName(e.Name())
		if !ok {
			continue
		}
		if keep != nil && !keep(wid) {
			continue
		}
		res, err := Reap(kind, wid, dir)
		if res.WasHeld || res.Killed || res.PID > 0 {
			out = append(out, res)
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return out, firstErr
}

func parseLockFileName(base string) (kind, workerID string, ok bool) {
	if !strings.HasPrefix(base, "workerlock-") || !strings.HasSuffix(base, ".pid") {
		return "", "", false
	}
	mid := strings.TrimSuffix(strings.TrimPrefix(base, "workerlock-"), ".pid")
	if mid == "" {
		return "", "", false
	}
	for _, k := range []string{"workerpoh", "workerfuzz", "worker"} {
		if mid == k {
			return k, "default", true
		}
		prefix := k + "-"
		if strings.HasPrefix(mid, prefix) {
			return k, mid[len(prefix):], true
		}
	}
	i := strings.IndexByte(mid, '-')
	if i <= 0 {
		return mid, "default", true
	}
	return mid[:i], mid[i+1:], true
}
