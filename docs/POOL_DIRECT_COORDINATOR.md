# Direct pool coordinator (bypass Cloudflare)

High-GH GPU miners can hit the origin directly to avoid CF timeouts on claim/submit.

## Endpoint

- URL: `http://132.243.112.100:18083`
- Same coordinator API as `https://hackme.tech/pool/coordinator`
- Auth: existing worker/admin token (`X-Hackme-Admin-Token`)
- Nginx: `deploy/nginx/hackme-pool-direct.conf` → `127.0.0.1:18081`

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
