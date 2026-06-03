package main

import "testing"

func TestCanonicalPeerStatusURL(t *testing.T) {
	if got := canonicalPeerStatusURL("https://hackme.tech"); got != "https://hackme.tech/api/status?lite=1" {
		t.Fatalf("got %q", got)
	}
	if canonicalPeerStatusURL("") != "" {
		t.Fatal("empty base")
	}
}

func TestCanonicalBaseIsSelfNode_publicAuthority(t *testing.T) {
	t.Setenv("HACKME_PUBLIC_AUTHORITY_BASE", "https://hackme.tech")
	t.Setenv("HACKME_P2P_PEERS", "")
	a := &app{}
	if !a.canonicalBaseIsSelfNode("https://hackme.tech") {
		t.Fatal("expected public authority to match self")
	}
	if a.canonicalBaseIsSelfNode("https://other.example") {
		t.Fatal("unexpected self match")
	}
}

func TestCanonicalBaseIsSelfNode_p2pPeer(t *testing.T) {
	t.Setenv("HACKME_PUBLIC_AUTHORITY_BASE", "")
	t.Setenv("HACKME_P2P_PEERS", "http://10.0.0.5:18080")
	a := &app{}
	if !a.canonicalBaseIsSelfNode("http://10.0.0.5:18080") {
		t.Fatal("expected P2P peer to match self")
	}
}

func TestCanonicalChainBaseURL_prefersPublicAuthorityOverP2P(t *testing.T) {
	t.Setenv("HACKME_CANONICAL_CHAIN_URL", "")
	t.Setenv("HACKME_PUBLIC_AUTHORITY_BASE", "https://hackme.tech")
	t.Setenv("HACKME_P2P_PEERS", "http://10.0.0.5:18080")
	t.Setenv("HACKME_POOL_COORDINATOR_URL", "")
	a := &app{}
	if got := a.canonicalChainBaseURL(); got != "https://hackme.tech" {
		t.Fatalf("got %q want https://hackme.tech", got)
	}
}
