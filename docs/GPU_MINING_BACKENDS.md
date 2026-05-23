# GPU mining backends — NVIDIA, AMD, Intel

HackMe pool workers support **native CUDA** (NVIDIA) and **OpenCL** (AMD, Intel, and any CL1.2+ GPU). One coordinator; pick the backend that matches your hardware.

## Verdict (production)

| Platform | Backend | Binary | Typical GH/s (PoH batch 4M) |
|----------|---------|--------|-----------------------------|
| **NVIDIA** (RTX 30/40/50) | **CUDA** (recommended) | `bin/workerpoh-cuda` | ~20–130 (device + driver) |
| **AMD** (RX 6000/7000) | OpenCL | `bin/workerpoh-opencl` | ~2–15 (Mesa/ROCm) |
| **Intel** (Arc / iGPU) | OpenCL | `bin/workerpoh-opencl` | ~0.5–8 |
| CPU-only (VPS) | — | `workerpoh` / loop | ~0.02–0.35 |

**Your desktop (RTX 5060 Ti): CUDA is the correct choice.** OpenCL on the same card was ~9 GH/s mis-reported and slower; native CUDA with NVRTC `compute_120` reaches measured **~80–116 GH/s** on the public pool.

## Why CUDA is ideal on NVIDIA

1. **Direct driver API** — no ICD layer (`pci id … driver (null)` OpenCL issues on new Blackwell cards).

### Global GPU matrix audit (all generations, simulated)

Cross-vendor compatibility and chaos resilience (VRAM/TDR/thermal → CPU fallback, no worker panic):

```bash
bash scripts/tests/global_gpu_matrix_hardware_audit.sh
# optional live probes on this host:
LIVE_HOST=1 bash scripts/tests/global_gpu_matrix_hardware_audit.sh
```

Covers Pascal→Blackwell/H100/B200 (Green), Polaris/RDNA (Red), Intel Arc (Blue). Reports under `reports/tests/<run_id>/global_gpu_matrix/`.
2. **NVRTC JIT** — kernel compiled for exact compute capability (`compute_120` on RTX 50).
3. **Stable timing** — kernel duration after `cuSynchronize`; pool hashrate matches real throughput.
4. **Production path** — `build_cuda_worker.sh`, `apply_desktop_gpu_pool.sh`, `desktop_worker_reset.sh` (CUDA direct start).

OpenCL remains supported for **non-NVIDIA** rigs and as emergency fallback (`HACKME_FORCE_OPENCL=1`).

## Build everything

```bash
bash scripts/ops/build_gpu_workers.sh
# or NVIDIA only:
bash scripts/ops/build_cuda_worker.sh --probe
```

Outputs:

- `bin/workerpoh-cuda` — `-tags cuda,opencl` (CUDA search + OpenCL discovery)
- `bin/workerpoh-opencl` — `-tags opencl` (AMD/Intel/VPS without NVRTC)
- `bin/workerpoh` — symlink to best backend for **this** machine

## Auto-detect backend

```bash
bash scripts/ops/detect_gpu_backend.sh
# cuda | opencl | cpu
```

Logic:

1. `HACKME_GPU_BACKEND=cuda|opencl|auto` (explicit wins)
2. `HACKME_FORCE_OPENCL=1` → OpenCL
3. NVIDIA GPU + CUDA toolkit / `workerpoh-cuda` → **cuda**
4. AMD (`0x1002`) / Intel (`0x8086`) GPU via sysfs or `clinfo` → **opencl**
5. Else **cpu**

## Environment (all vendors)

| Variable | Purpose |
|----------|---------|
| `HACKME_GPU_BACKEND` | `cuda` / `opencl` / `auto` |
| `HACKME_GPU_DISABLE` | `1` — CPU only |
| `HACKME_FORCE_OPENCL` | `1` — skip CUDA on NVIDIA |
| `HACKME_WORKER_HASHRATE_GHS` | Declared floor (not ceiling on CUDA) |
| `HACKME_CUDA_CALIBRATE_GHS` | Override calibrated GH/s |
| `HACKME_GPU_CALIBRATE_MOD` | Modulus for startup calibration |
| `HACKME_CUDA_VERBOSE` | `1` — CUDA init/search logs |
| `HACKME_OPENCL_VERBOSE` | `1` — OpenCL search logs |

## NVIDIA (CUDA)

See [CUDA_PRODUCTION.md](CUDA_PRODUCTION.md).

```bash
bash scripts/ops/setup_cuda_desktop.sh
export HACKME_GPU_BACKEND=cuda
bash scripts/ops/desktop_worker_reset.sh
```

