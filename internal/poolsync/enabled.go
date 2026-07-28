package poolsync

import (
	"os"
	"strings"
)

func envTruthy(key string) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// FuzzSettlePullEnabled is true when outbox pull is not explicitly disabled and an admin token is present.
func FuzzSettlePullEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_FUZZ_SETTLE_PULL")))
	if v == "0" || v == "false" || v == "no" || v == "off" {
		return false
	}
	return AdminToken() != ""
}

func preferPoolSyncDirect() bool {
	for _, k := range []string{"HACKME_POOL_SYNC_PREFER_DIRECT", "HACKME_POOL_DIRECT", "HACKME_DESKTOP_GPU_POOL"} {
		if envTruthy(k) {
			return true
		}
	}
	return false
}

func coordLooksPublic(u string) bool {
	low := strings.ToLower(strings.TrimSpace(u))
	return strings.Contains(low, "hackme.tech") || strings.Contains(low, "/pool/coordinator")
}
