package hunt

import (
	"fmt"
	"os"
)

// ReadRegularCorpusFile reads path only when it is a non-symlink regular file.
// Hunt harnesses get the persistent corpus RW-bound; they must not plant
// symlinks that host-side collectors resolve into .env / secret paths.
func ReadRegularCorpusFile(path string, maxSize int64) ([]byte, error) {
	info, err := os.Lstat(path)
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
	return os.ReadFile(path)
}

// isRegularCorpusFile reports whether path is a regular file (no symlink follow).
func isRegularCorpusFile(path string) (os.FileInfo, bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, false
	}
	return info, true
}
