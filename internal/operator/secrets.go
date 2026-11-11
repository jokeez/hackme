package operator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const coordinatorAdminTokenFile = "hackme_coordinator_admin_token"
const coordinatorWorkerTokenFile = "hackme_coordinator_worker_token"
const ordersPublishTokenFile = "pool.orders.token"
const ordersPrepaidHubSecretFile = "pool.orders.prepaid_hub.secret"
const ordersPrepaidHubSecretAltFile = "hackme_orders_prepaid_hub_secret"

// CoordinatorAdminTokenPaths returns candidate paths for the pool coordinator admin token.
func CoordinatorAdminTokenPaths() []string {
	return coordinatorTokenPaths(coordinatorAdminTokenFile)
}

// CoordinatorWorkerTokenPaths returns candidate paths for the pool miner/worker token.
func CoordinatorWorkerTokenPaths() []string {
	return coordinatorTokenPaths(coordinatorWorkerTokenFile)
}

func coordinatorTokenPaths(secretFile string) []string {
	secretName := filepath.Join(".secrets", secretFile)
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
	return readCoordinatorTokenFile(CoordinatorAdminTokenPaths())
}

// ReadCoordinatorWorkerToken loads the first line from .secrets/hackme_coordinator_worker_token.
// Never confuse this with the admin settle/register token.
func ReadCoordinatorWorkerToken() string {
	return readCoordinatorTokenFile(CoordinatorWorkerTokenPaths())
}

// OrdersPublishTokenPaths returns candidate paths for the scoped B2B Orders publish token
// (campaign register + settle-pull only — not admin, not miner pool.miner.token).
func OrdersPublishTokenPaths() []string {
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
		add(filepath.Join(root, ordersPublishTokenFile))
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for i := 0; i < 6; i++ {
			add(filepath.Join(dir, ordersPublishTokenFile))
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
			add(filepath.Join(dir, ordersPublishTokenFile))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	add(ordersPublishTokenFile)
	return paths
}

// ReadOrdersPublishToken loads pool.orders.token (release / Orders profile).
// Same hardening as admin/worker secrets: no symlinks, mode ≤ 0600.
func ReadOrdersPublishToken() string {
	for _, p := range OrdersPublishTokenPaths() {
		b, err := openSecretFile(p)
		if err != nil {
			continue
		}
		line := strings.TrimSpace(strings.SplitN(string(b), "\n", 2)[0])
		if line != "" && line != "REPLACE_WITH_ORDERS_PUBLISH_TOKEN" {
			return line
		}
	}
	return ""
}

// OrdersPrepaidHubSecretPaths returns candidate paths for the hub prepaid ticket
// signing secret (≠ pool.orders.token / publish fleet credential).
func OrdersPrepaidHubSecretPaths() []string {
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
	addNamed := func(base string, names ...string) {
		for _, n := range names {
			add(filepath.Join(base, n))
			add(filepath.Join(base, ".secrets", n))
		}
	}
	names := []string{ordersPrepaidHubSecretFile, ordersPrepaidHubSecretAltFile}
	if root := strings.TrimSpace(os.Getenv("HACKME_REPO_ROOT")); root != "" {
		addNamed(root, names...)
	}
	if ex, err := os.Executable(); err == nil {
		dir := filepath.Dir(ex)
		for i := 0; i < 6; i++ {
			addNamed(dir, names...)
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
			addNamed(dir, names...)
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	for _, n := range names {
		add(n)
		add(filepath.Join(".secrets", n))
	}
	return paths
}

// ReadOrdersPrepaidHubSecret loads pool.orders.prepaid_hub.secret (or
// .secrets/hackme_orders_prepaid_hub_secret). Distinct from the Orders publish
// fleet token so a leaked pool.orders.token alone cannot mint hubv1 tickets.
func ReadOrdersPrepaidHubSecret() string {
	for _, p := range OrdersPrepaidHubSecretPaths() {
		b, err := openSecretFile(p)
		if err != nil {
			continue
		}
		line := strings.TrimSpace(strings.SplitN(string(b), "\n", 2)[0])
		if line != "" && line != "REPLACE_WITH_ORDERS_PREPAID_HUB_SECRET" {
			return line
		}
	}
	return ""
}

func readCoordinatorTokenFile(paths []string) string {
	for _, p := range paths {
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
