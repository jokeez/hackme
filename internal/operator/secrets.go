package operator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const coordinatorAdminTokenFile = "hackme_coordinator_admin_token"

// CoordinatorAdminTokenPaths returns candidate paths for the pool coordinator admin token.
func CoordinatorAdminTokenPaths() []string {
	secretName := filepath.Join(".secrets", coordinatorAdminTokenFile)
	var paths []string
	seen := make(map[string]struct{})
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		paths = append(paths, p)
	}

	if root := strings.TrimSpace(os.Getenv("HACKME_REPO_ROOT")); root != "" {
		add(filepath.Join(root, secretName))
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for i := 0; i < 6; i++ {
			add(filepath.Join(dir, secretName))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	if dataDir := strings.TrimSpace(os.Getenv("HACKME_DATA_DIR")); dataDir != "" {
		dir := filepath.Dir(dataDir)
		for i := 0; i < 4; i++ {
			add(filepath.Join(dir, secretName))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	add(secretName)
	return paths
}

// openSecretFile rejects symlinks and files with group/other permission bits (must be ≤0600).
func openSecretFile(path string) ([]byte, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("secret file is a symlink: %s", path)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("secret file is not a regular file: %s", path)
	}
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		return nil, fmt.Errorf("secret file permissions too open (%04o; need 0600): %s", perm, path)
	}
	return os.ReadFile(path)
}

// ReadCoordinatorAdminToken loads the first line from .secrets/hackme_coordinator_admin_token.
// Skips world/group-readable files and symlinks (H51).
func ReadCoordinatorAdminToken() string {
	for _, p := range CoordinatorAdminTokenPaths() {
		b, err := openSecretFile(p)
		if err != nil {
			continue
		}
		line := strings.TrimSpace(strings.SplitN(string(b), "\n", 2)[0])
		if line != "" {
			return line
		}
	}
	return ""
}
