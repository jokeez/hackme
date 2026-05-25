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
	if trustClientForwardedFor {
		if cf := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); cf != "" {
			if ip, ok := parseClientIP(cf); ok {
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
		if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
			if ip, ok := parseClientIP(xri); ok {
				return ip
			}
		}
	}
	return keyFromRemoteAddr(r.RemoteAddr)
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
