package gpuhost

import "testing"

func TestClassifyNameNVIDIA(t *testing.T) {
	nv, amd, intel := classifyName("NVIDIA GeForce RTX 5060 Ti")
	if !nv || amd || intel {
		t.Fatalf("nv=%v amd=%v intel=%v", nv, amd, intel)
	}
}

func TestClassifyNameAMD(t *testing.T) {
	nv, amd, intel := classifyName("AMD Radeon RX 580 2048SP")
	if nv || !amd || intel {
		t.Fatalf("nv=%v amd=%v intel=%v", nv, amd, intel)
	}
}

func TestClassifyNameIntelArc(t *testing.T) {
	nv, amd, intel := classifyName("Intel Arc A770 Graphics")
	if nv || amd || !intel {
		t.Fatalf("nv=%v amd=%v intel=%v", nv, amd, intel)
	}
}

func TestResolveBackendNVIDIAWithCUDA(t *testing.T) {
	b := ResolveBackend(BackendChoiceInput{
		Report:           HostGPUReport{HasNVIDIA: true, Names: []string{"RTX 5060 Ti"}},
		HasCUDAWorkerBin: true,
		HasOCLWorkerBin:  true,
		NVIDIASMIOK:      true,
	})
	if b != "cuda" {
		t.Fatalf("got %q want cuda", b)
	}
}

func TestResolveBackendAMDOpenCL(t *testing.T) {
	b := ResolveBackend(BackendChoiceInput{
		Report:          HostGPUReport{HasAMD: true},
		HasOCLWorkerBin: true,
	})
	if b != "opencl" {
		t.Fatalf("got %q want opencl", b)
	}
}

func TestResolveBackendNoGPU(t *testing.T) {
	b := ResolveBackend(BackendChoiceInput{Report: HostGPUReport{}})
	if b != "cpu" {
		t.Fatalf("got %q want cpu", b)
	}
}