Ubuntu 24.04: `cuda-toolkit-12-8`, driver ≥550 (580+ for RTX 50).

## AMD (OpenCL)

**Debian/Ubuntu:**

```bash
sudo apt install -y opencl-headers ocl-icd-opencl-dev clinfo
# Mesa rusticl (modern AMD):
sudo apt install -y mesa-opencl-icd
# or ROCm OpenCL (datacenter / some desktop):
# sudo apt install -y rocm-opencl-runtime
clinfo | grep -E 'Device Type|Device Name'
```

```bash
bash scripts/ops/build_gpu_workers.sh
export HACKME_GPU_BACKEND=opencl
export HACKME_WORKER_HASHRATE_GHS=5   # honest floor for RX-class
bash scripts/ops/worker_autostart.sh
```

**Windows:** install AMD Adrenalin + OpenCL ICD; build with `go build -tags opencl`.

## Intel (OpenCL)

**Debian/Ubuntu:**

```bash
sudo apt install -y opencl-headers ocl-icd-opencl-dev clinfo intel-opencl-icd
clinfo
```

```bash
export HACKME_GPU_BACKEND=opencl
export HACKME_WORKER_HASHRATE_GHS=2   # Arc / iGPU ballpark
```

Arc discrete GPUs perform better than most iGPUs; set declared GH/s from a few minutes of pool stats.

## Desktop pool (NVIDIA)

```bash
bash scripts/ops/apply_desktop_gpu_pool.sh
# sets HACKME_GPU_BACKEND=cuda, batch 4194304, HASHRATE_GHS=20
```

## Verify

```bash
pgrep -af workerpoh
tail -n 5 "$(ls -t logs/workerpoh-*.log | head -1)"
# NVIDIA:  [CUDA compute_120] … ghs=80+
# AMD/Intel: [OpenCL] … ghs= measured
curl -fsS https://hackme.tech/pool/coordinator/api/work/stats \
  -H "X-Hackme-Admin-Token: $(cat .secrets/hackme_coordinator_admin_token)" \
  | jq .pool_hashrate_gh_s
```

## MiningPoolStats / listing

Pool API exposes `pool_hashrate_gh_s` from live worker submits. Use **CUDA on the main GPU rig** so listed hashrate reflects real contribution; MSK CPU worker stays on low OpenCL/CPU settings.

## Multi-GPU fleet (one worker per card, up to 20)

When `HACKME_GPU_FLEET=1` (default) and multiple GPUs are visible:

- **NVIDIA:** `worker_autostart.sh` spawns `WORKER_ID-gpu0`, `-gpu1`, … each with `-gpu-device N` (CUDA)
- **OpenCL:** same pattern for AMD/Intel discrete GPUs
- **Hybrid (NVIDIA + AMD on one PC):** `HACKME_GPU_HYBRID=auto` (default) runs **CUDA fleet + OpenCL fleet** with per-GPU rig profiles (`bin/fleetplan` prints the plan)
- Coordinator sees **separate rows** in `active_rigs` — pool hash is the **sum** of all worker GH/s

One `workerpoh` process still uses **one backend**; hybrid rigs use **multiple processes** (not CPU fallback on the second vendor).

`CUDA_VISIBLE_DEVICES=0,1` is honored by the driver (device index is within the visible set); combine with `-gpu-device` / `HACKME_GPU_DEVICE`.

## Full rig / GPU test suite

Run on any rig before joining the pool:

```bash
bash scripts/tests/gpu_rig_suite.sh
# Report: reports/tests/<RUN_ID>/gpu_rig_suite/summary.json
```

Covers: host inventory (`nvidia-smi`, `clinfo`, sysfs vendors), backend auto-detect, `build_gpu_workers.sh --probe`, unit tests (`gputune` model matrix for RTX/AMD/Intel names), **CUDA smoke on every device**, OpenCL list/init, fleet count vs discovery, short `-gpu-device` worker smoke, power-limit hints.

CUDA integration test (optional):

```bash
source scripts/ops/cuda_env.sh
HACKME_GPU_INTEGRATION=1 go test -tags cuda ./internal/gpupoh -run TestDiscoverAcceleratorsCUDA -v
```

Single-GPU probe:

```bash
HACKME_CUDA_VERBOSE=1 bin/gpuprobe-cuda
HACKME_OPENCL_VERBOSE=1 bin/gpuprobe-opencl
go run ./tools/listgpu   # with -tags cuda or opencl
go run ./tools/gpuhint "NVIDIA GeForce RTX 5060 Ti"
```
