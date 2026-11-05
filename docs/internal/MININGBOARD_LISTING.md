# MiningBoard — HackMe Official Pool listing

Submit HackMe (HMC) to [MiningBoard](https://miningboard.com/en/pools) pool directory.  
**Status:** **LISTED** (MiningBoard email 2026-11-04 UTC) — HMC + HackMe Official Pool.  
Live pages: https://miningboard.com/pools/hackme.tech · https://miningboard.com/mining/hackme  
Site badge/links: https://hackme.tech/contacts.html (self-hosted brand-kit SVGs under `web/site/assets/partners/`).

Related: MiningPoolStats listing was removed (directory **defunct**; do not cite as live proof).

## Why the first attempt likely failed

MiningBoard’s validator expects [pool-v1 JSON](https://miningboard.com/en/pools/submit):

```json
{ "coin": "HMC", "pool": { "hashrate": <raw H/s>, "miners": <n> } }
```

Our public `/pool/coordinator/api/pool/stats` is a different shape (`pool` is a **string** name, not an object). Use the dedicated feed instead.

## Where to submit (2 paths — do both)

| Path | URL / address | When |
|------|----------------|------|
| **A. Web form** | https://miningboard.com/en/pools/submit | Primary — **Validate** feed first |
| **B. Email** | **contact@miningboard.com** | Same day after form (or if silent 3–5 days) |

Contact page: https://miningboard.com/contact — data corrections & partnerships, response ~2–3 business days.

## Before you submit

```bash
PUBLIC_BASE=https://hackme.tech bash scripts/ops/miningboard_listing_preflight.sh
curl -fsS https://hackme.tech/pool/coordinator/api/miningboard/hmc | python3 -m json.tool
```

On https://miningboard.com/en/pools/submit paste **Per-coin stats URL**:

`https://hackme.tech/pool/coordinator/api/miningboard/hmc`

→ click **Validate** → only then Submit.

Checklist:

- [ ] `GET …/api/miningboard/hmc` → has `coin`, `pool.hashrate`, `pool.miners`
- [ ] Validator on MiningBoard submit page passes
- [ ] https://hackme.tech/downloads.html — SHA256SUMS for current release
- [ ] Follow-up email to contact@miningboard.com (template below)

## Pool facts (canonical)

| Field | Value |
|-------|--------|
| Pool name | **HackMe Official Pool** |
| Coin name | HackMe |
| Ticker | **HMC** |
| Pool website | https://hackme.tech |
| Coordinator base | https://hackme.tech/pool/coordinator |
| **MiningBoard feed (pool-v1)** | https://hackme.tech/pool/coordinator/api/miningboard/hmc |
| **Legacy pool stats** | https://hackme.tech/pool/coordinator/api/pool/stats |
| **Detailed work API** | https://hackme.tech/pool/coordinator/api/work/stats |
| Explorer | https://hackme.tech/pool/explorer |
| Downloads | https://hackme.tech/downloads.html |
| Algorithm | Blake3 PoH (SHARE_STRICT PPS) + Dig/Hunt fuzz lanes (useful customer work) — GPU: CUDA/OpenCL **workerpoh** — **not** SHA256/Scrypt |
| Connection | **HTTP coordinator + worker** — **NOT Stratum TCP** |
| Pool fee | **0%** (operator-funded settlement; no pool skim on shares) |
| Min payout | **0.0001 HMC** accrual threshold; on-chain settlement ~every 90s when treasury funded |
| Payout scheme | Off-chain accrual → on-chain `transfer_v1` to miner `HMC-…` address |
| Region | EU (primary hub VPS) + distributed workers |
| Software | Open source — https://github.com/jokeez/hackme (AGPL-3.0) |
| ANN | https://bitcointalk.org/index.php?topic=5583373.0 |
| Pool proof | https://hackme.tech/pool/coordinator/api/pool/stats |
| Contact | support@hackme.tech · https://hackme.tech/contacts.html |

### Important note for moderators (paste in “comments” / email)

```
HackMe is NOT a Stratum pool. Miners run hackme-node + workerpoh and talk to an HTTP
coordinator at /pool/coordinator. Public JSON stats are at /pool/coordinator/api/pool/stats
and /pool/coordinator/api/work/stats (hashrate_gh_s per worker). Please list as
"Custom / HTTP coordinator" — algorithm is Blake3 share-PPS PoH + Dig/Hunt fuzz (useful on fuzz only) (not SHA256/Scrypt).
```

## Suggested form fields (Submit a pool)

Use whatever the form exposes; map our values:

| Form field | Value |
|------------|--------|
| Pool name | HackMe Official Pool |
| Pool URL | https://hackme.tech |
| Coin / ticker | HMC (HackMe) |
| Algorithm | Other / Custom — *Blake3 PoH share-PPS (GPU) + Dig/Hunt useful fuzz* |
| Fee % | 0 |
| Payout | Other / Custom (HTTP accrual + on-chain settlement) |
| Min payout | 0.0001 HMC |
| Per-coin stats URL | `https://hackme.tech/pool/coordinator/api/miningboard/hmc` |
| Index URL | *(leave blank)* |
| Pool software | Custom / Other |
| Stratum endpoints | **Leave empty** — *N/A — no Stratum* |
| Logo URL | `https://hackme.tech/assets/logo-hex.png` |
| Proof links | GitHub + Bitcointalk ANN + MiningBoard feed |

## Email — follow-up / resubmit (copy & send)

**To:** contact@miningboard.com  
**Subject:** Follow-up — HackMe Official Pool (HMC) — MiningBoard pool-v1 feed ready

**Body (English):**

```
Hello MiningBoard team,

Following up on our earlier listing request for HackMe Official Pool (HMC).

We now publish a MiningBoard pool-v1 compatible feed (please Validate on your submit page):

  https://hackme.tech/pool/coordinator/api/miningboard/hmc

Example shape:
  {
    "spec": "miningboard-pool-v1",
    "coin": "HMC",
    "pool": { "hashrate": <raw H/s>, "miners": <n>, "fee_percent": 0, "payout_scheme": "CUSTOM", "min_payout": 0.0001 },
    "network": { "hashrate": <raw H/s>, "height": <tip> }
  }

Pool name:  HackMe Official Pool
Website:    https://hackme.tech
Explorer:   https://hackme.tech/pool/explorer
Downloads:  https://hackme.tech/downloads.html
GitHub:     https://github.com/jokeez/hackme
ANN:        https://bitcointalk.org/index.php?topic=5583373.0

Important:
- NOT a Stratum pool (HTTP coordinator + open-source workerpoh).
- HMC may be a new coin on your directory — happy with a directory listing
  and/or adding coin HMC as Custom (Blake3 PoH + Dig/Hunt fuzz).
- Fee 0%. No Stratum endpoints to publish.

If the first submission was rejected for schema reasons, please re-check the new feed.
Contact: support@hackme.tech

Thank you,
HackMe Network
```

## After approval

- [x] Add MiningBoard link + self-hosted badge to https://hackme.tech/contacts.html
- [x] News item + social copy (`web/site/assets/news.json` id `2026-11-05-miningboard-listed`)
- [ ] Post Telegram / X / Discord / Facebook / Reddit / Bitcointalk ANN bump (copy in PR / chat)
- Keep stats API uptime — MiningBoard aggregates from pool APIs (`/api/miningboard/hmc`).

## Not a substitute for

- Exchange listing — see [EXCHANGE_LISTING_MEMO.md](EXCHANGE_LISTING_MEMO.md) / [LISTING_PITCH_OUTLINE.md](LISTING_PITCH_OUTLINE.md).
- Defunct aggregators (e.g. MiningPoolStats) — do not cite as live.
