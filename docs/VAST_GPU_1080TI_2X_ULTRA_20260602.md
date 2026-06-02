# Vast.ai Ultra Test: 2× GTX 1080 Ti (10 series)

Date: 2026-06-02  
Host: `182.224.239.168:42902` (Seoul, KR — `2c1978c3318b`)

SSH:

```bash
ssh -i .secrets/vast/id_ed25519_10series -p 42902 root@182.224.239.168 -L 8080:localhost:8080
# proxy: ssh -p 33797 root@ssh9.vast.ai
```

## Scope

- Inventory + latency (ping, coordinator curl)
- 7 min dual-GPU mining (`420s`)
- Per-GPU OpenCL (fleet autostart failed: no `go`, fleetplan 1 slot)
- Aggregated worker id `vast-1080ti-2x` (one coordinator key)
- `target_mod` drift + local fuzz gate

## Inventory

| Item | Value |
|------|--------|
| GPUs | 2× NVIDIA GeForce GTX 1080 Ti |
| Driver | 580.126.09, CC **6.1** (Pascal) |
| VRAM | 11 264 MiB each |
| Worker ID | `vast-1080ti-2x` |

## Network

- ping `hackme.tech`: **~96 ms** avg (Seoul)
- Coordinator `/health` TTFB ~612 ms, `/api/work/stats` ~645 ms

## Runtime notes

- `01_run_fleet.sh` did **not** start miners (`go: command not found`, fleetplan returned 1 slot).
- **Manual dual OpenCL** with unique seed + nonce per GPU (`gpu0`, `gpu1` logs).

### Per-GPU (OpenCL)

| GPU | Calibrated GH/s | submit ok | 403 |
|-----|-----------------|-----------|-----|
| 0 | **46.12** | 232 | 0 |
| 1 | **43.11** | 208 | 0 |
| **Sum (logs)** | **~89.2** | **440** | **0** |

- `found=true`: 11 total  
- `target_mod`: **85 001 082 → 136 341 780**

CUDA: `libnvrtc.so.12` not found on host — OpenCL used (expected for this pack).

## Coordinator (`vast-1080ti-2x`)

- **Single key** in `workers` (no `-gpu0`/`-gpu1` split) — **PASS (UI row)**
- `online`: true
- `hashrate_gh_s` / `peak`: **~45.1 / 45.7** (coordinator reports ~one-stream rate; log sum ~89 GH/s)
- `accepted_hits`: 11
- `payout_hmc`: ~0.25
- `signed_submits`: 440

> **WARN:** coordinator GH/s may under-report vs sum of per-GPU calibrations until pool aggregate sums `peak_hashrate` across merged submits (same class of issue as early 4060Ti dual). Mining and payout are valid.

## Fuzz

Local `security_audit_gate.sh` → **PASS**, issues 0.

## Verdict

| Area | Result |
|------|--------|
| HMC mining (2× 1080 Ti) | **PASS** |
| OpenCL Pascal | **PASS** |
| One worker row | **PASS** |
| Coordinator GH/s display | **WARN** (~45 vs ~89 GH/s log sum) |
| Fleet autostart on Vast | **FAIL** (needs prebuilt bins, no `go`) |

## Artifacts

- `reports/vast-remote/rtx1080ti-2x-ultra/vast-ultra-1080ti-2x/` (`gpu0.log`, `gpu1.log`)

## 10-series closure

With this run, **NVIDIA 10 series (1080 Ti dual)** is validated for HMC pool mining. Next: **final matrix verdict** + pack rebuild (NVRTC, fleet without `go`, hashrate aggregation).
