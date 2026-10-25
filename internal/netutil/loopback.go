// Package netutil holds small shared network helpers used by node and pool packages.
package netutil

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// DenyAdminTokenRedirect refuses to follow redirects for clients that attach
// X-Hackme-Admin-Token (Go's default client can forward custom headers on 3xx).
func DenyAdminTokenRedirect(_ *http.Request, _ []*http.Request) error {
	return fmt.Errorf("netutil: refusing HTTP redirect (admin token)")
}

// HTTPClientNoRedirect returns an http.Client that never follows redirects.
func HTTPClientNoRedirect(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, CheckRedirect: DenyAdminTokenRedirect}
}

// IsLoopbackHost reports whether host is a loopback name or address.
func IsLoopbackHost(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	host = strings.Trim(host, "[]")
	if host == "127.0.0.1" || host == "localhost" || host == "::1" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// IsLoopbackURL reports whether u's hostname is loopback (http/https URLs).
// Unlike substring Contains checks, hosts like 127.0.0.1.attacker.example are NOT loopback.
// Userinfo is rejected: forms like http://127.0.0.1:8080@evil.example dial evil.example
// while a naive string-slicer can mis-read the userinfo as the host (report #24).
func IsLoopbackURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.User != nil && u.User.String() != "" {
		return false
	}
	host := u.Hostname()
	if host == "" {
		// bare host:port without scheme
		if h, _, err := net.SplitHostPort(raw); err == nil {
			host = h
		} else {
			host = raw
		}
	}
	return IsLoopbackHost(host)
}

// LooksRemoteCoordinatorURL is the inverse of IsLoopbackURL for empty-safe checks.
func LooksRemoteCoordinatorURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	return !IsLoopbackURL(raw)
}

// SafeHTTPBaseURL reports whether raw is an http(s) URL with a host and no userinfo.
// Used for coordinator / settle bases so credentials are never dialed to userinfo hosts.
// Prefer SafeHTTPURLForEgress for outbound money/status dials (also blocks metadata/private).
func SafeHTTPBaseURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	if u.User != nil && u.User.String() != "" {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return true
	default:
		return false
	}
}

// SafeHTTPURLForEgress is SafeHTTPBaseURL plus AssertHostSafeForEgress (metadata /
// link-local always; loopback/private unless HACKME_ALLOW_PRIVATE_EGRESS).
// Use for wallet/canonical public-chain dials.
func SafeHTTPURLForEgress(raw string) bool {
	return safeHTTPURLForEgress(raw, false)
}

// SafeHTTPURLForEgressAllowLab allows loopback/private (local coordinator, LAN lab)
// but still blocks metadata/link-local. Use for worker coord_url and curl fallbacks.
func SafeHTTPURLForEgressAllowLab(raw string) bool {
	return safeHTTPURLForEgress(raw, true)
}

func safeHTTPURLForEgress(raw string, allowPrivate bool) bool {
	if !SafeHTTPBaseURL(raw) {
		return false
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil {
		return false
	}
	return AssertHostSafeForEgress(u.Hostname(), allowPrivate) == nil
}

// AllowPrivateEgress is true when HACKME_ALLOW_PRIVATE_EGRESS=1 (lab / LAN git pins).
func AllowPrivateEgress() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_ALLOW_PRIVATE_EGRESS")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// IPUnsafeForEgress reports whether ip is blocked for outbound SSRF-sensitive dials
// (metadata/link-local always; loopback/private unless allowPrivate).
func IPUnsafeForEgress(ip net.IP, allowPrivate bool) bool {
	if ip == nil {
		return true
	}
	if ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
	}
	if !allowPrivate {
		if ip.IsLoopback() || ip.IsPrivate() {
			return true
		}
	}
	return false
}

// AssertHostSafeForEgress resolves host and rejects metadata/private/loopback classes
// unless allowPrivate is set (or HACKME_ALLOW_PRIVATE_EGRESS).
func AssertHostSafeForEgress(host string, allowPrivate bool) error {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" {
		return fmt.Errorf("empty host")
	}
	if !allowPrivate {
		allowPrivate = AllowPrivateEgress()
	}
	if ip := net.ParseIP(host); ip != nil {
		if IPUnsafeForEgress(ip, allowPrivate) {
			return fmt.Errorf("host is blocked address class")
		}
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("host dns lookup failed")
	}
	if len(ips) == 0 {
		return fmt.Errorf("host dns returned no addresses")
	}
	for _, ip := range ips {
		if IPUnsafeForEgress(ip, allowPrivate) {
			return fmt.Errorf("host resolves to blocked address class")
		}
	}
	return nil
}
