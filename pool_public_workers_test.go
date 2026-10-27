package main

import "testing"

func TestRedactPublicWorkStats(t *testing.T) {
	ws := map[string]any{
		"desktop_worker_id":             "worker-email-host",
		"desktop_worker_injected":       true,
		"desktop_worker_on_coordinator": false,
		"workers": map[string]any{
			"worker-kapa-pc": map[string]any{
				"payout_hmc":     1.5,
				"payout_address": "HMC-abc",
				"last_client_ip": "1.2.3.4",
				"online":         true,
			},
		},
	}
	redactPublicWorkStats(ws)
	if _, ok := ws["desktop_worker_id"]; ok {
		t.Fatal("desktop_worker_id must be stripped")
	}
	workers := ws["workers"].(map[string]any)
	if len(workers) != 1 {
		t.Fatalf("workers=%d", len(workers))
	}
	for k, row := range workers {
		if k == "worker-kapa-pc" || len(k) != 16 {
			t.Fatalf("want 16-hex pseudonym key, got %q", k)
		}
		m := row.(map[string]any)
		if _, ok := m["last_client_ip"]; ok {
			t.Fatal("ip leaked")
		}
		if m["payout_address"] != "HMC-abc" {
			t.Fatalf("row=%v", m)
		}
	}
}

func TestPublicWorkerPseudoKeyStable(t *testing.T) {
	a := publicWorkerPseudoKey("worker-kapa-pc")
	b := publicWorkerPseudoKey("worker-kapa-pc")
	c := publicWorkerPseudoKey("other")
	if a == "" || a != b || a == c {
		t.Fatalf("a=%q b=%q c=%q", a, b, c)
	}
}
