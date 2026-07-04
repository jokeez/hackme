# rc11j regression — 2× RTX 3060 Ti (Vast)

One-shot field check after **0.1.0-rc11j**: CUDA init fails on CUDA 13 → **auto OpenCL** (no `HACKME_FORCE_OPENCL`).

## Rent criteria

- **2× RTX 3060 Ti**, Linux, `nvidia-smi` OK
- Prefer **CUDA 13** toolkit on host (same class as 2026-06-02 matrix WARN)
- Region: low ping to `hackme.tech` (EU/TR/US — any stable host)

## SSH key (reuse 30-series)

Public key to paste in Vast **SSH Keys**:

```
ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBMBDyheicJZZLwNoebKkh6fQctalXwbDvUVa06R8Kei vast-30series-20260602T083556Z
```

Private key (local only): `.secrets/vast/id_ed25519_30series`

## After rent — tell agent

| Field | Example |
|-------|---------|
| `host` | `141.0.85.211` |
| `port` | `42060` |
| `proxy_ssh` | `ssh -p 33797 root@ssh9.vast.ai` (if Vast shows direct SSH fails) |
| `region` | `TR` / `EU` |

Agent runs:

```bash
bash scripts/vast/ssh_run_session.sh rtx3060ti-2x-rc11j
```

Pack: latest `dist/vast-gpu-matrix-*.tar.gz` (includes rc11j `workerpoh-cuda` + pool token).

## Pass criteria

1. Log contains `CUDA unavailable` → `trying OpenCL` (with `HACKME_CUDA_VERBOSE=1`)
2. Both GPUs mining, **~70+ GH/s each** (OpenCL path)
3. Pool UI: one aggregated worker row OR merged GH/s (fleet aggregate)
4. Accepted hits / payout tick up

## Session length

`run_seconds`: **900** (15 min) — enough for verdict.
