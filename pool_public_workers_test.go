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

func TestRedactPublicWorkStatsSkipsAlreadyPseudonymized(t *testing.T) {
	once := publicWorkerPseudoKey("worker-kapa-pc")
	twice := publicWorkerPseudoKey(once)
	ws := map[string]any{
		"workers_keys": "pseudonym_v1",
		"desktop_worker_id": "worker-kapa-pc",
		"workers": map[string]any{
			once: map[string]any{
				"payout_hmc":     441.5,
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
	if _, ok := workers[once]; !ok {
		t.Fatalf("expected key %s preserved, got %v", once, workers)
	}
	if _, ok := workers[twice]; ok {
		t.Fatal("must not double-hash already-pseudonym keys")
	}
	row := workers[once].(map[string]any)
	if _, ok := row["last_client_ip"]; ok {
		t.Fatal("ip must still be stripped")
	}
	if ws["workers_keys"] != "pseudonym_v1" {
		t.Fatalf("workers_keys=%v", ws["workers_keys"])
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

func TestRedactPublicWorkStatsActiveRigs(t *testing.T) {
	ws := map[string]any{
		"active_rigs": []any{
			map[string]any{"worker_id": "worker-bob-vps-ru", "name": "worker-bob-vps-ru", "hashrate_gh_s": 2.0, "last_client_ip": "198.51.100.4"},
		},
	}
	redactPublicWorkStats(ws)
	rows := ws["active_rigs"].([]any)
	row := rows[0].(map[string]any)
	want := publicWorkerPseudoKey("worker-bob-vps-ru")
	if row["worker_id"] != want || row["name"] != want {
		t.Fatalf("active_rigs must be pseudonymized, got %v", row)
	}
	if _, ok := row["last_client_ip"]; ok {
		t.Fatalf("ip must be stripped: %v", row)
	}
}
