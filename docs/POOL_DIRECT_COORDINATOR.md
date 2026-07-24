# Direct pool coordinator (bypass Cloudflare)

High-GH GPU miners can hit the origin directly to avoid CF timeouts on claim/submit.

## Endpoint

- URL: `http://132.243.112.100:18083`
- Auth: worker/coordinator token (`X-Hackme-Admin-Token` / pool worker token)
- Nginx: `deploy/nginx/hackme-pool-direct.conf` → `127.0.0.1:18081`
- **Allowlist only:** `/health`, `/api/work/claim|submit|stats|by-wallet`, `/api/pool/stats`, fuzz marketplace/settle pull (`/api/fuzz/pool/campaigns/list|progress`, `/api/fuzz/pool/stats`, `/api/fuzz/pool/settle/outbox(|/ack)`, `/api/fuzz/work/claim|submit`)
- **Blocked at edge:** `/api/work/admin/*`, fuzz cleanup/admin, everything else → `403`

## Worker config (no binary rebuild)

```bash
export COORD_URL=http://132.243.112.100:18083
export HACKME_POOL_COORDINATOR_URL=http://132.243.112.100:18083
export HACKME_POOL_DIRECT=1
export HACKME_DESKTOP_GPU_POOL=1
export HACKME_WORKER_BATCH_SIZE=16777216   # fewer RTTs per attempt
export HACKME_WORKER_CLAIM_COOLDOWN_MS=100 # avoid 429 bans on fast GPUs
```

Or flags:

```bash
bin/workerpoh-cuda -coord http://132.243.112.100:18083 -batch 16777216 ...
```

**Defaults (fleet templates):** `worker_autostart.sh` rewrites public CF → direct when `HACKME_DESKTOP_GPU_POOL=1` (or always when `HACKME_POOL_DIRECT=1`); CUDA/desktop GPU batch defaults to 16M; claim cooldown floors 0→100. Rig profiles bake 16M + 100ms for RTX 30/40/50, RDNA2/3, Hopper. Casual miners without those flags keep HTTPS via Cloudflare.

**Hybrid fuzz (fleet default ON):** release miners dig pool fuzz under the same `WORKER_ID` (`HACKME_WORKER_HYBRID_FUZZ` defaults on, mode=`inline`). Escape hatch `=0`. Process mode / dedicated dig needs `bin/workerfuzz` (linux + windows in the release bundle). Pure fuzz diggers (`bootstrap-fuzz-*`, `vps-canary-fuzz-*`) correctly show **0 GH** in the PoH workers table — they claim `/api/fuzz/work/*`, not PoH hashrate.

## Templates / release

Examples ship the bake-in values: `.env.desktop.example`, `scripts/ops/desktop_worker.env.example`, Vast pack, ISO miner firstboot / `run-miner-worker.sh`, Windows `write_hackme_env.ps1`. Reinstall or restart worker after pulling so env picks up the new defaults.
