package hunt

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Object store for Hunt harness binaries (issue #8 Phase 1).
// Metadata stays in SQLite; bytes live under HarnessObjectDir()/ab/<hash>.

var (
	harnessDirMu sync.RWMutex
	harnessDir   string
)

// SetHarnessObjectDir configures the content-addressed harness root (tests / coordinator boot).
func SetHarnessObjectDir(dir string) {
	harnessDirMu.Lock()
	defer harnessDirMu.Unlock()
	harnessDir = strings.TrimSpace(dir)
}

// HarnessObjectDir returns HACKME_HUNT_HARNESS_DIR, SetHarnessObjectDir, or "".
func HarnessObjectDir() string {
	if v := strings.TrimSpace(os.Getenv("HACKME_HUNT_HARNESS_DIR")); v != "" {
		return v
	}
	harnessDirMu.RLock()
	defer harnessDirMu.RUnlock()
	return harnessDir
}

// DefaultHarnessObjectDirBesideDB places harness files next to the fuzz sqlite file.
// Always returns a DB-scoped directory ({db}.harness) so multiple DBs under the same
// parent (e.g. /tmp/*.db in swarm/repro gates) do not share one /tmp/harness and
// reject publishes with "already bound to different binary".
// Outside temp parents, a legacy sibling "harness/" is renamed once into the scoped
// path so existing VPS blobs keep serving. Temp parents never adopt a shared legacy
// dir — that poisons fresh DBs with stale recipe-keyed binaries.
func DefaultHarnessObjectDirBesideDB(fuzzDBPath string) string {
	fuzzDBPath = strings.TrimSpace(fuzzDBPath)
	if fuzzDBPath == "" {
		return ""
	}
	abs, err := filepath.Abs(fuzzDBPath)
	if err != nil {
		abs = fuzzDBPath
	}
	scoped := abs + ".harness"
	parent := filepath.Dir(abs)
	legacy := filepath.Join(parent, "harness")
	if !dirExists(scoped) && dirExists(legacy) && !isTempParent(parent) {
		if err := os.Rename(legacy, scoped); err != nil {
			return legacy
		}
	}
	return scoped
}

func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

func isTempParent(parent string) bool {
	parent = filepath.Clean(parent)
	tmp := filepath.Clean(os.TempDir())
	if parent == tmp {
		return true
	}
	// Also treat /var/tmp and explicit TMPDIR children as non-migrating.
	for _, p := range []string{"/tmp", "/var/tmp", tmp} {
		p = filepath.Clean(p)
		if parent == p || strings.HasPrefix(parent+string(filepath.Separator), p+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// HarnessObjectPath returns <dir>/<hash[:2]>/<hash> (0600 files).
func HarnessObjectPath(dir, hash string) (string, error) {
	dir = strings.TrimSpace(dir)
	hash = strings.TrimSpace(strings.ToLower(hash))
	if dir == "" {
		return "", fmt.Errorf("hunt objectstore: empty dir")
	}
	if !ValidHarnessHash(hash) {
		return "", fmt.Errorf("hunt objectstore: invalid hash")
	}
	prefix := hash
	if len(prefix) >= 2 {
		prefix = hash[:2]
	}
	return SafeJoinUnder(dir, prefix, hash)
}

// WriteHarnessObject atomically writes harness bytes to the object store.
func WriteHarnessObject(dir, hash string, data []byte) (string, error) {
	path, err := HarnessObjectPath(dir, hash)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return "", err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return path, nil
}

// ReadHarnessObject loads bytes from the object store if present.
func ReadHarnessObject(dir, hash string) ([]byte, error) {
	path, err := HarnessObjectPath(dir, hash)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("hunt objectstore: empty file")
	}
	if len(data) > maxHarnessArtifactBytes {
		return nil, fmt.Errorf("hunt objectstore: exceeds %d bytes", maxHarnessArtifactBytes)
	}
	return data, nil
}

// HarnessObjectExists reports whether the on-disk object is present and non-empty.
func HarnessObjectExists(dir, hash string) bool {
	path, err := HarnessObjectPath(dir, hash)
	if err != nil {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular() && st.Size() > 0
}

func contentSHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// BackfillHarnessArtifactsToDisk exports SQLite BLOBs to disk and clears binary_blob.
// Safe to re-run. Returns number of rows migrated.
func BackfillHarnessArtifactsToDisk(ctx context.Context, db *sql.DB, dir string) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("hunt objectstore: no database")
	}
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = HarnessObjectDir()
	}
	if dir == "" {
		return 0, fmt.Errorf("hunt objectstore: harness dir not configured")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	rows, err := db.QueryContext(ctx,
		`SELECT harness_hash, binary_blob FROM hunt_harness_artifacts WHERE length(binary_blob) > 0`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var hash string
		var blob []byte
		if err := rows.Scan(&hash, &blob); err != nil {
			return n, err
		}
		if len(blob) == 0 || !ValidHarnessHash(hash) {
			continue
		}
		if _, err := WriteHarnessObject(dir, hash, blob); err != nil {
			return n, fmt.Errorf("backfill %s: %w", hash, err)
		}
		fp := contentSHA256Hex(blob)
		_, err := db.ExecContext(ctx,
			`UPDATE hunt_harness_artifacts
			 SET binary_blob=X'', content_sha256=CASE WHEN content_sha256='' THEN ? ELSE content_sha256 END
			 WHERE harness_hash=?`, fp, hash)
		if err != nil {
			return n, err
		}
		n++
	}
	return n, rows.Err()
}
