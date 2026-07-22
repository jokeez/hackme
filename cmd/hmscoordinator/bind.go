package main

import (
	"log"
	"net"
	"os"
	"strings"
)

// stratumBridgeAllowed fails closed for non-loopback Stratum without HMAC, and
// refuses HMS_STRATUM_INSECURE outside loopback unless HMS_STRATUM_ALLOW_PUBLIC=1 (H47).
func stratumBridgeAllowed(stratumAddr, httpAddr string) (ok bool, reason string) {
	if strings.TrimSpace(os.Getenv("HMS_STRATUM_ENABLE")) != "1" {
		return true, ""
	}
	addr := strings.TrimSpace(stratumAddr)
	if addr == "" {
		addr = "127.0.0.1:3334"
	}
	loopback := hmsBindLoopbackOnly(addr)
	insecure := envTruthy("HMS_STRATUM_INSECURE")
	allowPublic := envTruthy("HMS_STRATUM_ALLOW_PUBLIC")
	hmac := stratumHMACSecretFromEnv()
	if loopback {
		return true, ""
	}
	if hmac != "" {
		return true, ""
	}
	if insecure && allowPublic {
		return true, ""
	}
	if insecure && !loopback {
		return false, "HMS_STRATUM_INSECURE=1 on non-loopback also needs HMS_STRATUM_ALLOW_PUBLIC=1 or HMS_STRATUM_HMAC_SECRET"
	}
	_ = httpAddr
	return false, "non-loopback Stratum requires HMS_STRATUM_HMAC_SECRET (or HMS_STRATUM_WORKER_HMAC_SECRET), or INSECURE+ALLOW_PUBLIC"
}

func stratumHMACSecretFromEnv() string {
	for _, k := range []string{
		"HMS_STRATUM_HMAC_SECRET",
		"HACKME_HMS_STRATUM_HMAC_SECRET",
		"HMS_STRATUM_WORKER_HMAC_SECRET",
	} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func mustAllowStratumOrFatal(stratumAddr, httpAddr string) {
	ok, reason := stratumBridgeAllowed(stratumAddr, httpAddr)
	if !ok {
		log.Fatal("security: " + reason)
	}
}

func hmsBindLoopbackOnly(addr string) bool {
	host := strings.TrimSpace(addr)
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = strings.TrimSpace(h)
	} else if strings.HasPrefix(addr, ":") {
		return false
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	// Empty host (e.g. ":3334") means all interfaces — NOT loopback-only.
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}
