package main

import "testing"

func TestRedactPublicWorkStats(t *testing.T) {
	ws := map[string]any{
		"desktop_worker_id":             "worker-email-host",
		"desktop_worker_injected":       true,
		"desktop_worker_on_coordinator": false,
		"source":                        "http://127.0.0.1:18083",
		"target_mod":                    uint64(93615156251),
		"share_divisor":                 uint64(1024),
		"default_batch":                 uint64(4194304),
		"next_nonce":                    uint64(99),
		"tip_height":                    uint64(515000),
		"hashrate_hs":                   1.5e9,
		"pool_budget": map[string]any{
			"enabled":             true,
			"daily_budget_hmc":    28.8,
			"accrued_today_hmc":   10.0,
			"remaining_today_hmc": 18.8,
			"budget_clip_events":  99,
			"persisted":           true,
			"note":                "cap",
		},
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
	for _, k := range []string{"source", "target_mod", "share_divisor", "default_batch", "next_nonce"} {
		if _, ok := ws[k]; ok {
			t.Fatalf("%s must be stripped for public callers", k)
		}
	}
	if ws["tip_height"] != uint64(515000) {
		t.Fatalf("tip_height should remain for transparency, got %v", ws["tip_height"])
	}
	pd, ok := ws["pool_display"].(map[string]any)
	if !ok || pd == nil {
		t.Fatal("pool_display required after public redact")
	}
	if pd["mode"] != "hashrate" {
		t.Fatalf("pool_display.mode=%v", pd["mode"])
	}
	gh := parseAnyFloat(pd["hashrate_gh_s"])
	if !(gh > 1.4 && gh < 1.6) {
		t.Fatalf("pool_display.hashrate_gh_s from hashrate_hs want ~1.5, got %v", pd["hashrate_gh_s"])
	}
	if got := parseAnyFloat(ws["pool_hashrate_gh_s"]); !(got > 1.4 && got < 1.6) {
		t.Fatalf("pool_hashrate_gh_s should stay public, got %v", ws["pool_hashrate_gh_s"])
	}
	pb := ws["pool_budget"].(map[string]any)
	if _, ok := pb["budget_clip_events"]; ok {
		t.Fatalf("budget_clip_events must not be public: %v", pb)
	}
	if pb["daily_budget_hmc"] != 28.8 {
		t.Fatalf("daily_budget_hmc should remain, got %v", pb)
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

func TestPublicCoordinatorSourceLabel(t *testing.T) {
	if got := publicCoordinatorSourceLabel("http://127.0.0.1:18083"); got != "pool" {
		t.Fatalf("loopback -> pool, got %q", got)
	}
	if got := publicCoordinatorSourceLabel("https://hackme.tech/pool/coordinator"); got != "pool" {
		t.Fatalf("public also opaque label, got %q", got)
	}
}

func TestRedactInternalCoordinatorURLs(t *testing.T) {
	m := map[string]any{
		"pool_coordinator_url":           "http://127.0.0.1:18083",
		"pool_coordinator_url_effective": "http://127.0.0.1:18083",
		"miner_public_ux": map[string]any{
			"coordinator_url_effective": "http://127.0.0.1:18083",
		},
	}
	redactInternalCoordinatorURLs(m)
	if m["pool_coordinator_url"] != "pool" {
		t.Fatalf("url=%v", m["pool_coordinator_url"])
	}
	ux := m["miner_public_ux"].(map[string]any)
	if ux["coordinator_url_effective"] != defaultPublicStagingCoordinatorBase {
		t.Fatalf("ux=%v", ux)
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

func TestApplyWorkStatsPoolFieldsHashrateLane(t *testing.T) {
	ws := map[string]any{
		"workers_count":      uint64(53), // stale map size
		"workers_online":     uint64(32), // live
		"pool_hashrate_gh_s": 12.5,
		"hashrate_hs":        0.0,
		"target_mod":         uint64(9e9),
		"base_reward_hmc":    0.01,
		"found_bonus_hmc":    0.002,
		"share_strict":       true,
		"pool_hashrate_kind": "blake3_pow_ghs",
	}
	redactPublicWorkStats(ws)
	status := map[string]any{}
	applyWorkStatsPoolFields(status, ws)
	if status["pool_lane_mode"] != "hashrate" {
		t.Fatalf("lane=%v", status["pool_lane_mode"])
	}
	if status["pool_workers_count"] != uint64(32) {
		t.Fatalf("workers live=%v want 32 (not tracked 53)", status["pool_workers_count"])
	}
	pd := ws["pool_display"].(map[string]any)
	if pd["workers_online"] != uint64(32) || pd["workers_tracked"] != uint64(53) {
		t.Fatalf("pool_display=%v", pd)
	}
	gh := parseAnyFloat(status["pool_hashrate_gh_s"])
	if !(gh > 12.0 && gh < 13.0) {
		t.Fatalf("status gh=%v", status["pool_hashrate_gh_s"])
	}
	if _, ok := status["pool_target_mod"]; ok {
		t.Fatal("public status must not carry pool_target_mod from redacted work stats")
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
