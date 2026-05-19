# MiningPoolStats listing checklist — HackMe (HMC)

Use this **after** public pool + settlement are stable (no recurring `admin authentication required` in settlement logs).

## Pool facts (fill for submission)

| Field | Value |
|-------|--------|
| Coin name | HackMe |
| Symbol | HMC |
| Algorithm | PoH / WASM useful-PoW (see site) |
| Pool URL | https://hackme.tech |
| Stats / API | https://hackme.tech/pool/coordinator (public metrics); explorer: https://hackme.tech/pool/explorer |
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

## Not a substitute for

- CEX listing (separate process, compliance + liquidity).
- CoinGecko/CoinMarketCap (need broader market data and independent nodes).

See also [MINER_POOL_ECONOMICS.md](MINER_POOL_ECONOMICS.md).
