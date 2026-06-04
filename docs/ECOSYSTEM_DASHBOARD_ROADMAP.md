# Ecosystem dashboard roadmap (multi-coin vision)

HackMe already has an **Ecosystem** tab in `dashboard.html` (HMC live; Alpha / Orders / Shard as placeholders). This doc sequences work toward a real multi-pool UI without breaking rc11l production.

## Phase 1 — HMC pool transparency (now)

| Item | Status |
|------|--------|
| Pool miners table + live hashrate | Done |
| **Find miner by wallet** (`HMC-…`) | Done — Overview search + `GET /api/work/by-wallet?address=` |
| Payout address column in miners table | Done |
| Public site pool strip | Done (`web/site`) |
| Nginx ISO/downloads interest | Done (`nginx_downloads_interest.sh`) |

## Phase 2 — Unified operator dashboard

- **Wallet hub (UI v1 in `dashboard.html`)**: exchange-style sidebar (coin icons + balances), portfolio header, per-coin planned cards, live HMC with Overview / Earnings / Transfer sub-tabs. Registry = `ECOSYSTEM_COINS` (+ planned **SUP** bonus lane).
- **Global header**: pick coin → coordinator URL + explorer + downloads deep link.
- **Wallet hub (API next)**: one search box → pool workers + chain balance + settlement history + explorer txs.
- **Fleet view**: map `worker_id` ↔ `payout_address` ↔ rig label ↔ last seen (persist labels in localStorage).
- **Alerts**: worker offline >5 min, hashrate drop >30%, settlement lag.

## Phase 3 — Multi-coin registry (config-driven)

`ecosystem.json` (no hardcoded HTML table):

```json
{
  "coins": [
    {
      "id": "hmc",
      "name": "HackMe Coin",
      "status": "live",
      "coordinator_url": "https://hackme.tech/pool/coordinator",
      "explorer_url": "https://hackme.tech/pool/explorer",
      "downloads_anchor": "#hackme-os"
    }
  ]
}
```

Dashboard loads registry → renders cards + routes API calls per coin.

## Phase 4 — Shard / second pool

- Separate coordinator process or `chain_id` partition.
- Cross-pool search: `GET /api/ecosystem/wallet/{HMC-…}` aggregates all coordinators in registry.
- MiningPoolStats / Coinzilla widgets per coin.

## Phase 5 — Orders lane + Alpha testnet

- Orders tab tied to live `orders_active` from coordinator.
- Alpha coin: testnet coordinator URL + faucet + distinct worker prefix.

## APIs to add later

| Endpoint | Purpose |
|----------|---------|
| `GET /api/ecosystem/registry` | Coin list + URLs |
| `GET /api/ecosystem/wallet/{addr}` | All pools paying to wallet |
| `GET /api/workers/search?q=` | Fuzzy worker id / wallet / IP |

## What not to do yet

- Do not fake multi-coin balances in UI.
- Do not expose admin tokens in public registry.
- Keep `top_miners` labeled as “pool leaderboard (worker-derived)” unless backed by real payout addresses.
