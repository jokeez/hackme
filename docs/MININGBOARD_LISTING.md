# MiningBoard — HackMe Official Pool listing

Submit HackMe (HMC) to [MiningBoard](https://miningboard.com/pools) pool directory.  
**Status:** not listed yet (search `hackme` → no match, 2026-06-27).

Related: [MININGPOOLSTATS_LISTING.md](MININGPOOLSTATS_LISTING.md) (historical — MiningPoolStats is **defunct**; do not cite as live proof).

## Where to submit (2 paths — do both)

| Path | URL / address | When |
|------|----------------|------|
| **A. Web form** | https://miningboard.com/pools → scroll **“Pool not found?”** → **Submit a pool** | Primary |
| **B. Email** | **hello@miningboard.com** | Follow-up if no reply in 3–5 days; attach same text |

Contact page: https://miningboard.com/contact — data corrections & partnerships, response ~2–3 business days.

## Before you submit

```bash
PUBLIC_BASE=https://hackme.tech bash scripts/ops/miningboard_listing_preflight.sh
```

Checklist:

- [ ] `GET …/pool/coordinator/api/pool/stats` → `"status":"ok"`
- [ ] `GET …/pool/coordinator/api/work/stats` → workers online, `hashrate_gh_s` > 0
- [ ] https://hackme.tech/pool/coordinator/api/pool/stats → live hashrate / miners (MPS defunct — do not require)
- [ ] https://hackme.tech/downloads.html — SHA256SUMS for current release

## Pool facts (canonical)

| Field | Value |
|-------|--------|
| Pool name | **HackMe Official Pool** |
| Coin name | HackMe |
| Ticker | **HMC** |
| Pool website | https://hackme.tech |
| Coordinator base | https://hackme.tech/pool/coordinator |
| **Stats API (JSON)** | https://hackme.tech/pool/coordinator/api/pool/stats |
| **Detailed work API** | https://hackme.tech/pool/coordinator/api/work/stats |
| Explorer | https://hackme.tech/pool/explorer |
| Downloads | https://hackme.tech/downloads.html |
| Algorithm | Useful PoW / PoH + WASM task gates (GPU: CUDA/OpenCL **workerpoh**) — **not** SHA256/Scrypt |
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
"Custom / HTTP coordinator" — algorithm is useful-GPU-PoW (not SHA256/Scrypt).
```

## Suggested form fields (Submit a pool)

Use whatever the form exposes; map our values:

| Form field | Value |
|------------|--------|
| Pool name | HackMe Official Pool |
| Pool URL | https://hackme.tech |
| Coin / ticker | HMC (HackMe) |
| Algorithm | Other / Custom — *Useful PoW / WASM PoH (GPU)* |
| Fee % | 0 |
| Payout | Other / Custom (HTTP accrual + on-chain settlement) |
| Min payout | 0.0001 HMC |
| Stats API URL | `https://hackme.tech/pool/coordinator/api/pool/stats` |
| Stratum host:port | **Leave empty** or write in notes: *N/A — no Stratum* |
| Region | EU |
| Proof links | GitHub + Bitcointalk ANN + live pool stats API |

## Email — copy & send

**To:** hello@miningboard.com  
**Subject:** Pool listing request — HackMe Official Pool (HMC) — HTTP coordinator, live stats API

**Body (English):**

```
Hello MiningBoard team,

We would like to add our public mining pool to the MiningBoard directory.

Pool name:     HackMe Official Pool
Coin:          HackMe (HMC)
Website:       https://hackme.tech
Stats API:     https://hackme.tech/pool/coordinator/api/pool/stats
Work stats:    https://hackme.tech/pool/coordinator/api/work/stats
Explorer:      https://hackme.tech/pool/explorer
Downloads:     https://hackme.tech/downloads.html

Algorithm:     Useful PoW / Proof-of-History with WASM sandbox gates (GPU via workerpoh;
               CUDA on NVIDIA, OpenCL fallback). This is NOT SHA256/Scrypt Stratum mining.

Connection:    HTTP coordinator at https://hackme.tech/pool/coordinator — NOT Stratum TCP.
               Miners use the open-source hackme-node desktop/worker (see downloads page).

Economics:     0% pool fee. Coordinator accrues HMC off-chain per accepted work; operator
               settles to miner HMC-addresses on-chain (public chain explorer on same domain).

Proof / trust: • Open source: https://github.com/jokeez/hackme (AGPL-3.0)
               • Bitcointalk ANN: https://bitcointalk.org/index.php?topic=5583373.0
               • Live pool stats return JSON with status, hashrate, miners/workers count.

Sample pool/stats JSON (live):
  {"status":"ok","pool":"HackMe Official Pool","hashrate":<n>,"miners":<n>,"workers":<n>}

Contact: support@hackme.tech
Community: https://hackme.tech/contacts.html (Telegram, Discord, X)

Please list us as a custom / non-Stratum HTTP pool, or associate our entry with coin HMC.
Happy to provide any extra fields your template needs.

Thank you,
HackMe Network
support@hackme.tech
```

## After approval

- Add MiningBoard link to https://hackme.tech/contacts.html (optional card).
- Bump Bitcointalk ANN with “now on MiningBoard”.
- Keep stats API uptime — MiningBoard aggregates from pool APIs.

## Not a substitute for

- Exchange listing — see [EXCHANGE_LISTING_ROADMAP.md](EXCHANGE_LISTING_ROADMAP.md).
- Defunct aggregators (e.g. MiningPoolStats) — do not cite as live.
