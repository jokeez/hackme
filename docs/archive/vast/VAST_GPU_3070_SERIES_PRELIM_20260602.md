# Vast.ai 30 Series Preliminary (2x RTX 3070)

Date: 2026-06-02  
Host: `151.95.236.135:51652` (geohint: Mestre, IT)

## Scope

- Full remote inventory + latency probe
- Main worker run from packaged bundle
- Coordinator/UI visibility check
- Optional CPU baseline on same host

## Results

- Hardware detected: `2x NVIDIA GeForce RTX 3070`, driver `555.58.02`, CUDA CC `8.6`.
- CUDA runtime compatibility on this host is good (`libnvrtc.so.12` resolved by system CUDA).
- Network probe:
  - ping `hackme.tech`: avg `20.8 ms`, `0%` packet loss
  - `/api/work/stats` TTFB about `1.38 s`
  - `/api/pool/stats` TTFB about `5.35 s` on sampled run
- GPU mining (`worker_id=vast-3070-2x-it`) confirmed:
  - online `true`
  - hashrate about `60.49 GH/s`
  - accepted attempts/hits increased (`accepted_hits=59`)
  - payout increased (`~1.1756 HMC` in sampled snapshot)
- CPU baseline (`worker_id=vast-3070-2x-it-cpu`) confirmed as fallback-only:
  - hashrate about `0.01048576 GH/s` (~10.49 MH/s)
  - accepted hits `0` in short run
  - tiny payout growth (`~0.000249 HMC`)
  - frequent `429 claim_rate_limited` under mixed pool load (expected for low-rate CPU worker)

## Verdict

- **PASS (GPU):** 30-series RTX 3070 mining confirmed on CUDA path.
- **PASS (CPU fallback, limited):** works technically, but efficiency is very low vs GPU.

## Artifacts

- `reports/vast-remote/rtx3070-2x-it/vast-session/`

