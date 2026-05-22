package gputune

import "testing"

func TestForGPUName_RTX30(t *testing.T) {
	h := ForGPUName("NVIDIA GeForce RTX 3080")
	if h.Family != "Ampere" {
		t.Fatalf("family: %q", h.Family)
	}
	if h.TypicalTDPW != 350 {
		t.Fatalf("tdp: %d", h.TypicalTDPW)
	}
	if len(h.Tips) < 2 {
		t.Fatal("expected tips")
	}
}

func TestForGPUName_Empty(t *testing.T) {
	h := ForGPUName("")
	if h.Family != "Unknown" {
		t.Fatalf("family: %q", h.Family)
	}
}

func TestForGPUName_ModelMatrix(t *testing.T) {
	cases := []struct {
		name   string
		family string
		vendor string
	}{
		{"NVIDIA H100 80GB", "Hopper", "NVIDIA"},
		{"NVIDIA B200", "Hopper", "NVIDIA"},
		{"NVIDIA GeForce RTX 2060", "Turing", "NVIDIA"},
		{"NVIDIA GeForce GTX 1060", "Pascal", "NVIDIA"},
		{"NVIDIA GeForce RTX 5060 Ti", "Blackwell (hint)", "NVIDIA"},
		{"NVIDIA GeForce RTX 5070", "Blackwell (hint)", "NVIDIA"},
		{"NVIDIA GeForce RTX 5080", "Blackwell (hint)", "NVIDIA"},
		{"NVIDIA GeForce RTX 4070 SUPER", "Ada Lovelace", "NVIDIA"},
		{"NVIDIA GeForce RTX 4090", "Ada Lovelace", "NVIDIA"},
		{"NVIDIA GeForce RTX 4060 Ti", "Ada Lovelace", "NVIDIA"},
		{"NVIDIA A5000", "Ampere", "NVIDIA"},
		{"NVIDIA GeForce RTX 3080", "Ampere", "NVIDIA"},
		{"NVIDIA GeForce RTX 3060", "Ampere", "NVIDIA"},
		{"NVIDIA GeForce GTX 1660 SUPER", "Turing", "NVIDIA"},
		{"NVIDIA GeForce RTX 2080", "Turing", "NVIDIA"},
		{"NVIDIA GeForce GTX 1080 Ti", "Pascal", "NVIDIA"},
		{"NVIDIA Tesla P40", "Pascal", "NVIDIA"},
		{"AMD Radeon RX 7900 XTX", "AMD RDNA 3", "AMD"},
		{"AMD Radeon RX 7800 XT", "AMD RDNA 3", "AMD"},
		{"AMD Radeon RX 6800 XT", "AMD RDNA 2", "AMD"},
		{"AMD Radeon RX 6700 XT", "AMD RDNA 2", "AMD"},
		{"AMD Radeon RX 5700 XT", "AMD RDNA 1", "AMD"},
		{"AMD Radeon RX 580", "AMD Polaris/Vega", "AMD"},
		{"AMD Radeon Vega 64", "AMD Polaris/Vega", "AMD"},
		{"Intel Arc A770", "Intel Arc", "Intel"},
		{"Intel Arc A380", "Intel Arc", "Intel"},
	}
	for _, tc := range cases {
		h := ForGPUName(tc.name)
		if h.Family != tc.family {
			t.Fatalf("%s family: got %q want %q", tc.name, h.Family, tc.family)
		}
		if h.Vendor != tc.vendor {
			t.Fatalf("%s vendor: got %q want %q", tc.name, h.Vendor, tc.vendor)
		}
		if h.RecommendedPL <= 0 {
			t.Fatalf("%s expected recommended PL", tc.name)
		}
		if h.PLRangeMin <= 0 || h.PLRangeMax <= 0 || h.PLRangeMin > h.PLRangeMax {
			t.Fatalf("%s invalid PL range min=%d max=%d", tc.name, h.PLRangeMin, h.PLRangeMax)
		}
	}
}

func TestForGPUName_UnknownModelFallsBack(t *testing.T) {
	h := ForGPUName("SuperUnknown Vendor Accelerator 123")
	if h.Family != "Unknown" {
		t.Fatalf("family: got %q want Unknown", h.Family)
	}
	if h.Vendor != "Generic" {
		t.Fatalf("vendor: got %q want Generic", h.Vendor)
	}
	if h.RecommendedPL <= 0 {
		t.Fatalf("expected default recommended PL > 0")
	}
}
