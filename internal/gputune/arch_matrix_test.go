package gputune

import (
	"testing"

	"hackme/internal/gpupoh"
	"hackme/internal/sandbox"
)

func TestGreenCamp_AllProfilesValid(t *testing.T) {
	for _, a := range GreenCampCatalog() {
		if a.Camp != CampGreen {
			t.Fatalf("%s camp", a.ID)
		}
		if a.Backend != "cuda" {
			t.Fatalf("%s backend", a.ID)
		}
		if err := ValidateSimBatch(a, a.MaxWorkerBatch, a.MaxGPUChunk); err != nil {
			t.Fatalf("%s valid max batch: %v", a.ID, err)
		}
		if err := ValidateSimBatch(a, a.MaxWorkerBatch+1, a.MaxGPUChunk); err == nil {
			t.Fatalf("%s expected batch overflow error", a.ID)
		}
		h := ForGPUName(a.MarketingName)
		if h.Vendor != "NVIDIA" {
			t.Fatalf("%s hints vendor %q", a.ID, h.Vendor)
		}
	}
}

func TestRedCamp_PolarisStableTarget(t *testing.T) {
	for _, a := range RedCampCatalog() {
		if a.Camp != CampRed {
			t.Fatalf("%s camp", a.ID)
		}
		if a.Backend != "opencl" {
			t.Fatalf("%s backend", a.ID)
		}
		if a.LocalWorkSize < 64 {
			t.Fatalf("%s local work size", a.ID)
		}
	}
	rx580, ok := LookupSimArch("amd_rx580")
	if !ok || rx580.TargetStableGHS < 2.5 || rx580.TargetStableGHS > 3.5 {
		t.Fatalf("rx580 stable ghs: %+v", rx580)
	}
	if err := SimOpenCLCompile(rx580, "rocm"); err != nil {
		t.Fatal(err)
	}
	if err := SimOpenCLCompile(rx580, "adrenalin"); err == nil {
		// adrenalin name on linux may still work via rocm — allowed in SimOpenCLCompile
	}
}

func TestBlueCamp_IntelArc(t *testing.T) {
	a, ok := LookupSimArch("intel_arc_a770")
	if !ok {
		t.Fatal("missing arc")
	}
	if err := SimOpenCLCompile(a, "neo"); err != nil {
		t.Fatal(err)
	}
	p, ok := DetectRigProfile([]string{a.MarketingName})
	if !ok || p.ID != "intel_arc_daily" {
		t.Fatalf("rig profile: ok=%v id=%q", ok, p.ID)
	}
}

func TestAmpere_WASMSandboxHeadroom(t *testing.T) {
	a, _ := LookupSimArch("nv_rtx3080")
	want := RecommendSandboxWasmBytes(a)
	if want < 131072 {
		t.Fatalf("ampere wasm hint %d", want)
	}
	pol := sandbox.Policy()
	if pol.MaxCheckWasmBytes < 1024 {
		t.Fatalf("sandbox policy broken: %+v", pol)
	}
	if want > pol.MaxCheckWasmBytes {
		t.Fatalf("ampere hint %d exceeds active sandbox max %d (profile=%s)", want, pol.MaxCheckWasmBytes, pol.Profile)
	}
}

func TestPascal_LegacyCUDAFallbackChain(t *testing.T) {
	a, _ := LookupSimArch("nv_gtx1060")
	if !a.LegacyCUDAFallback {
		t.Fatal("expected legacy cuda flag")
	}
	chain := gpupoh.NVRTCArchChainForTest(6, 1)
	if len(chain) == 0 || chain[0] != "compute_61" {
		t.Fatalf("pascal arch chain: %v", chain)
	}
	for _, fb := range chain {
		if fb == "compute_60" {
			return
		}
	}
	t.Fatalf("expected compute_60 in fallback chain: %v", chain)
}

func TestAda_FatBatchAllowed(t *testing.T) {
	a, _ := LookupSimArch("nv_rtx4090")
	if a.MaxWorkerBatch < 4<<20 {
		t.Fatalf("4090 batch cap")
	}
	if err := ValidateSimBatch(a, 4<<20, 4<<20); err != nil {
		t.Fatal(err)
	}
	p, ok := DetectRigProfile([]string{"NVIDIA GeForce RTX 4090"})
	if !ok || p.ID != "nvidia_rtx_40_daily" {
		t.Fatalf("rig: ok=%v id=%q", ok, p.ID)
	}
}

func TestHopper_HMAIProfile(t *testing.T) {
	a, _ := LookupSimArch("nv_h100")
	if RecommendSandboxWasmBytes(a) < 512*1024 {
		t.Fatalf("hopper wasm headroom")
	}
	p, ok := DetectRigProfile([]string{"NVIDIA H100 80GB PCIe"})
	if !ok || p.ID != "nvidia_hopper_hmai" {
		t.Fatalf("rig: ok=%v id=%q", ok, p.ID)
	}
	h := ForGPUName("NVIDIA H100 80GB")
	if h.Family != "Hopper" {
		t.Fatalf("hints family %q", h.Family)
	}
}

func TestGlobalMatrix_Count(t *testing.T) {
	all := AllSimArch()
	if len(all) < 15 {
		t.Fatalf("expected >=15 sim arch, got %d", len(all))
	}
	seen := map[string]bool{}
	for _, a := range all {
		if seen[a.ID] {
			t.Fatalf("duplicate id %s", a.ID)
		}
		seen[a.ID] = true
	}
}

func TestRigProfiles_AllSimArchResolvable(t *testing.T) {
	for _, a := range AllSimArch() {
		p, ok := DetectRigProfile([]string{a.MarketingName})
		if !ok {
			t.Fatalf("no rig profile for %s (%s)", a.ID, a.MarketingName)
		}
		if p.Env["HACKME_GPU_BACKEND"] != a.Backend {
			t.Fatalf("%s backend mismatch rig=%s sim=%s", a.ID, p.Env["HACKME_GPU_BACKEND"], a.Backend)
		}
	}
}
