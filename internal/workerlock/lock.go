// Package workerlock prevents duplicate workerpoh/workerfuzz processes for the
// same WORKER_ID (flock + pidfile), which otherwise causes 429 claim storms.
package workerlock

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Guard holds an exclusive flock on a per-(kind,workerID) pidfile.
type Guard struct {
	path string
	f    *os.File
}

// Acquire locks logs/workerlock-<kind>-<safeWorkerID>.pid under dir (default: logs/).
// Returns ErrAlreadyRunning if another live process holds the lock.
func Acquire(kind, workerID, dir string) (*Guard, error) {
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
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, fmt.Sprintf("workerlock-%s-%s.pid", kind, workerID))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := lockFile(f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("%w: %s (kind=%s worker_id=%s)", ErrAlreadyRunning, path, kind, workerID)
	}
	if err := f.Truncate(0); err != nil {
		_ = unlockFile(f)
		_ = f.Close()
		return nil, err
	}
	if _, err := fmt.Fprintf(f, "%d\n", os.Getpid()); err != nil {
		_ = unlockFile(f)
		_ = f.Close()
		return nil, err
	}
	_ = f.Sync()
	return &Guard{path: path, f: f}, nil
}

// Release unlocks and closes the pidfile (best-effort).
func (g *Guard) Release() {
	if g == nil || g.f == nil {
		return
	}
	_ = unlockFile(g.f)
	_ = g.f.Close()
	g.f = nil
}

// Path returns the pidfile path.
func (g *Guard) Path() string {
	if g == nil {
		return ""
	}
	return g.path
}

// ErrAlreadyRunning means another process already holds this worker lock.
var ErrAlreadyRunning = fmt.Errorf("worker already running")

// Held reports whether another live process currently holds the lock for kind+workerID.
// Used by hybrid process-mode to avoid spawn/restart storms when digger is already up.
func Held(kind, workerID, dir string) bool {
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
	path := filepath.Join(dir, fmt.Sprintf("workerlock-%s-%s.pid", kind, workerID))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return false
	}
	defer f.Close()
	if err := lockFile(f); err != nil {
		return true
	}
	_ = unlockFile(f)
	return false
}

func sanitize(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if len(out) > 80 {
		out = out[:80]
	}
	return out
}
