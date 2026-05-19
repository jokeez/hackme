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
| **API stats URL** | `https://hackme.tech/pool/coordinator/api/work/stats` |

In pool notes: *Not Stratum — hackme-node + workerpoh, coordinator at /pool/coordinator.*

## Not a substitute for

- CEX listing (separate process, compliance + liquidity).
- CoinGecko/CoinMarketCap (need broader market data and independent nodes).

See also [MINER_POOL_ECONOMICS.md](MINER_POOL_ECONOMICS.md).
