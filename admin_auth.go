package main

import (
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// adminTokenFromEnv returns HACKME_ADMIN_TOKEN when set (trimmed). Empty means auth is disabled.
func adminTokenFromEnv() string {
	return strings.TrimSpace(os.Getenv("HACKME_ADMIN_TOKEN"))
}

func adminAuthEnabled() bool {
	return adminTokenFromEnv() != ""
}

func extractAdminSecret(r *http.Request) string {
	if s := strings.TrimSpace(r.Header.Get("X-Hackme-Admin-Token")); s != "" {
		return s
	}
	const prefix = "Bearer "
	auth := r.Header.Get("Authorization")
	if len(auth) > len(prefix) && strings.EqualFold(auth[:len(prefix)], prefix) {
		return strings.TrimSpace(auth[len(prefix):])
	}
	return ""
}

func secretsEqualConstantTime(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// adminRequestAuthed reports whether the request carries a valid node admin secret (or auth is disabled).
func adminRequestAuthed(r *http.Request) bool {
	expected := adminTokenFromEnv()
	if expected == "" {
		return true
	}
	return secretsEqualConstantTime(extractAdminSecret(r), expected)
}

// requireAdminAuth returns false and writes HTTP 401 if HACKME_ADMIN_TOKEN is set and the request does not match.
func requireAdminAuth(w http.ResponseWriter, r *http.Request) bool {
	expected := adminTokenFromEnv()
	if expected == "" {
		return true
	}
	got := extractAdminSecret(r)
	if !secretsEqualConstantTime(got, expected) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-admin"`)
		http.Error(w, "admin authentication required", http.StatusUnauthorized)
		return false
	}
	return true
}

// coordinatorTokenFromSecrets loads the pool coordinator admin token (not the node HACKME_ADMIN_TOKEN).
func coordinatorTokenFromSecrets() string {
	var paths []string
	secretName := filepath.Join(".secrets", "hackme_coordinator_admin_token")
	if root := strings.TrimSpace(os.Getenv("HACKME_REPO_ROOT")); root != "" {
		paths = append(paths, filepath.Join(root, secretName))
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for i := 0; i < 6; i++ {
			paths = append(paths, filepath.Join(dir, secretName))
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
			paths = append(paths, filepath.Join(dir, secretName))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	paths = append(paths, secretName)
	for _, p := range paths {
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

func ensurePoolCoordinatorTokenEnv() {
	if strings.TrimSpace(os.Getenv("HACKME_POOL_COORDINATOR_TOKEN")) != "" {
		return
	}
	if t := coordinatorTokenFromSecrets(); t != "" {
		_ = os.Setenv("HACKME_POOL_COORDINATOR_TOKEN", t)
	}
}

// requestFromLoopback is true when the HTTP client is the local machine (not via reverse proxy).
func requestFromLoopback(r *http.Request) bool {
	if strings.TrimSpace(r.Header.Get("X-Forwarded-For")) != "" ||
		strings.TrimSpace(r.Header.Get("X-Real-IP")) != "" {
		return false
	}
	host := strings.TrimSpace(r.Host)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

// handleDesktopLocalAuth exposes HACKME_ADMIN_TOKEN to the dashboard on loopback only (desktop mode).
func handleDesktopLocalAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !envBool("HACKME_DESKTOP_MODE", false) || !requestFromLoopback(r) {
		http.NotFound(w, r)
		return
	}
	tok := adminTokenFromEnv()
	expose := envBool("HACKME_DESKTOP_EXPOSE_ADMIN_TOKEN", false)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	out := map[string]any{
		"ok":                     true,
		"admin_token_configured": tok != "",
		"hint":                   "token is embedded in dashboard HTML on loopback desktop mode; set HACKME_DESKTOP_EXPOSE_ADMIN_TOKEN=1 only for legacy clients",
	}
	if expose && tok != "" {
		out["admin_token"] = tok
	}
	_ = json.NewEncoder(w).Encode(out)
}

// resolveCoordinatorToken picks the coordinator bearer token. Never falls back to HACKME_ADMIN_TOKEN
// (that causes claim 401 on public pool).
func resolveCoordinatorToken(reqCoordToken string) string {
	if t := strings.TrimSpace(os.Getenv("HACKME_POOL_COORDINATOR_TOKEN")); t != "" {
		return t
	}
	if t := coordinatorTokenFromSecrets(); t != "" {
		return t
	}
	rt := strings.TrimSpace(reqCoordToken)
	if rt == "" {
		return ""
	}
	admin := adminTokenFromEnv()
	if admin != "" && secretsEqualConstantTime(rt, admin) {
		return ""
	}
	return rt
}
