package fuzzupstream

import (
	"strings"
	"testing"
)

func TestScrubBuildEnvDropsHubTokens(t *testing.T) {
	in := []string{
		"PATH=/usr/bin",
		"HACKME_ADMIN_TOKEN=admin",
		"HACKME_SETTLEMENT_ADMIN_TOKEN=settle",
		"HACKME_COORDINATOR_ORDERS_PUBLISH_TOKEN=pub",
		"HACKME_POOL_ORDERS_PUBLISH_TOKEN=pub2",
		"HACKME_ORDERS_PUBLISH_TOKEN=pub3",
		"HACKME_P2P_TOKEN=p2p-secret",
		"HACKME_CANONICAL_RELAY_ADMIN_TOKEN=relay-admin",
		"HACKME_ORDERS_PREPAID_HUB_SECRET=hub-prepaid",
		"COORD_TOKEN=x",
		"DATABASE_URL=postgres://secret",
		"AFL_PATH=/opt/afl",
		"RUSTFLAGS=-Zsanitizer=address",
		"HACKME_POOL_COORDINATOR_URL=https://pool.example",
	}
	out := ScrubBuildEnv(in)
	joined := strings.Join(out, "\n")
	for _, bad := range []string{
		"HACKME_ADMIN_TOKEN=", "HACKME_SETTLEMENT_ADMIN_TOKEN=", "COORD_TOKEN=",
		"HACKME_COORDINATOR_ORDERS_PUBLISH_TOKEN=", "HACKME_POOL_ORDERS_PUBLISH_TOKEN=",
		"HACKME_ORDERS_PUBLISH_TOKEN=", "HACKME_P2P_TOKEN=", "HACKME_CANONICAL_RELAY_ADMIN_TOKEN=",
		"HACKME_ORDERS_PREPAID_HUB_SECRET=", "DATABASE_URL=",
	} {
		if strings.Contains(joined, bad) {
			t.Fatalf("secret leaked: %s\n%s", bad, joined)
		}
	}
	keep := map[string]bool{}
	for _, e := range out {
		k, _, _ := strings.Cut(e, "=")
		keep[k] = true
	}
	if !keep["PATH"] || !keep["AFL_PATH"] || !keep["RUSTFLAGS"] || !keep["HACKME_POOL_COORDINATOR_URL"] {
		t.Fatalf("expected compiler knobs kept: %#v", out)
	}
}
