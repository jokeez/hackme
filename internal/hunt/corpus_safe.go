package hunt

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ReadRegularCorpusFile reads path only when it is a non-symlink regular file.
// Hunt harnesses get the persistent corpus RW-bound; they must not plant
// symlinks that host-side collectors resolve into .env / secret paths.
// CorpusPathConfined reports whether path resolves under root after EvalSymlinks
// (blocks intermediate directory symlinks, not only final-component links).
func CorpusPathConfined(root, path string) bool {
	return corpusPathConfined(root, path)
}

func corpusPathConfined(root, path string) bool {
	root = strings.TrimSpace(root)
	path = strings.TrimSpace(path)
	if root == "" || path == "" {
		return false
	}
	absRoot, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return false
	}
	// Resolve the root too: it is host-chosen, and a legit symlink in its path
	// (macOS /tmp → /private/tmp, a symlinked home or data disk) would otherwise put
	// every resolved child "outside" and silently empty the corpus.
	if rr, rerr := filepath.EvalSymlinks(absRoot); rerr == nil {
		absRoot = rr
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		dirRes, derr := filepath.EvalSymlinks(filepath.Dir(path))
		if derr != nil {
			return false
		}
		resolved = filepath.Join(dirRes, filepath.Base(path))
	}
	absPath, err := filepath.Abs(filepath.Clean(resolved))
	if err != nil {
		return false
	}
	return pathUnderRoot(absRoot, absPath)
}

func ReadRegularCorpusFile(path string, maxSize int64) ([]byte, error) {
	return ReadRegularCorpusFileUnder("", path, maxSize)
}

// ReadRegularCorpusFileUnder is like ReadRegularCorpusFile but rejects paths that
// resolve outside root (harness-planted directory symlinks in RW corpus binds).
func ReadRegularCorpusFileUnder(root, path string, maxSize int64) ([]byte, error) {
	if root != "" && !corpusPathConfined(root, path) {
		return nil, fmt.Errorf("hunt corpus: path escapes root: %s", path)
	}
	// O_NOFOLLOW closes the Lstat-then-ReadFile race: a harness that swaps the
	// path for a symlink between the check and the read must not be followed.
	f, err := os.OpenFile(path, os.O_RDONLY|oNoFollow, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("hunt corpus: refuse non-regular %s", path)
	}
	if info.Size() <= 0 {
		return nil, fmt.Errorf("hunt corpus: empty %s", path)
	}
	if maxSize > 0 && info.Size() > maxSize {
		return nil, fmt.Errorf("hunt corpus: oversized %s", path)
	}
	if maxSize > 0 {
		return io.ReadAll(io.LimitReader(f, maxSize))
	}
	return io.ReadAll(f)
}

// isRegularCorpusFile reports whether path is a regular file (no symlink follow).
func isRegularCorpusFile(path string) (os.FileInfo, bool) {
	return isRegularCorpusFileUnder("", path)
}

func isRegularCorpusFileUnder(root, path string) (os.FileInfo, bool) {
	if root != "" && !corpusPathConfined(root, path) {
		return nil, false
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, false
	}
	return info, true
}
