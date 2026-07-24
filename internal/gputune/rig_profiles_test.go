package gputune

import "testing"

func TestDetectRigProfileRX5802048SP(t *testing.T) {
	p, ok := DetectRigProfile([]string{"AMD Radeon RX 580 2048SP"})
	if !ok {
		t.Fatal("expected match")
	}
	if p.ID != "amd_rx580_2048sp" {
		t.Fatalf("got %q", p.ID)
	}
	if p.Env["HACKME_WORKER_BATCH_SIZE"] != "1048576" {
		t.Fatalf("batch: %v", p.Env["HACKME_WORKER_BATCH_SIZE"])
	}
}

func TestDetectRigProfileGeneric580(t *testing.T) {
	p, ok := DetectRigProfile([]string{"Radeon RX 580 Series"})
	if !ok || p.ID != "amd_rx580_generic" {
		t.Fatalf("got ok=%v id=%q", ok, p.ID)
	}
}

func TestDetectRigProfileIntelArc(t *testing.T) {
	p, ok := DetectRigProfile([]string{"Intel Arc A770 Graphics"})
	if !ok || p.ID != "intel_arc_daily" {
		t.Fatalf("got ok=%v id=%q", ok, p.ID)
	}
}

func TestDetectRigProfileRTX5060(t *testing.T) {
	p, ok := DetectRigProfile([]string{"NVIDIA GeForce RTX 5060 Ti"})
	if !ok || p.ID != "nvidia_rtx_50_daily" {
		t.Fatalf("got ok=%v id=%q", ok, p.ID)
	}
}

func TestGetRigProfileMissing(t *testing.T) {
	if _, ok := GetRigProfile("nope"); ok {
		t.Fatal("expected missing")
	}
}

func TestGPURigProfilesBatchAndCooldown(t *testing.T) {
	// Fast GPU profiles: 16M batch + claim cooldown ≥80 (fleet default 100).
	fast16M := map[string]bool{
		"amd_rdna2_daily":     true,
		"amd_rdna3_daily":     true,
		"nvidia_rtx_30_daily": true,
		"nvidia_rtx_40_daily": true,
		"nvidia_rtx_50_daily": true,
		"nvidia_hopper_hmai":  true,
	}
	for _, p := range ListRigProfiles() {
		cd := p.Env["HACKME_WORKER_CLAIM_COOLDOWN_MS"]
		if cd == "" || cd == "0" {
			t.Fatalf("%s: GPU profile must set claim cooldown ≥80 (got %q)", p.ID, cd)
		}
		var n int
		for _, c := range cd {
			if c < '0' || c > '9' {
				t.Fatalf("%s: bad cooldown %q", p.ID, cd)
			}
			n = n*10 + int(c-'0')
		}
		if n < 80 {
			t.Fatalf("%s: cooldown %d < 80", p.ID, n)
		}
		if fast16M[p.ID] && p.Env["HACKME_WORKER_BATCH_SIZE"] != "16777216" {
			t.Fatalf("%s: want batch 16777216, got %q", p.ID, p.Env["HACKME_WORKER_BATCH_SIZE"])
		}
	}
}

func TestDetectRigProfileRTX3080Batch(t *testing.T) {
	p, ok := DetectRigProfile([]string{"NVIDIA GeForce RTX 3080"})
	if !ok || p.ID != "nvidia_rtx_30_daily" {
		t.Fatalf("got ok=%v id=%q", ok, p.ID)
	}
	if p.Env["HACKME_WORKER_BATCH_SIZE"] != "16777216" {
		t.Fatalf("batch: %v", p.Env["HACKME_WORKER_BATCH_SIZE"])
	}
	if p.Env["HACKME_WORKER_CLAIM_COOLDOWN_MS"] != "100" {
		t.Fatalf("cooldown: %v", p.Env["HACKME_WORKER_CLAIM_COOLDOWN_MS"])
	}
}
