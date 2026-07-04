# Hashrate.no — HackMe pool + HMC coin listing

Submit at:
- **Pool:** https://www.hashrate.no/submit/pool
- **Coin:** https://www.hashrate.no/submit/coin (if HMC not in dropdown → **Other**)

Related: [MININGPOOLSTATS_LISTING.md](MININGPOOLSTATS_LISTING.md) (HMC **live** on MPS)

## Preflight

```bash
PUBLIC_BASE=https://hackme.tech bash scripts/ops/mps_listing_readiness.sh
curl -fsS https://hackme.tech/pool/coordinator/api/pool/stats
curl -fsS https://hackme.tech/pool/coordinator/api/work/stats
```

---

## Pool form (https://www.hashrate.no/submit/pool)

| Field | Value |
|-------|--------|
| **Pool** | Other → **HackMe Official Pool** |
| **Coin** | Other → **HMC (HackMe)** |
| **Payout scheme** | **PPS** or **Other** — paste note below |
| **Fee** | **0** % (operator-funded; no pool skim) |
| **Dashboard URL** | `https://hackme.tech` |
| **API URL (pool hashrate)** | `https://hackme.tech/pool/coordinator/api/pool/stats` |
| **Stratum / connection** | **N/A — not Stratum** (see note) |

### Stratum rows (if required)

Hashrate.no expects `stratum+tcp` host/port. HackMe uses **HTTP coordinator + workerpoh**, not TCP Stratum.

| Region | URL (host only) | TCP port | TLS port | Notes |
|--------|---------------|----------|----------|--------|
| EU | `hackme.tech` | leave empty or `443` | `443` | Miners use HTTPS coordinator at `/pool/coordinator` |
| — | — | — | — | **Do not** list fake Stratum on 3333 |

**Paste in comments / Other:**

```
HackMe is NOT a Stratum pool. Miners run open-source hackme-node + workerpoh (CUDA/OpenCL).
Connection: HTTPS coordinator https://hackme.tech/pool/coordinator
Stats JSON: /api/pool/stats (hashrate, miners) and /api/work/stats (per-worker hashrate_gh_s).
Payout: off-chain accrual per accepted work → on-chain HMC transfer every ~90s.
Already on MiningPoolStats: https://miningpoolstats.app/coins/HMC
Source: https://github.com/jokeez/hackme (AGPL-3.0)
Contact: support@hackme.tech
```

---

## Coin form (https://www.hashrate.no/submit/coin)

| Field | Value |
|-------|--------|
| **Ticker** | `HMC` |
| **Name** | `HackMe` |
| **Algorithm** | **Other** → `Useful PoW / WASM PoH (GPU workerpoh)` |
| **Website** | `https://hackme.tech` |
| **Explorer** | `https://hackme.tech/explorer-lite.html` |
| **Block time** | `30` s |
| **Total block reward** | `0.01` HMC (base PoH block; pool pays `reward_per_m` per attempt — see work/stats API) |
| **% to miners** | `100` |
| **% to masternodes** | `0` |
| **% to developers** | `0` (genesis treasury 50k HMC disclosed separately) |
| **Exchanges** | *(none listed — testnet / early stage)* |
| **Other** | Max supply 100,000,000 HMC · halving every 2,102,400 blocks · AGPL repo |

**Algorithm note:** Not KawPoW/SHA256. GPU mining via **workerpoh** against coordinator work ranges (`eval(n) mod M = 0` WASM gates).

---

## Sample live API (for moderators)

```bash
curl -sS https://hackme.tech/pool/coordinator/api/pool/stats
# {"status":"ok","pool":"HackMe Official Pool","hashrate":2.9e10,"miners":3,"workers":3,...}

curl -sS https://hackme.tech/pool/coordinator/api/work/stats | jq '{reward_per_m,target_mod,workers_count,total_payout_hmc}'
```

---

## After submit

- Expect **manual review** (days).
- If rejected for “no Stratum”: reply with MPS link + GitHub + offer **custom/HTTP** category.
- Keep `api/pool/stats` returning `status: ok` 24/7.
