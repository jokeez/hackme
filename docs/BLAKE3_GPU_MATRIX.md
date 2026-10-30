# Blake3 PoH GPU matrix (LIVE fv≥3)

**Status:** CUDA soft-launch matrix filled through Pascal 10xx; **AMD in-matrix = RX 580 2048SP only** (OpenCL device PASS: burn ~0.8 · pool ~0.94 SHARE_STRICT). No other RX / RDNA rows until calibrated.  
**Do not compare Peak GH/s to the June 2026 algebraic table** — that was pre-cliff hybrid/v1 calib.

Historical (pre-Blake3) matrix: [`archive/vast/VAST_GPU_MATRIX_VERDICT_20260602.md`](archive/vast/VAST_GPU_MATRIX_VERDICT_20260602.md) — kept as archive only.

---

## How to read columns

| Column | Meaning |
|--------|---------|
| **Burn GH/s** | Pure SearchPoH wall (`blake3bench` / calib maxBatch) — kernel truth |
| **Pool GH/s** | **Only** from `GET /api/work/stats` → `active_rigs[].hashrate_gh_s` (coordinator credit). Never from worker `submit ok ghs=` logs. |
| **Util %** | nvidia-smi during mining (often low even when burn is healthy) |
| **Backend** | `cuda` device / `opencl` device / `opencl→host` (host only if device build fails or `HACKME_OPENCL_BLAKE3_HOST=1`) |

Method: tip ≥#485000, `pool_hashrate_kind=blake3_pow_ghs`, SHARE_STRICT PPS, adaptive leases. Official `0.1.0-rc18` channel; device kernels still beat host fallback.

**Economics (2026-10-25+):** Blake3 **share-PPS** (`SHARE_STRICT=1`): pay verified shares; pool GH/M from shares/wall — not client `hashrate_gh_s`. Live `REWARD_AUTO=0` + `REWARD_PER_M=5e-5`, `found_bonus=0.0001`, `max_claim_batch` up to 64G. Useful customer work lives on **Dig/Hunt fuzz lanes**, not Blake3 PoH itself.

---

## Results matrix (Blake3)

| Series | Config | Worker / rig | Burn GH/s | Pool GH/s | Util % | Backend | Verdict | Notes |
|--------|--------|--------------|-----------|-----------|--------|---------|---------|-------|
| **50xx** | 1× RTX 5060 Ti | `worker-kapa-pc` | **~5–7.8** | **~5–6.5** | ~15–30 | **cuda** | **PASS** | Desktop under SHARE_STRICT; prefer CUDA (OpenCL ICD also has device Blake3) |
| **50xx** | 1× RTX 5090 | `vast-rtx5090-blake3-01` | **~26.8** (2²⁶) | **~20–27** (SHARE_STRICT era; ignore pre-fix ~1 GH hist) | ~0–2 | **cuda** | **PASS** | Burn locked; pool credit scales with PPS once leased (instance often gone) |
| **50xx** | **2× RTX 5090** | `vast-2x5090` (`-gpu0`+`-gpu1`) | **~26.3 + ~26.4** | **~29–38** merged | ~0–2 | **cuda** | **PASS** | Σ burn ~52.7; fleet merge strip `-gpuN` |
| **40xx** | 1× RTX 4090 | `vast-rtx4090-blake3-01` | **~17.1** (2²⁶) | **~17–25** | ~0 | **cuda** | **PASS** | CC 8.9 |
| **40xx** | 2× RTX 4070 Ti S | `vast-4070tis-az` (`-gpu0`+`-gpu1`) | **~11.9 + ~11.9** (2²⁶) | **~12.6–27** merged | ~0–99 | **cuda** | **PASS** | NVRTC12 on CUDA13 images; `impossible_found_rate` false ban fixed |
| **30xx** | 1× RTX 3060 Ti | `vast-3060ti-01` | **~4.83** (2²⁶) | **~4.9–5.1** | ~98 | **cuda** | **PASS** | ≈ desktop 5060 Ti class on Blake3 |
| **30xx** | **2× RTX 3060 Ti** | `vast-2x3060ti` (`-gpu0`+`-gpu1`) | **~5.05 + ~4.85** (2²⁶) | **~10.9** merged | ~0–98 | **cuda** | **PASS** | Σ burn ≈ pool |
| **20xx** | **4× RTX 2080 Ti** | `vast-4x2080ti` (`-gpu0…3`) | **~5.88+5.65+5.88+5.81** | **~19.4** merged (Σ burn ~23.2) | ~0–84 | **cuda** | **PASS** | nvidia sees **4**; fleet → one UI row; CC 7.5 |
| **10xx** | **6× GTX 1070 Ti** | `vast-6x1070ti` (`-gpu0…5`) | **~2.00×6** (2²⁶) | **~16.2** merged (Σ burn ~12) | ~0–100 | **cuda** | **PASS** | Pascal CC 6.1; replaces 1080 Ti target |
| **AMD** | 1× RX 580 **2048SP** (Polaris) | `worker-indep-alpha-80194f` (LAN docker→public pool) | **~0.80–0.84** (2²⁵–2²⁶) | **~0.94** | — | **opencl device** | **PASS** | Device kernel + SHARE_STRICT; single worker `GPU_CHUNK=16M`; bravo/charlie stopped; rusticl/Polaris |

---

## Multi-GPU notes

- Inventory: `nvidia-smi -L` / `blake3bench -device N` → N devices.  
- Mining: one `workerpoh-cuda` per GPU (`GPU_DEVICE` / `-gpu-device`, `WORKER_ID=…-gpuN`).  
- Pool UI: fleet merge strips `-gpuN` → **one** row (Σ credit), not N rows.

---

## Next fills

1. **Windows** CUDA/OpenCL smoke.  
2. Economy / RCF pass (after Windows).  
3. Other AMD SKUs only when a real burn exists — do not invent RX 6000/7000 rows.

```bash
bash scripts/vast/build_blake3_pack.sh   # bundles libnvrtc.so.12 for CUDA13 images
```

**Stability:** in-kernel shares; host verify before submit; bootstrap 512M + short-wall lease grow; CPU Blake3 chunks full lease; Hunt CLEAN floor no longer full PayRun.
