# Vast.ai GPU Final: 20 Series (RTX 2080 Ti)

Date: 2026-06-02  
Scope: HMC mining readiness validation for NVIDIA 20 series on real rented rig.

## Test Configuration

- Provider: Vast.ai
- Rig: 4x RTX 2080 Ti
- Coordinator: `https://hackme.tech/pool/coordinator`
- Worker aggregation mode: single UI worker id `vast-2080ti-rig`
- Per-GPU safety: unique miner seed + unique nonce file per GPU process

## What Was Verified

- CUDA worker startup on 20 series after runtime compatibility fix (`libnvrtc.so.12` symlink to available NVRTC library).
- Multi-GPU adaptivity: all 4 GPUs started and submitted shares.
- Coordinator anti-abuse recovery: replay/temporary ban states were cleared and stable submit flow restored.
- UI aggregation behavior: 4 GPU processes were grouped under one worker id in pool stats.
- Live mining activity: accepted attempts/hits and payout increments observed.
- Network quality to coordinator: stable connectivity with normal latency profile.

## Final Signals (Pass Criteria)

- `vast-2080ti-rig` status: `online=true`
- Non-zero hashrate observed (aggregated rig hashrate sustained and updated over time)
- Accepted shares/hits increased during run
- Payout (`payout_hmc`) increased during run
- No persistent replay/ban condition after seed/worker-id hygiene

## Final Verdict

**PASS**: NVIDIA 20 series (RTX 2080 Ti) mining is confirmed for HMC in production-like pool conditions, including multi-GPU adaptivity and UI visibility.

## Note About Local Desktop Check

During final local checks, port `127.0.0.1:8080` on operator machine was occupied by an SSH tunnel process, which prevented stable local dashboard probing on that port in this session. Remote coordinator metrics still confirmed active 20-series mining and accepted shares.

