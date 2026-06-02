# Vast.ai Ultra Test: 2x RTX 4070 Ti SUPER

Date: 2026-06-02  
Host: `98.191.113.4:10720` (Phoenix, Arizona, US)

## Scope

- Full inventory and latency probe
- CUDA startup path check
- Dual-GPU run with one aggregated `worker_id`
- Difficulty/hashrate/accepted/payout validation against coordinator

## Inventory and Network

- GPUs: `2x NVIDIA GeForce RTX 4070 Ti SUPER`
- Driver: `580.95.05`, CC `8.9`, VRAM `~16 GiB` each
- ping `hackme.tech`: avg `~16.2 ms`, `0%` packet loss
- Coordinator API timings were stable in this run

## Runtime Findings

- CUDA binary from current pack did not run on this host:
  - `libnvrtc.so.12` symbol version mismatch on CUDA 13 runtime host
  - symlink alone was not sufficient (`version 'libnvrtc.so.12' not found`)
- OpenCL fallback succeeded on both GPUs with no replay errors after per-GPU seed/nonce isolation.

## Aggregation and Live Metrics (important)

- Worker id used: `vast-4070tis-2x-us` (single row expected in UI/work stats)
- Coordinator response showed only one key for this worker family:
  - `keys = ["vast-4070tis-2x-us"]`
- Snapshot:
  - `online: true`
  - `hashrate_gh_s: 122.66847962029632`
  - `accepted_attempts: 1,526,726,656`
  - `accepted_hits: 209`
  - `payout_hmc: 7.8141027247574515`
  - `target_mod: 3,639,787`
  - `pool_hashrate_gh_s: 123.04993690029632`

## Verdict

- **PASS (display + aggregation):** two GPUs are active and represented under one aggregated worker row with current GH/s.
- **PASS (mining):** accepted attempts/hits/payout increased strongly during run.
- **WARN (CUDA pack compatibility):** host with CUDA 13 still requires final binary compatibility fix for native CUDA path.

## Artifacts

- `reports/vast-remote/rtx4070tis-2x-us/vast-session/`
- `reports/vast-remote/rtx4070tis-2x-us/vast-session-ultra/`

