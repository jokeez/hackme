package hms

import (
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
)

var trustClientForwardedFor bool

// InitClientIPTrust configures whether abuse keys honor proxy client IPs.
// Loopback bind auto-trusts unless HMS_TRUST_X_FORWARDED_FOR/HMS_COORDINATOR_TRUST_X_FORWARDED_FOR is set.
func InitClientIPTrust(bindAddr string) {
	if v := strings.TrimSpace(os.Getenv("HMS_TRUST_X_FORWARDED_FOR")); v != "" {
		trustClientForwardedFor = envTruthy("HMS_TRUST_X_FORWARDED_FOR")
		return
	}
	if v := strings.TrimSpace(os.Getenv("HMS_COORDINATOR_TRUST_X_FORWARDED_FOR")); v != "" {
		trustClientForwardedFor = envTruthy("HMS_COORDINATOR_TRUST_X_FORWARDED_FOR")
		return
	}
	if v := strings.TrimSpace(os.Getenv("HACKME_TRUST_X_FORWARDED_FOR")); v != "" {
		trustClientForwardedFor = envTruthy("HACKME_TRUST_X_FORWARDED_FOR")
		return
	}
	trustClientForwardedFor = bindLoopbackOnly(bindAddr)
}

func bindLoopbackOnly(addr string) bool {
	host := strings.TrimSpace(addr)
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = strings.TrimSpace(h)
	} else if strings.HasPrefix(addr, ":") {
		return false
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

func clientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	if trustClientForwardedFor && peerTrustedProxy(r.RemoteAddr) {
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
	}
	return keyFromRemoteAddr(r.RemoteAddr)
}

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
		raw = strings.TrimSpace(os.Getenv("HMS_PROXY_TRUST_CIDRS"))
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

func keyFromRemoteAddr(remoteAddr string) string {
	host := strings.TrimSpace(remoteAddr)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if ip, ok := parseClientIP(host); ok {
		return ip
	}
	return host
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

func envTruthy(key string) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}
