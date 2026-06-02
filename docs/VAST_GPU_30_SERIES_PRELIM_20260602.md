# Vast.ai 30 Series Preliminary (2x RTX 3060 Ti)

Date: 2026-06-02  
Host: `141.0.85.211:42060` (provider geohint: Oslo)

## Scope

- Inventory + network latency probe
- Worker launch via packaged matrix bundle
- Coordinator/UI visibility check
- Per-GPU validation on `GPU #0` and `GPU #1`

## Results

- Hardware detected: `2x NVIDIA GeForce RTX 3060 Ti`, driver `590.48.01`, CUDA CC `8.6`.
- Network probe to coordinator:
  - ping `hackme.tech`: avg `1.862 ms`, `0%` packet loss
  - `/api/work/stats` TTFB about `0.072 s`
  - `/api/pool/stats` TTFB about `5.27 s` on sampled run
- CUDA worker in current pack failed on this host:
  - `libnvrtc.so.12` ABI required, host provides CUDA 13 (`libnvrtc.so.13`)
  - simple symlink is not enough (`version 'libnvrtc.so.12' not found`)
- OpenCL fallback mining on same GPUs succeeded:
  - `vast-3060ti-2x-tr-gpu0` online, hashrate about `74.1 GH/s`
  - `vast-3060ti-2x-tr-gpu1` online, hashrate about `73.9 GH/s`
  - accepted attempts/hits and payout increased for both workers

## Verdict (current build on this host)

- **PASS (mining path):** 30 series mining confirmed on this host via OpenCL, including both GPUs.
- **WARN (pack compatibility):** CUDA binary compatibility issue on CUDA 13 hosts remains and must be fixed in final rebuild.

## Artifacts

- Local reports:
  - `reports/vast-remote/rtx3060ti-2x-tr/vast-session/`
  - `reports/vast-remote/rtx3060ti-2x-tr/vast-session-fix/`

