# Native CUDA — production setup

HackMe pool workers can use **native NVIDIA CUDA** (NVRTC JIT) instead of OpenCL. CUDA is preferred when built with `-tags cuda` and a toolkit is installed.

**Multi-vendor overview (AMD / Intel / auto-detect):** [GPU_MINING_BACKENDS.md](GPU_MINING_BACKENDS.md)

## Requirements

| Component | Version / notes |
|-----------|-----------------|
| NVIDIA driver | Recent (RTX 50 / Blackwell: latest production driver) |
| CUDA toolkit | 12.x with **nvrtc** (`nvrtc.h`, `libnvrtc`) |
| Go | 1.25+ |
| C compiler | `gcc` / `build-essential` for CGO |
| GPU | Any CUDA-capable GPU; arch auto-detected (e.g. RTX 5060 Ti → `compute_120`) |

## Quick install (Ubuntu / Debian)

```bash
bash ~/Desktop/HackMe/scripts/ops/setup_cuda_desktop.sh
```

**Ubuntu 24.04 (Noble)** — there is no `libnvrtc-dev` package. Use NVIDIA CUDA 12.8 (RTX 5060 Ti / Blackwell needs NVRTC ≥12.8):

```bash
sudo mv /etc/apt/sources.list.d/v2raya.list /etc/apt/sources.list.d/v2raya.list.bak 2>/dev/null || true
wget https://developer.download.nvidia.com/compute/cuda/repos/ubuntu2404/x86_64/cuda-keyring_1.1-1_all.deb
sudo dpkg -i cuda-keyring_1.1-1_all.deb
sudo apt-get update
sudo apt-get install -y cuda-toolkit-12-8
sudo ln -sf /usr/local/cuda-12.8 /usr/local/cuda
bash ~/Desktop/HackMe/scripts/ops/build_cuda_worker.sh --probe
bash ~/Desktop/HackMe/scripts/ops/apply_desktop_gpu_pool.sh
```

Or one script: `sudo bash ~/Desktop/HackMe/scripts/ops/install_cuda_dev_ubuntu.sh`

Fallback (CUDA 12.0, may not JIT `compute_120` on RTX 50): `sudo apt-get install -y nvidia-cuda-dev build-essential`

## Build

```bash
source scripts/ops/cuda_env.sh   # sets CUDA_HOME, CGO_*, LD_LIBRARY_PATH
bash scripts/ops/build_cuda_worker.sh
```

Outputs:

- `bin/workerpoh-cuda` — CUDA + OpenCL discovery (`-tags cuda,opencl`)
- `bin/workerpoh` — symlink to CUDA build
- `bin/workerpoh-opencl` — OpenCL-only fallback

Desktop pool:

```bash
bash scripts/ops/apply_desktop_gpu_pool.sh
```

## Environment

| Variable | Purpose |
|----------|---------|
| `HACKME_GPU_BACKEND` | `cuda` / `opencl` / `auto` |
| `HACKME_CUDA_ARCH` | Force NVRTC arch, e.g. `compute_120` or `sm_120` |
| `HACKME_CUDA_VERBOSE` | `1` — log per-device init and arch |
| `HACKME_CUDA_BLOCK_THREADS` | Block size (default 256) |
| `HACKME_FORCE_OPENCL` | `1` — skip CUDA even if built |
| `CUDA_HOME` | Toolkit root if not auto-detected |

## Architecture selection

On first use per GPU, NVRTC compiles `internal/gpupoh/poh_search.cu` for the device **compute capability** (e.g. 12.0 → `compute_120`). If that fails (older toolkit), a fallback chain is tried down to `compute_60`.

Override for testing:

```bash
export HACKME_CUDA_ARCH=compute_120
```

## Verify

```bash
go build -tags cuda -o bin/gpuprobe-cuda ./tools/gpuprobe/
HACKME_CUDA_VERBOSE=1 ./bin/gpuprobe-cuda
nvidia-smi
# Worker log should show: GPU #0 [CUDA compute_120] …
```

## vs OpenCL

| | CUDA | OpenCL |
|---|------|--------|
| NVIDIA RTX | Native PTX, best throughput | ICD / often slower |
| AMD / Intel | OpenCL (`workerpoh-opencl`) | See [GPU_MINING_BACKENDS.md](GPU_MINING_BACKENDS.md) |
| Build | `nvrtc.h`, CGO | `libOpenCL` |
| MSK CPU worker | Use CPU build | — |

Remote CPU workers: **do not** deploy the CUDA binary; use `go build -o workerpoh ./cmd/workerpoh` (no tags).

## Troubleshooting

**`nvrtc.h: No such file`** — install toolkit; `source scripts/ops/cuda_env.sh`.

**`libOpenCL.so.1` on CUDA binary** — you copied the OpenCL-tagged binary; rebuild with `build_cuda_worker.sh`.

**OpenCL label on RTX 5060 Ti** — CUDA build failed; run `build_cuda_worker.sh` and check compile log.

**Low GH/s with CUDA** — set `HACKME_CUDA_VERBOSE=1`, confirm `compute_120` (not `compute_60`).
