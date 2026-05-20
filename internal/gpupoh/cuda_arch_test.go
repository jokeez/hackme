package gpupoh

import "testing"

func TestCudaComputeArch(t *testing.T) {
	cases := []struct {
		maj, min int
		want     string
	}{
		{12, 0, "compute_120"},
		{8, 9, "compute_89"},
		{7, 5, "compute_75"},
		{6, 0, "compute_60"},
	}
	for _, c := range cases {
		if got := cudaComputeArch(c.maj, c.min); got != c.want {
			t.Fatalf("cudaComputeArch(%d,%d)=%q want %q", c.maj, c.min, got, c.want)
		}
	}
}

func TestNvrtcArchChainEnvOverride(t *testing.T) {
	t.Setenv("HACKME_CUDA_ARCH", "compute_89")
	chain := nvrtcArchChain(12, 0)
	if len(chain) != 1 || chain[0] != "compute_89" {
		t.Fatalf("env override: %v", chain)
	}
	t.Setenv("HACKME_CUDA_ARCH", "sm_120")
	chain = nvrtcArchChain(8, 9)
	if len(chain) != 1 || chain[0] != "sm_120" {
		t.Fatalf("sm override: %v", chain)
	}
}

func TestNvrtcArchChainPrimaryFirst(t *testing.T) {
	t.Setenv("HACKME_CUDA_ARCH", "")
	chain := nvrtcArchChain(12, 0)
	if chain[0] != "compute_120" {
		t.Fatalf("primary arch first: %v", chain)
	}
}
