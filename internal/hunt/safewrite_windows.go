//go:build windows

package hunt

import (
	"os"
	"path/filepath"
)

// Windows runs no Hunt cage (bwrap is Linux-only), so there is no untrusted writer
// sharing these directories; plain writes are kept for the build to stay portable.

func writeFileBeneath(_ string, path string, data []byte, perm os.FileMode) error {
	_ = os.Remove(path)
	return os.WriteFile(path, data, perm)
}

func writeNewFileBeneath(_ string, path string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return werr
}

func createFileBeneath(_ string, path string, perm os.FileMode) (*os.File, error) {
	_ = os.Remove(path)
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
}

func mkdirAllBeneath(_ string, dir string, perm os.FileMode) error {
	return os.MkdirAll(filepath.Clean(dir), perm)
}
