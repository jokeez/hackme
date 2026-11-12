//go:build !windows

package hunt

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// Host writes into directories that are bind-mounted read-write into the Hunt cage
// (corpus dirs, .hunt-run, merge parents, AFL work dirs) must never follow a path the
// untrusted harness can plant. os.WriteFile / os.Create follow symlinks in every
// component, so a planted link let a harness overwrite or truncate any file the
// worker UID can write (fimasw, write-side follow-up to the read-side series).
//
// writeFileBeneath walks from root one component at a time with
// openat(O_NOFOLLOW|O_DIRECTORY) relative to the previous fd, then unlinks the final
// name and creates it O_CREAT|O_EXCL|O_NOFOLLOW relative to the parent fd. No step
// resolves a symlink and no step re-resolves a path string, so swapping a directory
// mid-operation cannot redirect the write.

// writeFileBeneath replaces path (which must be under root) with data.
func writeFileBeneath(root, path string, data []byte, perm os.FileMode) error {
	f, err := openFileBeneath(root, path, perm, true, true)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

// writeNewFileBeneath creates path under root only if it does not exist (os.ErrExist otherwise).
func writeNewFileBeneath(root, path string, data []byte, perm os.FileMode) error {
	f, err := openFileBeneath(root, path, perm, false, true)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

// createFileBeneath replaces path under root with a new empty file opened for writing.
func createFileBeneath(root, path string, perm os.FileMode) (*os.File, error) {
	return openFileBeneath(root, path, perm, true, true)
}

// mkdirAllBeneath creates dir (under root) and its parents without following symlinks.
func mkdirAllBeneath(root, dir string, perm os.FileMode) error {
	rel, err := relBeneath(root, dir)
	if err != nil {
		return err
	}
	fd, err := openRootFD(root)
	if err != nil {
		return err
	}
	if rel == "." {
		return unix.Close(fd)
	}
	last, err := walkDirsBeneath(fd, strings.Split(rel, string(filepath.Separator)), perm)
	if err != nil {
		return err
	}
	return unix.Close(last)
}

func openFileBeneath(root, path string, perm os.FileMode, replace, mkdirs bool) (*os.File, error) {
	rel, err := relBeneath(root, path)
	if err != nil {
		return nil, err
	}
	if rel == "." {
		return nil, fmt.Errorf("hunt safewrite: %s is the root itself", path)
	}
	parts := strings.Split(rel, string(filepath.Separator))
	name := parts[len(parts)-1]
	fd, err := openRootFD(root)
	if err != nil {
		return nil, err
	}
	dirfd := fd
	if len(parts) > 1 {
		mode := os.FileMode(0)
		if mkdirs {
			mode = 0o755
		}
		dirfd, err = walkDirsBeneath(fd, parts[:len(parts)-1], mode)
		if err != nil {
			return nil, err
		}
	}
	defer unix.Close(dirfd)
	if replace {
		// Drop whatever the harness left there (file or symlink) — relative to the
		// pinned parent fd, so a planted link is removed, never followed.
		if err := unix.Unlinkat(dirfd, name, 0); err != nil && !errors.Is(err, unix.ENOENT) {
			return nil, fmt.Errorf("hunt safewrite: unlink %s: %w", path, err)
		}
	}
	nfd, err := unix.Openat(dirfd, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, uint32(perm.Perm()))
	if err != nil {
		if errors.Is(err, unix.EEXIST) {
			return nil, os.ErrExist
		}
		return nil, fmt.Errorf("hunt safewrite: create %s: %w", path, err)
	}
	return os.NewFile(uintptr(nfd), path), nil
}

// walkDirsBeneath opens each directory component with O_NOFOLLOW relative to the
// previous fd (mkdir'ing missing ones when perm != 0). It consumes startFD and
// returns the fd of the last directory.
func walkDirsBeneath(startFD int, comps []string, perm os.FileMode) (int, error) {
	cur := startFD
	for _, c := range comps {
		if c == "" || c == "." || c == ".." {
			unix.Close(cur)
			return -1, fmt.Errorf("hunt safewrite: bad path component %q", c)
		}
		next, err := unix.Openat(cur, c, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if errors.Is(err, unix.ENOENT) && perm != 0 {
			if merr := unix.Mkdirat(cur, c, uint32(perm.Perm())); merr != nil && !errors.Is(merr, unix.EEXIST) {
				unix.Close(cur)
				return -1, fmt.Errorf("hunt safewrite: mkdir %s: %w", c, merr)
			}
			next, err = unix.Openat(cur, c, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		}
		unix.Close(cur)
		if err != nil {
			// ELOOP / ENOTDIR here means a symlink or file was planted in place of a dir.
			return -1, fmt.Errorf("hunt safewrite: refusing path component %q: %w", c, err)
		}
		cur = next
	}
	return cur, nil
}

func openRootFD(root string) (int, error) {
	abs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return -1, err
	}
	// The root is host-chosen (a bind source the cage cannot rename); its final
	// component must still not be a symlink.
	fd, err := unix.Open(abs, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, fmt.Errorf("hunt safewrite: open root %s: %w", abs, err)
	}
	return fd, nil
}

func relBeneath(root, path string) (string, error) {
	absRoot, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", err
	}
	absPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("hunt safewrite: %s is not under %s", path, root)
	}
	return rel, nil
}
