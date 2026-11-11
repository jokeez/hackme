package poolsync

import (
	"os"
	"strings"

	"hackme/internal/operator"
)

func envTruthy(key string) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// RequireHubPrepaidTicket is true on D1 minting hubs (or when explicitly forced).
// Publish must present a hub-signed ticket — forgeable publish-token self-MAC is refused.
// Also true whenever the prepaid hub secret is configured: a set signing key must not
// coexist with accepting publish-token self-MAC (PUB-04).
func RequireHubPrepaidTicket() bool {
	if envTruthy("HACKME_ORDERS_REQUIRE_HUB_PREPAID_TICKET") {
		return true
	}
	if envTruthy("HACKME_CHAIN_MINTING_HUB") {
		return true
	}
	return OrdersPrepaidHubSecret() != ""
}

// PrepaidLocalSignAllowed is false when operators keep the signing key hub-only
// (HACKME_ORDERS_PREPAID_LOCAL_SIGN=0). Orders then must present an admin-minted
// hubv1 ticket; the fleet publish token alone cannot mint.
func PrepaidLocalSignAllowed() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_ORDERS_PREPAID_LOCAL_SIGN")))
	if v == "0" || v == "false" || v == "no" || v == "off" {
		return false
	}
	return true
}

// OrdersPrepaidHubSecret is the hub HMAC secret for prepaid tickets (≠ publish token).
// Env first, then distinct on-disk file (pool.orders.prepaid_hub.secret) — never the
// Orders publish fleet credential file.
func OrdersPrepaidHubSecret() string {
	for _, k := range []string{
		"HACKME_ORDERS_PREPAID_HUB_SECRET",
		"HACKME_COORDINATOR_ORDERS_PREPAID_HUB_SECRET",
		"HACKME_POOL_ORDERS_PREPAID_HUB_SECRET",
	} {
		if t := strings.TrimSpace(os.Getenv(k)); t != "" {
			return t
		}
	}
	return operator.ReadOrdersPrepaidHubSecret()
}

// FuzzSettlePullEnabled is true when outbox pull is not explicitly disabled and an
// Orders customer credential is present (scoped publish or admin — not worker token).
func FuzzSettlePullEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_FUZZ_SETTLE_PULL")))
	if v == "0" || v == "false" || v == "no" || v == "off" {
		return false
	}
	return OrdersCustomerToken() != ""
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
