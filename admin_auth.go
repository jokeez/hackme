package main

import (
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"

	"hackme/internal/operator"
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
// When the token is unset this still fail-opens (legacy loopback/dev). Prefer requireAdminAuthStrict
// for treasury spend, from_code, and security_audit.
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

// requireAdminAuthStrict fails closed when HACKME_ADMIN_TOKEN is unset (C3).
func requireAdminAuthStrict(w http.ResponseWriter, r *http.Request) bool {
	expected := adminTokenFromEnv()
	if expected == "" {
		w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-admin"`)
		http.Error(w, "admin authentication required (HACKME_ADMIN_TOKEN unset)", http.StatusUnauthorized)
		return false
	}
	got := extractAdminSecret(r)
	if !secretsEqualConstantTime(got, expected) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-admin"`)
		http.Error(w, "admin authentication required", http.StatusUnauthorized)
		return false
	}
	return true
}

// desktopMutatingOriginOK rejects cross-site browser POSTs (CSRF-01).
// Non-browser clients (no Sec-Fetch-Site / Origin) are allowed when already loopback-authed.
func desktopMutatingOriginOK(r *http.Request) bool {
	if r == nil {
		return false
	}
	site := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")))
	if site == "cross-site" {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	ou, err := url.Parse(origin)
	if err != nil || ou == nil {
		return false
	}
	oh := strings.ToLower(strings.TrimSpace(ou.Hostname()))
	rh := strings.ToLower(strings.TrimSpace(r.Host))
	if h, _, err := net.SplitHostPort(rh); err == nil {
		rh = h
	}
	rh = strings.Trim(rh, "[]")
	if oh == "" || rh == "" {
		return false
	}
	return oh == rh || (oh == "localhost" && (rh == "127.0.0.1" || rh == "::1")) ||
		(rh == "localhost" && (oh == "127.0.0.1" || oh == "::1"))
}

// coordinatorTokenFromSecrets loads the pool coordinator admin token (not the node HACKME_ADMIN_TOKEN).
func coordinatorTokenFromSecrets() string {
	return operator.ReadCoordinatorAdminToken()
}

func ensurePoolCoordinatorTokenEnv() {
	if strings.TrimSpace(os.Getenv("HACKME_POOL_COORDINATOR_TOKEN")) != "" {
		return
	}
	if t := coordinatorTokenFromSecrets(); t != "" {
		_ = os.Setenv("HACKME_POOL_COORDINATOR_TOKEN", t)
	}
}

// requestFromLoopback is true when the TCP peer is the local machine.
// Uses RemoteAddr + IP.IsLoopback only — never Host (spoofable) or forwarded headers.
func requestFromLoopback(r *http.Request) bool {
	if r == nil {
		return false
	}
	// Forwarded headers are untrusted for loopback privilege (no proxy-CIDR allowlist here).
	if strings.TrimSpace(r.Header.Get("X-Forwarded-For")) != "" ||
		strings.TrimSpace(r.Header.Get("X-Real-IP")) != "" {
		return false
	}
	host := strings.TrimSpace(r.RemoteAddr)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// canonicalRelayAdminToken is used when desktop forwards a signed transfer to hackme.tech (remote still requires admin on older builds).
func canonicalRelayAdminToken(r *http.Request) string {
	if t := strings.TrimSpace(os.Getenv("HACKME_CANONICAL_RELAY_ADMIN_TOKEN")); t != "" {
		return t
	}
	if r != nil {
		if t := strings.TrimSpace(extractAdminSecret(r)); t != "" {
			return t
		}
	}
	return adminTokenFromEnv()
}

// desktopAdminTokenEmbedScript returns an inline script that sets
// window.__HACKME_EMBEDDED_ADMIN_TOKEN__ only when desktop mode + loopback +
// HACKME_DESKTOP_EXPOSE_ADMIN_TOKEN=1 (H2 fail-closed).
func desktopAdminTokenEmbedScript(r *http.Request) string {
	if !envBool("HACKME_DESKTOP_MODE", false) || !requestFromLoopback(r) || !adminAuthEnabled() {
		return ""
	}
	if !envBool("HACKME_DESKTOP_EXPOSE_ADMIN_TOKEN", false) {
		return ""
	}
	t := adminTokenFromEnv()
	if t == "" {
		return ""
	}
	b, _ := json.Marshal(t)
	return `<script>window.__HACKME_EMBEDDED_ADMIN_TOKEN__=` + string(b) + `;</script>`
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
	// H2: never return the raw token unless explicitly opted in.
	expose := envBool("HACKME_DESKTOP_EXPOSE_ADMIN_TOKEN", false)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	out := map[string]any{
		"ok":                     true,
		"admin_token_configured": tok != "",
		"hint":                   "set HACKME_DESKTOP_EXPOSE_ADMIN_TOKEN=1 to return admin_token on loopback Sync",
	}
	if tok != "" && expose {
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
