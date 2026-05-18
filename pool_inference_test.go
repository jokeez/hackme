package main

import (
	"os"
	"testing"
)

func TestInferCanonicalChainBaseFromCoordinatorURL(t *testing.T) {
	t.Run("18081_to_18080", func(t *testing.T) {
		t.Setenv("HACKME_POOL_COORDINATOR_URL", "http://leader.example:18081")
		got := inferCanonicalChainBaseFromCoordinatorURL()
		want := "http://leader.example:18080"
		if got != want {
			t.Fatalf("got %q want %q", got, want)
		}
	})
	t.Run("8081_to_8080", func(t *testing.T) {
		t.Setenv("HACKME_POOL_COORDINATOR_URL", "http://127.0.0.1:8081")
		got := inferCanonicalChainBaseFromCoordinatorURL()
		want := "http://127.0.0.1:8080"
		if got != want {
			t.Fatalf("got %q want %q", got, want)
		}
	})
	t.Run("no_port_skipped", func(t *testing.T) {
		t.Setenv("HACKME_POOL_COORDINATOR_URL", "http://leader.example")
		if inferCanonicalChainBaseFromCoordinatorURL() != "" {
			t.Fatal("expected empty when coordinator URL has no explicit port")
		}
	})
}

func TestInferCoordinatorURLFromCommandBase(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"http://1.2.3.4:18080", "http://1.2.3.4:18081"},
		{"http://127.0.0.1:8080", "http://127.0.0.1:8081"},
		{"https://pool.example.com", "https://pool.example.com/pool/coordinator"},
		{"https://pool.example.com/", "https://pool.example.com/pool/coordinator"},
	}
	for _, tc := range tests {
		if got := inferCoordinatorURLFromCommandBase(tc.in); got != tc.want {
			t.Fatalf("inferCoordinatorURLFromCommandBase(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestApplyPublicAuthorityBaseEnv(t *testing.T) {
	t.Setenv("HACKME_PUBLIC_AUTHORITY_BASE", "http://10.0.0.5:18080")
	t.Setenv("HACKME_CANONICAL_CHAIN_URL", "")
	t.Setenv("HACKME_POOL_COORDINATOR_URL", "")
	t.Setenv("HACKME_PUBLIC_COORDINATOR_URL", "")
	applyPublicAuthorityBaseEnv()
	if got := os.Getenv("HACKME_CANONICAL_CHAIN_URL"); got != "http://10.0.0.5:18080" {
		t.Fatalf("canonical: %q", got)
	}
	if got := os.Getenv("HACKME_POOL_COORDINATOR_URL"); got != "http://10.0.0.5:18081" {
		t.Fatalf("coordinator: %q", got)
	}
}
