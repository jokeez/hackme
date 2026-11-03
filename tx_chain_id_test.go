package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Desktops/followers sign relayed transfers with the hub's chain_id, not their own env view.
func TestFetchCanonicalTxChainID(t *testing.T) {
	serve := func(body map[string]any) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/status" {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(body)
		}))
	}
	v2 := serve(map[string]any{"chain_id": "hackme-mainnet-1", "tx_chain_id": "hackme-mainnet-1"})
	defer v2.Close()
	if id, ok := fetchCanonicalTxChainID(context.Background(), v2.URL); !ok || id != "hackme-mainnet-1" {
		t.Fatalf("v2 hub: %q %v", id, ok)
	}
	pre := serve(map[string]any{"chain_id": "hackme-dev-mainnet", "tx_chain_id": ""})
	defer pre.Close()
	if id, ok := fetchCanonicalTxChainID(context.Background(), pre.URL); !ok || id != "" {
		t.Fatalf("pre-v2 hub: %q %v", id, ok)
	}
	old := serve(map[string]any{"chain_id": "hackme-dev-mainnet"}) // build without the field
	defer old.Close()
	if id, ok := fetchCanonicalTxChainID(context.Background(), old.URL); !ok || id != "" {
		t.Fatalf("old hub: %q %v", id, ok)
	}
	if _, ok := fetchCanonicalTxChainID(context.Background(), "http://127.0.0.1:9"); ok {
		t.Fatal("unreachable hub must report !ok")
	}
}
