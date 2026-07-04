# Vast.ai GPU matrix — runbook (HackMe pool)

Pack for tomorrow: one tarball with Linux `workerpoh-cuda`, scripts, env template.

## Build pack (on your Linux desktop, tonight)

```bash
cd /path/to/HackMe

# Optional: one miner seed for all test rigs (or new seed per GPU)
./bin/minersign -gen-seed   # if minersign exists locally

# Fill local secrets (not committed):
cp env.vast.example .env.vast   # or edit after pack
# COORD_TOKEN from .secrets/hackme_coordinator_worker_token
# HACKME_MINER_ED25519_SEED_HEX=...

bash scripts/ops/pack_vast_gpu_matrix.sh --include-token
# → dist/vast-gpu-matrix-<stamp>.tar.gz
```

Without `--include-token`: copy token into `env.vast` on each instance manually.

## Pick instances on [Vast.ai](https://cloud.vast.ai/create/)

- Ubuntu 22/24, **CUDA 12+**, `nvidia-smi` works in template
- **≥ 8 GB VRAM** for batch 4M
- Unique **WORKER_ID** per instance: `vast-rtx4090-01`, etc.
- **Stop/destroy** when done (set billing alert)

Suggested matrix (3–4 runs):

| Slot | GPU class |
|------|-----------|
| 1 | Cheap 8GB (T4 / 3060) |
| 2 | RTX 3080/3090 |
| 3 | RTX 4090 |
| 4 | Older Pascal (1080 Ti) if cheap |

## On each Vast instance

```bash
tar xzf vast-gpu-matrix-*.tar.gz
cd vast-gpu-matrix-*/

# If env.vast not pre-filled:
cp env.vast.example env.vast
nano env.vast   # WORKER_ID unique per box

bash scripts/00_inventory.sh
bash scripts/01_run_pool_worker.sh    # default 30 min
# Ctrl+C or wait timeout

bash scripts/02_collect_report.sh
```

Copy back to PC:

```bash
scp -P <port> root@<vast-ip>:~/vast-gpu-matrix-*/reports/vast-session ./reports/vast-from-rtx4090/
```

## Verify on hub

- [MPS HMC](https://miningpoolstats.app/coins/HMC) — workers + hashrate
- `curl -sS https://hackme.tech/pool/coordinator/api/pool/stats | jq`

## Do not on Vast

- security-audit / heavy fuzz (CPU $$$)
- admin token on instances
- leave instances running overnight without cap

## Optional (if repo cloned on instance + `go` installed)

```bash
LIVE_HOST=1 bash scripts/tests/global_gpu_matrix_hardware_audit.sh
bash scripts/tests/gpu_rig_suite.sh
```

The pack is enough without Go.

## Troubleshooting

| Issue | Fix |
|-------|-----|
| `error while loading shared libraries` | Build pack on same arch (linux amd64); on Vast use CUDA template |
| NVRTC / CUDA init fail | Try newer driver image; `HACKME_CUDA_VERBOSE=1` |
| 401 on claim | Wrong token — use **worker** token |
| Low GH/s | Expected on small GPUs; note in sheet |
| OpenCL on NVIDIA | Force `HACKME_GPU_BACKEND=cuda` |

See also [GPU_MINING_BACKENDS.md](GPU_MINING_BACKENDS.md).
