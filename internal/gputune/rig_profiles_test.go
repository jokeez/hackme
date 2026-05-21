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
