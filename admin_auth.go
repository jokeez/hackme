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
// for mint/burn/genesis, market orders, treasury spend, from_code, and security_audit.
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

// desktopLoopbackAdminOK is true for same-machine desktop dashboard traffic.
// Used so Start Worker / Mining controls keep working after restart even if the
// browser still holds a stale sessionStorage token (common after env repair).
func desktopLoopbackAdminOK(r *http.Request) bool {
	return envBool("HACKME_DESKTOP_MODE", false) &&
		requestFromLoopback(r) &&
		requestHostIsLoopbackLiteral(r) &&
		desktopMutatingOriginOK(r)
}

// requireAdminAuthOrDesktopLoopback accepts a valid admin token OR trusted desktop loopback.
// When HACKME_ADMIN_TOKEN is unset it never fail-opens for remote clients (audit C-money):
// only loopback Host + Origin gates pass (beginner solo / desktop).
func requireAdminAuthOrDesktopLoopback(w http.ResponseWriter, r *http.Request) bool {
	expected := adminTokenFromEnv()
	if expected != "" {
		if secretsEqualConstantTime(extractAdminSecret(r), expected) {
			return true
		}
		if desktopLoopbackAdminOK(r) {
			return true
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="hackme-admin"`)
		http.Error(w, "admin authentication required", http.StatusUnauthorized)
		return false
	}
	if requestFromLoopback(r) && requestHostIsLoopbackLiteral(r) && desktopMutatingOriginOK(r) {
		return true
	}
	return requireAdminAuthStrict(w, r)
}

// requestHostIsLoopbackLiteral is true when the HTTP Host is a literal loopback name.
// Blocks DNS-rebinding: TCP may be 127.0.0.1 while Host is attacker-controlled.
func requestHostIsLoopbackLiteral(r *http.Request) bool {
	if r == nil {
		return false
	}
	rh := strings.ToLower(strings.TrimSpace(r.Host))
	if h, _, err := net.SplitHostPort(rh); err == nil {
		rh = h
	}
	rh = strings.Trim(rh, "[]")
	return rh == "127.0.0.1" || rh == "localhost" || rh == "::1"
}

// requireDesktopLoopbackMutating applies the same Host/Origin DNS-rebind gate used by
// /api/tx/send (allowLoopbackDesktopDashboardAuth) to other desktop spend surfaces.
// No-op when HACKME_DESKTOP_MODE is off (public/integrator VPS).
func requireDesktopLoopbackMutating(w http.ResponseWriter, r *http.Request) bool {
	if !envBool("HACKME_DESKTOP_MODE", false) {
		return true
	}
	if requestFromLoopback(r) && requestHostIsLoopbackLiteral(r) && desktopMutatingOriginOK(r) {
		return true
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": "desktop mutating routes require loopback Host and same-origin",
		"code":  "desktop_origin_required",
	})
	return false
}

// desktopMutatingOriginOK rejects cross-site / same-site browser POSTs (CSRF-01).
// Chromium marks different ports on the same host as Sec-Fetch-Site=same-site; that
// must not authorize spend (cross-port localhost CSRF → simpleSign / Orders fund).
// Non-browser clients (no Sec-Fetch-Site / Origin) are allowed when already loopback-authed.
// When Origin is present, Host must be a loopback literal and Origin host+port must match
// the request Host (localhost ↔ 127.0.0.1 / ::1 aliases allowed on the same port).
func desktopMutatingOriginOK(r *http.Request) bool {
	if r == nil {
		return false
	}
	site := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")))
	if site == "cross-site" || site == "same-site" {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	if !requestHostIsLoopbackLiteral(r) {
		return false
	}
	ou, err := url.Parse(origin)
	if err != nil || ou == nil {
		return false
	}
	oh, op := originHostPort(ou)
	rh, rp := requestHostPort(r)
	if oh == "" || rh == "" {
		return false
	}
	if !loopbackHostAlias(oh, rh) {
		return false
	}
	// Port must match when both sides have one. Host without a port (rare) only
	// matches Origin default http/80 or https/443.
	if rp == "" {
		return op == "80" || op == "443"
	}
	return op == rp
}

func originHostPort(u *url.URL) (host, port string) {
	if u == nil {
		return "", ""
	}
	host = strings.ToLower(strings.TrimSpace(u.Hostname()))
	host = strings.Trim(host, "[]")
	port = u.Port()
	if port == "" {
		switch strings.ToLower(u.Scheme) {
		case "https":
			port = "443"
		default:
			port = "80"
		}
	}
	return host, port
}

func requestHostPort(r *http.Request) (host, port string) {
	if r == nil {
		return "", ""
	}
	rh := strings.ToLower(strings.TrimSpace(r.Host))
	h, p, err := net.SplitHostPort(rh)
	if err != nil {
		return strings.Trim(rh, "[]"), ""
	}
	return strings.Trim(h, "[]"), p
}

func loopbackHostAlias(a, b string) bool {
	if a == b {
		return true
	}
	return (a == "localhost" && (b == "127.0.0.1" || b == "::1")) ||
		(b == "localhost" && (a == "127.0.0.1" || a == "::1"))
}

// coordinatorWorkerTokenFromSecrets loads the pool miner/worker token (not admin).
func coordinatorWorkerTokenFromSecrets() string {
	return operator.ReadCoordinatorWorkerToken()
}

func ensurePoolCoordinatorTokenEnv() {
	if strings.TrimSpace(os.Getenv("HACKME_POOL_COORDINATOR_TOKEN")) != "" {
		return
	}
	// Never promote admin settle/register token into the worker-scoped env.
	if t := coordinatorWorkerTokenFromSecrets(); t != "" {
		_ = os.Setenv("HACKME_POOL_COORDINATOR_TOKEN", t)
	}
}

// ensurePoolCoordinatorAdminTokenEnv loads Dig/Hunt pool_sync credentials from on-disk
// secrets when env is empty. Scoped Orders publish (pool.orders.token) goes into
// HACKME_POOL_ORDERS_PUBLISH_TOKEN only — never into the admin env (rc18.1).
// Worker pool.miner.token is never used for campaign register / settle-pull.
func ensurePoolCoordinatorAdminTokenEnv() {
	if strings.TrimSpace(os.Getenv("HACKME_POOL_ORDERS_PUBLISH_TOKEN")) == "" &&
		strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_ORDERS_PUBLISH_TOKEN")) == "" &&
		strings.TrimSpace(os.Getenv("HACKME_ORDERS_PUBLISH_TOKEN")) == "" {
		if t := operator.ReadOrdersPublishToken(); t != "" {
			_ = os.Setenv("HACKME_POOL_ORDERS_PUBLISH_TOKEN", t)
		}
	}
	if strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_ADMIN_TOKEN")) != "" {
		return
	}
	if strings.TrimSpace(os.Getenv("HACKME_POOL_COORDINATOR_ADMIN_TOKEN")) != "" {
		return
	}
	if t := operator.ReadCoordinatorAdminToken(); t != "" {
		_ = os.Setenv("HACKME_POOL_COORDINATOR_ADMIN_TOKEN", t)
	}
}

// requireLocalInventoryAuth gates host inventory GETs (disks, GPU, rig detect).
// Admin token or loopback+literal Host — same policy as /api/local/disks.
func requireLocalInventoryAuth(w http.ResponseWriter, r *http.Request) bool {
	if adminAuthEnabled() && adminRequestAuthed(r) {
		return true
	}
	if requestFromLoopback(r) && requestHostIsLoopbackLiteral(r) {
		return true
	}
	http.Error(w, "local inventory requires loopback or admin auth", http.StatusForbidden)
	return false
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

// canonicalRelayAdminToken returns an explicit relay credential for forwarding
// signed transfers to the canonical chain URL.
//
// Never falls back to HACKME_ADMIN_TOKEN or the client Authorization header —
// that exfils the node admin secret to the canonical host (or a MITM /
// mis-set HACKME_CANONICAL_CHAIN_URL). Fully signed mempool txs do not need
// admin on current public /api/tx/send; set HACKME_CANONICAL_RELAY_ADMIN_TOKEN
// only when the remote still requires it (desktop_mode_up.sh can sync it).
func canonicalRelayAdminToken(_ *http.Request) string {
	return strings.TrimSpace(os.Getenv("HACKME_CANONICAL_RELAY_ADMIN_TOKEN"))
}

// desktopAdminTokenEmbedScript returns an inline script that sets
// window.__HACKME_EMBEDDED_ADMIN_TOKEN__ only when desktop mode + loopback +
// HACKME_DESKTOP_EXPOSE_ADMIN_TOKEN=1 (H2 fail-closed).
func desktopAdminTokenEmbedScript(r *http.Request) string {
	if !envBool("HACKME_DESKTOP_MODE", false) || !requestFromLoopback(r) || !adminAuthEnabled() {
		return ""
	}
	if !requestHostIsLoopbackLiteral(r) {
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

// handleDesktopLocalAuth exposes HACKME_ADMIN_TOKEN to the dashboard on loopback
// only (desktop mode). Token return is fail-closed:
//   - HACKME_DESKTOP_EXPOSE_ADMIN_TOKEN=1, or
//   - Unix-socket peer, or
//   - browser document context: Origin + Referer both loopback same-origin
//     (Sec-Fetch-Site alone is spoofable — curl with a forged header gets nothing).
//
// EXPOSE also gates HTML embed + the optional UI note. CLI must use EXPOSE=1 or a token file.
func handleDesktopLocalAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !envBool("HACKME_DESKTOP_MODE", false) || !requestFromLoopback(r) || !requestHostIsLoopbackLiteral(r) {
		http.NotFound(w, r)
		return
	}
	// Same Origin / Sec-Fetch-Site gate as spend paths (CSRF / local page exfil).
	if !desktopMutatingOriginOK(r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "desktop local-auth requires same-origin",
			"code":  "desktop_origin_required",
		})
		return
	}
	tok := adminTokenFromEnv()
	expose := envBool("HACKME_DESKTOP_EXPOSE_ADMIN_TOKEN", false)
	allowToken := expose || requestViaUnixSocket(r) || desktopLocalAuthBrowserDocumentOK(r)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	out := map[string]any{
		"ok":                     true,
		"admin_token_configured": tok != "",
		"desktop_mode":           true,
		"exposed":                expose,
		"hint":                   "admin_token returned when HACKME_DESKTOP_EXPOSE_ADMIN_TOKEN=1, Unix-socket, or browser Origin+Referer loopback same-origin; Sec-Fetch-Site alone is not enough; rotate token on shared machines",
	}
	if tok != "" && allowToken {
		out["admin_token"] = tok
	}
	_ = json.NewEncoder(w).Encode(out)
}

// requestViaUnixSocket reports a UDS peer (RemoteAddr "@" / empty host forms used by net/http).
func requestViaUnixSocket(r *http.Request) bool {
	if r == nil {
		return false
	}
	addr := strings.TrimSpace(r.RemoteAddr)
	if addr == "@" || strings.HasPrefix(addr, "@") {
		return true
	}
	// Go unix peers often look like "@" or lack a host:port TCP shape.
	if _, _, err := net.SplitHostPort(addr); err != nil && addr != "" && !strings.Contains(addr, ".") {
		if !strings.Contains(addr, ":") {
			return true
		}
	}
	return false
}

// desktopLocalAuthBrowserDocumentOK requires both Origin and Referer to be loopback
// same-origin with Host, plus Sec-Fetch-Site=same-origin. Spoofing only Sec-Fetch-Site
// (curl -H) must not yield the admin token when EXPOSE=0.
func desktopLocalAuthBrowserDocumentOK(r *http.Request) bool {
	if r == nil {
		return false
	}
	site := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")))
	if site != "same-origin" {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	referer := strings.TrimSpace(r.Header.Get("Referer"))
	if origin == "" || referer == "" {
		return false
	}
	if !requestHostIsLoopbackLiteral(r) {
		return false
	}
	ou, err := url.Parse(origin)
	if err != nil || ou == nil {
		return false
	}
	ru, err := url.Parse(referer)
	if err != nil || ru == nil {
		return false
	}
	oh, op := originHostPort(ou)
	rh, rp := originHostPort(ru)
	hh, hp := requestHostPort(r)
	if oh == "" || rh == "" || hh == "" {
		return false
	}
	if !loopbackHostAlias(oh, hh) || !loopbackHostAlias(rh, hh) {
		return false
	}
	if hp == "" {
		return (op == "80" || op == "443") && (rp == "80" || rp == "443")
	}
	return op == hp && rp == hp
}

// resolveCoordinatorToken picks the coordinator worker bearer token. Never falls back to
// HACKME_ADMIN_TOKEN (claim 401) or the coordinator admin settle/register secret.
func resolveCoordinatorToken(reqCoordToken string) string {
	if t := strings.TrimSpace(os.Getenv("HACKME_POOL_COORDINATOR_TOKEN")); t != "" {
		return t
	}
	if t := coordinatorWorkerTokenFromSecrets(); t != "" {
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
	// Reject coordinator admin secret pasted as "worker" token.
	if adm := operator.ReadCoordinatorAdminToken(); adm != "" && secretsEqualConstantTime(rt, adm) {
		return ""
	}
	return rt
}
