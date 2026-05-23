package gpuhost

import (
	"testing"

	"hackme/internal/gputune"
)

func TestSlotForGPURTX5060Profile(t *testing.T) {
	s := slotForGPU("worker-test", "-cuda0", "cuda", 0, "NVIDIA GeForce RTX 5060 Ti")
	if s.Env["HACKME_GPU_BACKEND"] != "cuda" {
		t.Fatalf("backend env: %q", s.Env["HACKME_GPU_BACKEND"])
	}
	if s.RigProfileID != "nvidia_rtx_50_daily" {
		t.Fatalf("profile: %q", s.RigProfileID)
	}
	if s.Env["HACKME_WORKER_BATCH_SIZE"] != "4194304" {
		t.Fatalf("batch: %q", s.Env["HACKME_WORKER_BATCH_SIZE"])
	}
}

func TestSlotForGPURX580Profile(t *testing.T) {
	s := slotForGPU("worker-test", "-ocl0", "opencl", 0, "AMD Radeon RX 580 Series")
	if s.Env["HACKME_FORCE_OPENCL"] != "1" {
		t.Fatalf("force opencl: %q", s.Env["HACKME_FORCE_OPENCL"])
	}
	p, ok := gputune.GetRigProfile(s.RigProfileID)
	if !ok {
		t.Fatal("missing profile")
	}
	if p.Env["HACKME_GPU_BACKEND"] != "opencl" {
		t.Fatalf("profile backend: %s", p.Env["HACKME_GPU_BACKEND"])
	}
}
