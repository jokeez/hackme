package main

import "testing"

func TestRedactInternalTopologyNested(t *testing.T) {
	m := map[string]any{
		"pool_sync":    map[string]any{"coordinator_url_resolved": "http://127.0.0.1:18081"},
		"network_sync": map[string]any{"bind_addr": "127.0.0.1:18080"},
	}
	redactInternalCoordinatorURLs(m)
	if got := m["pool_sync"].(map[string]any)["coordinator_url_resolved"]; got != "pool" {
		t.Fatalf("coordinator_url_resolved = %v", got)
	}
	if got := m["network_sync"].(map[string]any)["bind_addr"]; got != "loopback" {
		t.Fatalf("bind_addr = %v", got)
	}
	pub := map[string]any{"pool_sync": map[string]any{"coordinator_url_resolved": "https://pool.hackme.tech"}}
	redactInternalCoordinatorURLs(pub)
	if got := pub["pool_sync"].(map[string]any)["coordinator_url_resolved"]; got != "https://pool.hackme.tech" {
		t.Fatalf("public URL must stay, got %v", got)
	}
}
