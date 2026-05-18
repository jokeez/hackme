// Package gpupoh implements optional GPU batch search for PoH (eval(n)=n*7+13, eval%mod==0).
//
// Build tags:
//
//	go build -tags cuda -o hackme .          // NVIDIA CUDA (all visible GPUs, NVRTC)
//	go build -tags opencl -o hackme .        // OpenCL (AMD / Intel / others; CGO + OpenCL headers)
//	go build -tags "cuda,opencl" -o hackme . // Prefer CUDA when devices exist, else OpenCL
//
// Default builds (!cuda && !opencl) expose stubs only (CPU PoH).
//
// Environment:
//
//	HACKME_USE_CUDA=0       — force CPU PoH even when GPU build tags are on (same as before).
//	HACKME_FORCE_OPENCL=1 — skip CUDA and use OpenCL only (when built with opencl).
//
// CUDA requires CGO, a C compiler, NVIDIA driver, and CUDA toolkit (nvrtc).
// OpenCL requires CGO, OpenCL ICD/headers (Khronos, vendor SDK, or CUDA toolkit include path).
package gpupoh
