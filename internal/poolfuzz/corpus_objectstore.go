package poolfuzz

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Issue #8 Phase 2: large corpus / snapshot bytes live on disk; SQLite holds a small marker.

const (
	corpusObjMarkerPrefix = "@obj:"
	corpusObjMinBytes     = 512 // below this, keep inline in SQLite
)

var (
	corpusDirMu sync.RWMutex
	corpusDir   string
)

// SetCorpusObjectDir configures the pool corpus object root (tests / coordinator boot).
func SetCorpusObjectDir(dir string) {
	corpusDirMu.Lock()
	defer corpusDirMu.Unlock()
	corpusDir = strings.TrimSpace(dir)
}

// CorpusObjectDir returns HACKME_POOL_CORPUS_DIR, SetCorpusObjectDir, or "".
func CorpusObjectDir() string {
	if v := strings.TrimSpace(os.Getenv("HACKME_POOL_CORPUS_DIR")); v != "" {
		return v
	}
	corpusDirMu.RLock()
	defer corpusDirMu.RUnlock()
	return corpusDir
}

// DefaultCorpusObjectDirBesideDB places corpus objects next to the fuzz sqlite file.
// Always returns a DB-scoped directory ("<dbpath>.corpus-objects"). Legacy sibling
// "corpus-objects/" is renamed once into the scoped path when present.
func DefaultCorpusObjectDirBesideDB(fuzzDBPath string) string {
	fuzzDBPath = strings.TrimSpace(fuzzDBPath)
	if fuzzDBPath == "" {
		return ""
	}
	abs, err := filepath.Abs(fuzzDBPath)
	if err != nil {
		abs = fuzzDBPath
	}
	scoped := abs + ".corpus-objects"
	parent := filepath.Dir(abs)
	legacy := filepath.Join(parent, "corpus-objects")
	if _, err := os.Stat(scoped); err != nil && os.IsNotExist(err) {
		if st, err2 := os.Stat(legacy); err2 == nil && st.IsDir() && !corpusTempParent(parent) {
			if err := os.Rename(legacy, scoped); err != nil {
				return legacy
			}
		}
	}
	return scoped
}

func corpusTempParent(parent string) bool {
	parent = filepath.Clean(parent)
	tmp := filepath.Clean(os.TempDir())
	for _, p := range []string{"/tmp", "/var/tmp", tmp} {
		p = filepath.Clean(p)
		if parent == p || strings.HasPrefix(parent+string(filepath.Separator), p+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func corpusObjectPath(dir, kind, id string) (string, error) {
	dir = strings.TrimSpace(dir)
	kind = strings.TrimSpace(kind)
	id = strings.ToLower(strings.TrimSpace(id))
	if dir == "" || kind == "" || id == "" {
		return "", fmt.Errorf("poolfuzz corpus object: bad path")
	}
	if strings.Contains(id, "..") || strings.ContainsAny(id, `/\`) || strings.Contains(kind, "..") || strings.ContainsAny(kind, `/\`) {
		return "", fmt.Errorf("poolfuzz corpus object: invalid id")
	}
	if len(id) < 8 {
		return "", fmt.Errorf("poolfuzz corpus object: short id")
	}
	prefix := id[:2]
	full := filepath.Join(dir, kind, prefix, id)
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	absFull, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	sep := string(os.PathSeparator)
	if absFull != absDir && !strings.HasPrefix(absFull, absDir+sep) {
		return "", fmt.Errorf("poolfuzz corpus object: escapes root")
	}
	return absFull, nil
}

func writeCorpusObject(kind, id string, data []byte) (marker []byte, err error) {
	dir := CorpusObjectDir()
	if dir == "" || len(data) < corpusObjMinBytes {
		return data, nil
	}
	if id == "" {
		id = sha256Hex(data)
	}
	path, err := corpusObjectPath(dir, kind, id)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	return []byte(corpusObjMarkerPrefix + id), nil
}

func readCorpusObject(stored []byte) ([]byte, error) {
	if !isCorpusObjMarker(stored) {
		return stored, nil
	}
	id := strings.TrimPrefix(string(stored), corpusObjMarkerPrefix)
	dir := CorpusObjectDir()
	if dir == "" {
		return nil, fmt.Errorf("poolfuzz corpus object: dir not set for marker %s", id)
	}
	// Try common kinds.
	for _, kind := range []string{"snapshot", "seed", "expected"} {
		path, err := corpusObjectPath(dir, kind, id)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			return data, nil
		}
	}
	return nil, fmt.Errorf("poolfuzz corpus object missing: %s", id)
}

func isCorpusObjMarker(b []byte) bool {
	return len(b) > len(corpusObjMarkerPrefix) && strings.HasPrefix(string(b), corpusObjMarkerPrefix)
}
