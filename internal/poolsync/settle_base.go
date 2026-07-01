package poolsync

import (
	"net"
	"os"
	"strings"
)

// ResolveOrdersSettleBase returns the node URL coordinators should relay fuzz escrow
// settlements to, and whether pull-mode is required (loopback / unreachable from VPS).
func ResolveOrdersSettleBase() (base string, pull bool) {
	if v := strings.TrimRight(strings.TrimSpace(os.Getenv("HACKME_ORDERS_SETTLE_BASE")), "/"); v != "" {
		return v, isLoopbackSettleBase(v)
	}
	if v := strings.TrimRight(strings.TrimSpace(os.Getenv("BASE_URL")), "/"); v != "" {
		if !isLoopbackSettleBase(v) {
			return v, false
		}
	}
	bind := strings.TrimSpace(os.Getenv("HACKME_BIND_ADDR"))
	if bind == "" {
		bind = "127.0.0.1:8080"
	}
	host, _, err := net.SplitHostPort(bind)
	if err != nil {
		host = bind
	}
	if isLoopbackHost(host) {
		return "http://" + bind, true
	}
	if !strings.Contains(bind, "://") {
		return "http://" + bind, false
	}
	return strings.TrimRight(bind, "/"), false
}

func isLoopbackSettleBase(u string) bool {
	low := strings.ToLower(strings.TrimSpace(u))
	if strings.HasPrefix(low, "http://127.0.0.1") || strings.HasPrefix(low, "http://localhost") {
		return true
	}
	if strings.HasPrefix(low, "https://127.0.0.1") || strings.HasPrefix(low, "https://localhost") {
		return true
	}
	return false
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}
