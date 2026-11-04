# Direct pool coordinator (bypass Cloudflare)

High-GH GPU miners can hit the origin directly to avoid CF timeouts on claim/submit.

## Endpoints

| Mode | URL | Notes |
|------|-----|-------|
| **Cleartext (current / default)** | `http://132.243.112.100:18083` | **Use this today.** No TLS — traffic (incl. worker token) is plaintext. Fleet default until a trusted leaf exists. |
| **TLS by IP (lab only)** | `https://132.243.112.100:18443` | Self-signed `CN=hackme.tech` **without IP SANs** — stock Go workers reject it (`x509: … doesn't contain any IP SANs`). Do **not** put this URL in miner env. |
| **TLS by DNS (target)** | `https://pool-direct.hackme.tech:18443` | Requires **DNS-only** (grey-cloud) A → `132.243.112.100` + Let's Encrypt (or other public CA). Then workers work without custom RootCAs. Orange-cloud CF proxy defeats the bypass. |

- Auth: **pool worker / join token** only (public `pool.miner.token` or operator-issued worker credential). Miner claim/submit paths do **not** use operator admin credentials.
- Nginx: `deploy/nginx/hackme-pool-direct.conf` (:18083) + `deploy/nginx/hackme-pool-direct-tls.conf` (:18443) → shared snippet `deploy/nginx/snippets/hackme-pool-direct-locations.conf` → `127.0.0.1:18081`
- **Allowlist only (miner paths):** `/health`, `/api/work/claim|submit|stats|by-wallet`, `/api/pool/stats`, fuzz marketplace (`/api/fuzz/pool/campaigns/list|progress`, `/api/fuzz/pool/stats`), Dig/Hunt worker paths (`/api/fuzz/work/claim|claim_batch|submit|submit_batch|corpus_snapshot|corpus_delta|release`, Hunt harness GET)
- **Blocked at edge:** `/api/work/admin/*`, fuzz cleanup/admin, many settle routes → `403`
- **Desktop / hub settle-pull:** do **not** rely on `:18083` / `:18443` for `/api/fuzz/pool/settle/outbox` — nginx returns **403** (loopback-only). Use VPS loopback `http://127.0.0.1:18081` or desktop SSH tunnel (`bash scripts/ops/install_coord_admin_tunnel_user.sh`).

## Worker config (no binary rebuild)

```bash
# Current working path (cleartext — documented honestly):
export COORD_URL=http://132.243.112.100:18083
export HACKME_POOL_COORDINATOR_URL=http://132.243.112.100:18083
export HACKME_POOL_DIRECT=1
export HACKME_DESKTOP_GPU_POOL=1
export HACKME_WORKER_BATCH_SIZE=16777216   # fewer RTTs per attempt
export HACKME_WORKER_CLAIM_COOLDOWN_MS=100 # avoid 429 bans on fast GPUs
# After pool-direct.hackme.tech has a public CA cert:
# export COORD_URL=https://pool-direct.hackme.tech:18443
```

Or flags:

```bash
bin/workerpoh-cuda -coord http://132.243.112.100:18083 -batch 16777216 ...
```

**Defaults (fleet templates):** `worker_autostart.sh` rewrites public CF → direct when `HACKME_DESKTOP_GPU_POOL=1` (or always when `HACKME_POOL_DIRECT=1`); CUDA/desktop GPU batch defaults to 16M; claim cooldown floors 0→100. Rig profiles bake 16M + 100ms for RTX 30/40/50, RDNA2/3, Hopper. Casual miners without those flags keep HTTPS via Cloudflare.

**Hybrid fuzz (fleet default ON):** release miners dig pool fuzz under the same `WORKER_ID` (`HACKME_WORKER_HYBRID_FUZZ` defaults on, mode=`inline`). Escape hatch `=0`. Process mode / dedicated dig needs `bin/workerfuzz` (linux + windows in the release bundle). Pure fuzz diggers (`bootstrap-fuzz-*`, `vps-canary-fuzz-*`) correctly show **0 GH** in the PoH workers table — they claim `/api/fuzz/work/*`, not PoH hashrate.

