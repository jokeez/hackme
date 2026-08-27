package main

import (
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
)

// trustClientForwardedFor is set by initClientIPTrust from bind addr + env.
var trustClientForwardedFor bool

// initClientIPTrust configures whether claim/submit abuse keys use proxy client IPs.
// Production VPS: coordinator on 127.0.0.1:18081 behind nginx → auto-trust unless explicitly disabled.
func initClientIPTrust(bindAddr string) {
	if v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_TRUST_X_FORWARDED_FOR")); v != "" {
		trustClientForwardedFor = envBool("HACKME_COORDINATOR_TRUST_X_FORWARDED_FOR", false)
		return
	}
	if v := strings.TrimSpace(os.Getenv("HACKME_TRUST_X_FORWARDED_FOR")); v != "" {
		trustClientForwardedFor = envBool("HACKME_TRUST_X_FORWARDED_FOR", false)
		return
	}
	trustClientForwardedFor = coordinatorBindLoopbackOnly(bindAddr)
}

func clientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	// Only honor forwarded headers from a trusted proxy peer (loopback or allowlisted CIDR).
	if trustClientForwardedFor && peerTrustedProxy(r.RemoteAddr) {
		// Prefer nginx-set X-Real-IP / X-Forwarded-For. Only honor CF-Connecting-IP when the
		// immediate peer is Cloudflare (otherwise clients can rotate forged CF headers).
		if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
			if ip, ok := parseClientIP(xri); ok {
				return ip
			}
		}
		if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
			parts := strings.Split(xff, ",")
			if len(parts) > 0 {
				if ip, ok := parseClientIP(strings.TrimSpace(parts[0])); ok {
					return ip
				}
			}
		}
		if peerIsCloudflare(r.RemoteAddr) {
			if cf := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); cf != "" {
				if ip, ok := parseClientIP(cf); ok {
					return ip
				}
			}
		}
	}
	return keyFromRemoteAddr(r.RemoteAddr)
}

// peerTrustedProxy reports whether RemoteAddr is loopback or in HACKME_PROXY_TRUST_CIDRS
// (comma-separated, e.g. "10.0.0.0/8,192.168.0.0/16").
func peerTrustedProxy(remoteAddr string) bool {
	host := strings.TrimSpace(remoteAddr)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.IsValid() {
		return false
	}
	if ip.Is4In6() {
		ip = ip.Unmap()
	}
	if ip.IsLoopback() {
		return true
	}
	for _, cidr := range proxyTrustCIDRs() {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

func proxyTrustCIDRs() []netip.Prefix {
	raw := strings.TrimSpace(os.Getenv("HACKME_PROXY_TRUST_CIDRS"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_PROXY_TRUST_CIDRS"))
	}
	if raw == "" {
		return nil
	}
	var out []netip.Prefix
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		p, err := netip.ParsePrefix(part)
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	return out
}

// peerIsCloudflare reports whether RemoteAddr is in published Cloudflare IP ranges.
// Used only as a gate for trusting CF-Connecting-IP (B3).
func peerIsCloudflare(remoteAddr string) bool {
	host := strings.TrimSpace(remoteAddr)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.IsValid() {
		return false
	}
	if ip.Is4In6() {
		ip = ip.Unmap()
	}
	for _, cidr := range cloudflareEdgeCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// Cloudflare published edge ranges (IPv4 + IPv6). Keep in sync with https://www.cloudflare.com/ips/
var cloudflareEdgeCIDRs = mustParseCIDRs(
	"173.245.48.0/20",
	"103.21.244.0/22",
	"103.22.200.0/22",
	"103.31.4.0/22",
	"141.101.64.0/18",
	"108.162.192.0/18",
	"190.93.240.0/20",
	"188.114.96.0/20",
	"197.234.240.0/22",
	"198.41.128.0/17",
	"162.158.0.0/15",
	"104.16.0.0/13",
	"104.24.0.0/14",
	"172.64.0.0/13",
	"131.0.72.0/22",
	"2400:cb00::/32",
	"2606:4700::/32",
	"2803:f800::/32",
	"2405:b500::/32",
	"2405:8100::/32",
	"2a06:98c0::/29",
	"2c0f:f248::/32",
)

func mustParseCIDRs(cidrs ...string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(cidrs))
	for _, c := range cidrs {
		p, err := netip.ParsePrefix(c)
		if err != nil {
			panic("clientip: bad cloudflare cidr: " + c)
		}
		out = append(out, p)
	}
	return out
}

func clientIPKey(r *http.Request) string {
	return clientIP(r)
}

func parseClientIP(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = strings.TrimSpace(host)
	}
	ip, err := netip.ParseAddr(raw)
	if err != nil || !ip.IsValid() {
		return "", false
	}
	if ip.Is4In6() {
		ip = ip.Unmap()
	}
	return ip.String(), true
}
