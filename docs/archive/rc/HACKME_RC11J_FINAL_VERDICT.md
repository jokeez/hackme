# HackMe 0.1.0-rc11j — Final Release Verdict

Date: 2026-06-02  
Follows: [../vast/VAST_GPU_MATRIX_VERDICT_20260602.md](../vast/VAST_GPU_MATRIX_VERDICT_20260602.md)

## What rc11j delivers

### Miner runtime (all platforms with `workerpoh-cuda`)

1. **CUDA → OpenCL auto-fallback** — if NVRTC/CUDA init fails (common on CUDA 13 Vast hosts), the **same binary** continues on OpenCL instead of dying or falling back to CPU.
2. **CUDA toolkit paths** — build scripts probe `/usr/local/cuda-13` and split Debian `libnvrtc-dev`.
3. **`detect_gpu_backend.sh`** — prefers `workerpoh-cuda` when NVIDIA driver is healthy.

### Pool hub (already on VPS from matrix deploy)

- Multi-GPU **one worker row** + **summed GH/s** in coordinator/UI.
- Vast pack: prebuilt miners, `SKIP_WORKER_BUILD`, fleet aggregate `-gpuN` submit ids.

### Release artifacts (`0.1.0-rc11j`)

| Artifact | Contents |
|----------|----------|
| **Windows** | `HackMe-Setup-0.1.0-rc11j.exe` — wizard, GPU page, pool token, `workerpoh-opencl.exe`, optional `workerpoh-cuda.exe` when built |
| **Linux** | `hackme_0.1.0-rc11j_linux.tar.gz` — `hackme`, `workerpoh-cuda`, `workerpoh-opencl`, `fleetplan`, ops scripts |
| **HackMe OS ISO** | Live USB, Zero-Knowledge Start, autostart pool worker with backend auto-detect |
| **Vast pack** | `dist/vast-gpu-matrix-*.tar.gz` refreshed with rc11j workers |

### Windows installer (master setup)

- Inno Setup wizard: **Auto / CUDA / OpenCL / CPU** on GPU page.
- `write_hackme_env.ps1` + `detect_gpu.ps1` — fair pool batch (`4194304`), no 28s claim sleep on NVIDIA.
- Shortcuts: desktop, optional autostart, launch miner after install.
- `repair_fair_env.bat` — refresh env after upgrades.

### Honest platform notes

| Platform | Expected path |
|----------|----------------|
| **Linux / HackMe OS** | `workerpoh-cuda` → CUDA if NVRTC matches; else OpenCL in-process |
| **Windows** | `workerpoh-opencl.exe` primary for NVIDIA today; installer documents Linux/ISO for max CUDA |
| **Vast rent** | Pack with OpenCL + optional CUDA; no `go` on host |

## Why this is better than rc11i + matrix-only

| Before | After rc11j |
|--------|-------------|
| CUDA fail on CUDA 13 → manual OpenCL or broken worker | **Automatic OpenCL** in same process |
| Multi-GPU split rows / low GH/s display | **One rig, summed GH/s** |
| Vast fleet needed Go toolchain | **Prebuilt pack only** |
| Operator guesswork on backend | **`auto` + probe scripts** |
| Matrix proved mining but pack lagged | **Release, ISO, installer aligned with proof** |

## Upgrade path

1. Download **0.1.0-rc11j** from [downloads](https://hackme.tech/downloads.html).
2. Verify SHA256 (`SHA256SUMS.txt` / `SHA256SUMS-iso.txt`).
3. Windows: run new Setup or `repair_fair_env.bat` in install folder.
4. Linux: replace `bin/workerpoh-cuda` + `worker_autostart.sh` from tarball.
5. Pool: no miner change required for hub fixes (already live).

## Downloads

- https://hackme.tech/downloads.html  
- https://hackme.tech/dist/release_0.1.0-rc11j/
