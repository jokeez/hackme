# GPU Matrix Field Test — Final Verdict (2026-06-02)

Production pool: `https://hackme.tech/pool/coordinator`  
Method: rented Vast.ai rigs + operator desktop (RTX 5060 Ti), ultra runs (inventory, latency, 6–7 min mining, coordinator stats, optional fuzz gate).

---

## Executive summary

We ran **real HMC mining** across **NVIDIA generations from Pascal (GTX 1080 Ti) to Blackwell (RTX 5090)** on production infrastructure. Every target generation **submitted signed shares**, increased **hits / payout**, and showed **live difficulty retarget** (`target_mod` growth).

**Overall: PASS** for pool readiness. **One release train** remains to harden the miner pack (CUDA 13 / NVRTC, Vast fleet without `go`, aggregated GH/s display).

---

## Results matrix

| Series | Config | Worker ID | Peak GH/s | Region / ping | Mining | Notes |
|--------|--------|-----------|-----------|---------------|--------|-------|
| **10xx** | 2× GTX 1080 Ti | `vast-1080ti-2x` | **~89** (logs); UI ~45 | Seoul ~96 ms | **PASS** | OpenCL; dual manual (fleet needs `go` on host) |
| **20xx** | 4× RTX 2080 Ti | `vast-2080ti-rig` | **~92** agg | TR ~44 ms | **PASS** | CUDA after NVRTC symlink |
| **30xx** | 2× RTX 3060 Ti | `vast-3060ti-2x-tr` | **~74+74** | Oslo ~1.9 ms | **PASS** | OpenCL (CUDA13 NVRTC fail) |
| **30xx** | 2× RTX 3070 | `vast-3070-2x-it` | **~60** | IT | **PASS** | CUDA |
| **40xx** | 2× RTX 4060 Ti | `vast-4060ti-2x` | **~40–41** | EU | **PARTIAL** | mining OK; early UI split `-gpu0/1` |
| **40xx** | 2× RTX 4070 Ti SUPER | `vast-4070tis-2x-us` | **~123** agg | US ~16 ms | **PASS** | OpenCL, one UI row |
| **40xx** | 1× RTX 4090 | `vast-4090-01` | **~149** | JP ~238 ms | **PASS** | OpenCL; high RTT still OK |
| **50xx** | 1× RTX 5090 | `vast-5090-01` | **~140** | CZ **~0.8 ms** | **PASS** | OpenCL; best latency |
| **Desktop** | 1× RTX 5060 Ti | `worker-kapa-pc` | **~87–90** | local | **PASS** | OpenCL; CUDA `driver (null)` on Blackwell |

**Not required for HMC consumer-miner goal:** Data Center (A100/H100/B200), GTX 900/700, Quadro P-series, Titan (optional smoke only).

---

## What we fixed during / after the matrix

### Deployed to production coordinator (VPS)

1. **Multi-GPU worker aggregation** — `fleetBaseWorkerID()` + `mergeWorkerStat()` so `worker-gpu0` / `worker-gpu1` collapse to one rig row in `/api/work/stats` and pool UI.
2. **Fleet aggregate GH/s** — summed `LastHashrateGHS` across merged slots; peak tracks combined throughput.
3. **`worker_autostart.sh`** — valid `fleetplan` JSON only; **`SKIP_WORKER_BUILD=1`** on Vast (no `go` on image); per-GPU `-gpuN` submit ids when `HACKME_GPU_FLEET_AGGREGATE_ID=1` (fixes under-reported dual-GPU hashrate).

### Pack / ops (this release)

- Matrix tarball includes prebuilt `workerpoh-cuda`, `workerpoh-opencl`, `fleetplan`, `minersign`.
- `scripts/vast/01_run_fleet.sh` defaults `HACKME_GPU_FLEET_AGGREGATE_ID=1`.
- Per-GPU nonce files in aggregate mode (replay-safe).

### Still for next miner pack build (tracked, not blocking verdict)

| Item | Impact |
|------|--------|
| **NVRTC / CUDA 13** on Vast | Native CUDA fails → OpenCL fallback works |
| **Blackwell desktop CUDA** | `driver (null)` → use OpenCL until driver/toolkit catch-up |
| **Fleet autostart on bare Vast** | Requires prebuilt bins (`SKIP_WORKER_BUILD=1`) — now respected |

---

## Why the system is better

1. **Proof, not theory** — hashrate, hits, and HMC payout were measured on **live coordinator**, not a local fork.
2. **One rig, one row** — multi-GPU rentals match how operators think (single worker in dashboard).
3. **Graceful degradation** — OpenCL path keeps mining when NVRTC/CUDA mismatch would otherwise hard-fail.
4. **Anti-abuse hygiene** — unique seeds/nonces per GPU; `clear-abuse` + replay lessons documented.
5. **Geography** — latency probes (5090 ~0.8 ms EU vs 4090 ~238 ms APAC) inform where to host workers vs coordinator edge.

---

## Per-run documentation

| Report |
|--------|
| [VAST_GPU_20_SERIES_FINAL.md](VAST_GPU_20_SERIES_FINAL.md) |
| [VAST_GPU_30_SERIES_PRELIM_20260602.md](VAST_GPU_30_SERIES_PRELIM_20260602.md) |
| [VAST_GPU_3070_SERIES_PRELIM_20260602.md](VAST_GPU_3070_SERIES_PRELIM_20260602.md) |
| [VAST_GPU_4070TIS_ULTRA_20260602.md](VAST_GPU_4070TIS_ULTRA_20260602.md) |
| [VAST_GPU_4090_ULTRA_20260602.md](VAST_GPU_4090_ULTRA_20260602.md) |
| [VAST_GPU_5090_ULTRA_20260602.md](VAST_GPU_5090_ULTRA_20260602.md) |
| [VAST_GPU_1080TI_2X_ULTRA_20260602.md](VAST_GPU_1080TI_2X_ULTRA_20260602.md) |

Artifacts: `reports/vast-remote/*`

---

## Final verdict

| Layer | Status |
|-------|--------|
| HMC pool mining (all tested generations) | **PASS** |
| Coordinator / payout / difficulty | **PASS** |
| Multi-GPU UI aggregation | **PASS** (after VPS deploy + autostart fix) |
| CUDA pack on CUDA-13 Vast images | **WARN** — ship NVRTC13 build |
| Operator desktop 5060 Ti | **PASS** (OpenCL) |

**Recommendation:** proceed with **customer-facing pool** for NVIDIA miners; ship **one consolidated miner/pack release** with NVRTC13 + documented OpenCL fallback.

---

*Generated after field matrix 2026-06-02. Deploy: `NODE_SSH=hackme-vps bash scripts/ops/deploy_hackme_node.sh`.*
