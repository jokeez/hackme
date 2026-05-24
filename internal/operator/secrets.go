package operator

import (
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

// ReadCoordinatorAdminToken loads the first line from .secrets/hackme_coordinator_admin_token.
func ReadCoordinatorAdminToken() string {
	for _, p := range CoordinatorAdminTokenPaths() {
		b, err := os.ReadFile(p)
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
