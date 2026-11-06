package main

import "testing"

func TestRedactInternalTopologyNested(t *testing.T) {
	m := map[string]any{
		"pool_sync":                  map[string]any{"coordinator_url_resolved": "http://127.0.0.1:18081"},
		"network_sync":               map[string]any{"bind_addr": "127.0.0.1:18080"},
		"pool_coordinator_url":       "http://10.0.0.8:18081",
		"canonical_chain_base_url":   "http://192.168.1.10:18090",
		"public_authority_base":      "http://169.254.169.254/",
	}
	redactInternalCoordinatorURLs(m)
	if got := m["pool_sync"].(map[string]any)["coordinator_url_resolved"]; got != "pool" {
		t.Fatalf("coordinator_url_resolved = %v", got)
	}
	if got := m["network_sync"].(map[string]any)["bind_addr"]; got != "internal" {
		t.Fatalf("bind_addr = %v", got)
	}
	if got := m["pool_coordinator_url"]; got != "pool" {
		t.Fatalf("private coordinator must redact, got %v", got)
	}
	if got := m["canonical_chain_base_url"]; got != "pool" {
		t.Fatalf("private chain base must redact, got %v", got)
	}
	if got := m["public_authority_base"]; got != "pool" {
		t.Fatalf("link-local authority must redact, got %v", got)
	}
	pub := map[string]any{"pool_sync": map[string]any{"coordinator_url_resolved": "https://pool.hackme.tech"}}
	redactInternalCoordinatorURLs(pub)
	if got := pub["pool_sync"].(map[string]any)["coordinator_url_resolved"]; got != "https://pool.hackme.tech" {
		t.Fatalf("public URL must stay, got %v", got)
	}
}
