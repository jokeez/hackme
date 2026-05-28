# MiningPoolStats listing checklist — HackMe (HMC)

Use this **after** public pool + settlement are stable (no recurring `admin authentication required` in settlement logs).

## Pool facts (fill for submission)

| Field | Value |
|-------|--------|
| Coin name | HackMe |
| Symbol | HMC |
| Algorithm | PoH / WASM useful-PoW (see site) |
| Pool URL | https://hackme.tech |
| Stats / API | `https://hackme.tech/pool/coordinator/api/work/stats` (includes `hashrate`, `workers`) or `.../api/network/stats` |
| Explorer | https://hackme.tech/pool/explorer |
| Downloads | https://hackme.tech/downloads.html |
| ANN | https://bitcointalk.org/index.php?topic=5583373.0 |
| Source | https://github.com/jokeez/hackme |
| Contact | support@hackme.tech · https://hackme.tech/contacts.html |

## Before you submit

- [ ] Settlement timer active: `hackme-worker-settlement.timer`
- [ ] `bash scripts/ops/sync_settlement_admin_token.sh` on VPS (ADMIN_TOKEN = node token)
- [ ] `bash scripts/ops/settlement_healthcheck.sh` → OK
- [ ] Miners can set `WORKER_PAYOUT_MAP` / hybrid payout address and receive on-chain HMC
- [ ] Official builds SHA256 on https://hackme.tech/downloads.html
- [ ] Pool hashrate honest on stats API (NVIDIA: **CUDA** `workerpoh-cuda`; AMD/Intel: OpenCL — see [GPU_MINING_BACKENDS.md](GPU_MINING_BACKENDS.md))

## Submission

1. Register / log in at [miningpoolstats.stream](https://miningpoolstats.stream) (or current MPS portal).
2. Add pool → provide **public stats URL**, coin ticker, contact email.
3. Attach Bitcointalk ANN + GitHub for verification.
4. Expect **manual review** — new coins often wait until visible hashrate + uptime.

## Pool form (step 4) — avoids validation errors

| Field | Value |
|-------|--------|
| Pool name | HackMe Official Pool |
| Pool website | https://hackme.tech |
| **Stratum host** | `hackme.tech` only (**no** `https://`, **no** path) |
| **Stratum port** | `3333` (symbolic; miners use HTTP worker, not Stratum TCP) |
| Region | EU |
| Fee % | 0 |
| Min payout | 0.01 |
| Payment | Other / Custom (or PPS with note: accrual + settlement) |
| Pool software | Other / Custom |
| **API stats URL** | `https://hackme.tech/pool/coordinator/api/pool/stats` (minimal JSON for pollers; includes `block_height` / `tip_height` from canonical node when reachable) |
| **Payment** | **Other / Custom** — not PPS (we are not Stratum shares) |
| **Fee %** | `1` if `0` fails validation |

In pool notes: *Not Stratum — hackme-node + workerpoh, coordinator at /pool/coordinator.*

### If validation still fails on step 4

1. **Leave API Stats URL empty** and Submit (add stats later in dashboard).
2. **Stratum** — MPS may TCP-probe `host:port`; HackMe has **no Stratum on 3333**, so probe fails. Options:
   - Submit **coin only** without pools (if the form allows skipping step 4), then add pool via support.
   - Email MPS: *non-Stratum HTTP coordinator pool* + link to this repo.
3. Re-check **steps 1–3** (logo PNG, supply `100000000`, block reward `0.01`, block time `30`).

## Not a substitute for

- CEX listing (separate process, compliance + liquidity).
- CoinGecko/CoinMarketCap (need broader market data and independent nodes).

See also [MINER_POOL_ECONOMICS.md](MINER_POOL_ECONOMICS.md).

## After moderators approve (Approved)

### Your MPS dashboard

- Pool card **HackMe Official Pool** linked to your account.
- **API poll log** — green if `GET https://hackme.tech/pool/coordinator/api/pool/stats` returns `200` + JSON.
- Edit logo, fee %, links (Telegram / Discord), notes: *HTTP coordinator, not Stratum*.

### What changes on miningpoolstats.stream

- Coin page for **HMC** (if new to their DB).
- Pool row with live **hashrate**, **workers**, network context.
- Organic traffic from “new coins” — send them to calculator + downloads, not fixed $/day claims.

### Blocks (24h) vs block height on MPS

- **`GET /api/pool/stats`** exposes canonical **`block_height`** / **`tip_height`** (chain tip from linked node).
- MPS **“Blocks (24h)”** usually means **blocks found by the pool**, not network height — extra JSON fields may **not** fill that column until MPS maps them or you agree a custom note with moderators.
- After coordinator deploy, verify: `curl -fsS https://hackme.tech/pool/coordinator/api/pool/stats | jq '{block_height,tip_height,hashrate,workers}'`

### Keep hub VPS healthy (moderation ping)

```bash
PUBLIC_BASE=https://hackme.tech NODE_SSH=hackme-vps bash scripts/ops/mps_listing_readiness.sh --vps
```

Required while listed:

| Check | Command / URL |
|-------|----------------|
| Pool stats | `curl -fsS https://hackme.tech/pool/coordinator/api/pool/stats` |
| Node status | `curl -fsS https://hackme.tech/api/status` |
| Settlement | `hackme-worker-settlement.timer` active on VPS |
| Workers | PC + MSK (or more) submitting — **workers** in stats &gt; 0 |

### First miners message

Copy **[MINER_WELCOME_MPS_APPROVED.md](MINER_WELCOME_MPS_APPROVED.md)** to Telegram / Discord pin.

### Exchanges (after MPS)

See **[EXCHANGE_LISTING_ROADMAP.md](EXCHANGE_LISTING_ROADMAP.md)** — Xeggex → NonKYC → TradeOgre → CoinEx.