## Origin :443 (site) vs pool-direct

- Public site/API on **:443** should be reached via Cloudflare. Optional nginx guard: `scripts/ops/apply_cloudflare_origin_guard.sh` (CF IP allowlist via `$realip_remote_addr`). **Do not** enable `APPLY_VHOSTS=1` until verified on a canary — a 2026-10-26 attempt using `$remote_addr` after `real_ip` falsely 403'd Cloudflare and was rolled back. Do **not** put that guard on `:18083` / `:18443`.
- Pool-direct ports stay open for miners by design (cleartext + TLS).

### Orders settle-pull + Hunt harness (CDN body hang)

Cloudflare / edge often **stalls** request/response bodies ≳ ~24KiB on
`/api/fuzz/pool/hunt/harness` and can starve settle outbox pulls when Orders is
pointed at a CF-fronted URL (or a local proxy that only forwards to CF).

For **desktop Orders** (publish token + settle-pull + harness sync), prefer:

```bash
export HACKME_POOL_COORDINATOR_URL=https://hackme.tech/pool/coordinator
export HACKME_POOL_ORDERS_PUBLISH_TOKEN=…   # pool.orders.token
export HACKME_POOL_ORIGIN_DIAL=132.243.112.100   # dial origin IP, keep Host/SNI=hackme.tech
# optional exact-host override (default hackme.tech; no subdomain suffix match):
# export HACKME_POOL_ORIGIN_DIAL_HOST=hackme.tech
# export HACKME_POOL_SYNC_TIMEOUT_SEC=180
```

`HACKME_POOL_ORIGIN_DIAL` is honored only by `netutil.HTTPClientPoolOriginDial`
(Orders poolsync + Hunt harness fetch). Shared `HTTPClientNoRedirect` is unchanged.
TLS verification stays on (SNI = URL hostname). Do **not** use miner-only `:18083`
for settle outbox — nginx returns **403** on admin settle routes (see table above).

Dedicated Hunt fleets need coordinator env when GHS priority is on, e.g.
`HACKME_FUZZ_CLAIM_GHS_EXEMPT_PREFIXES=bootstrap-fuzz-,bootstrap-dig-,worker-hunt-`
so dig-only ASAN workers are not starved by `ghs_priority_defer`.

## Templates / release

Examples ship the bake-in values: `.env.desktop.example`, `scripts/ops/desktop_worker.env.example`, Vast pack, ISO miner firstboot / `run-miner-worker.sh`, Windows `write_hackme_env.ps1`. Reinstall or restart worker after pulling so env picks up the new defaults.

## Change log (ops)

- **2026-10-26:** Added TLS listener `:18443` + shared location snippet; docs no longer describe admin-token auth for miner paths; CF-only `:443` helper script added but **not left enabled** (real_ip/geo bug → rolled back). Fleet left on cleartext `:18083` until staged TLS trust.
- **2026-10-26 (fix):** IP HTTPS `:18443` is unusable for stock Go clients (no IP SAN; leaf SANs are `hackme.tech` / `www` / `explorer` only); cleartext `:18083` is the supported direct path until `pool-direct.hackme.tech` has Let's Encrypt.

## Remaining ops (LE / DNS)

1. Cloudflare DNS: create **DNS-only** (grey-cloud) `A` `pool-direct.hackme.tech` → `132.243.112.100` (orange-cloud defeats the bypass).
2. On hub: issue LE cert for that name (certbot/nginx or existing ACME) and point `deploy/nginx/hackme-pool-direct-tls.conf` at the new leaf (include DNS SAN `pool-direct.hackme.tech`).
3. Reload nginx; verify `curl -fsS https://pool-direct.hackme.tech:18443/health`.
4. Stage fleet `COORD_URL=https://pool-direct.hackme.tech:18443` only after trust works on a canary worker — keep bulk fleet on `:18083` until then.
