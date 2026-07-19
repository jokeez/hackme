# Direct pool coordinator (bypass Cloudflare)

High-GH GPU miners can hit the origin directly to avoid CF timeouts on claim/submit.

## Endpoint

- URL: `http://132.243.112.100:18083`
- Auth: worker/coordinator token (`X-Hackme-Admin-Token` / pool worker token)
- Nginx: `deploy/nginx/hackme-pool-direct.conf` → `127.0.0.1:18081`
- **Allowlist only:** `/health`, `/api/work/claim|submit|stats|by-wallet`, `/api/pool/stats`
- **Blocked at edge:** `/api/work/admin/*`, fuzz, everything else → `403`

## Worker config (no binary rebuild)

```bash
export COORD_URL=http://132.243.112.100:18083
export HACKME_POOL_COORDINATOR_URL=http://132.243.112.100:18083
export HACKME_WORKER_BATCH_SIZE=16777216   # fewer RTTs per attempt
export HACKME_WORKER_CLAIM_COOLDOWN_MS=100 # avoid 429 bans on fast GPUs
```

Or flags:

```bash
bin/workerpoh-cuda -coord http://132.243.112.100:18083 -batch 16777216 ...
```

Public HTTPS via Cloudflare remains the default for casual miners.

## Next miner binary release

When shipping a new `workerpoh` / installer / ISO / vast image, bake in the defaults above (cooldown floor, 16M CUDA batch, documented direct URL). See Obsidian: `Ops/Pool GPU Direct + Next Binary`.
