# HMC (HackMe Coin) — tokenomics

**Ticker:** HMC · **Chain:** native HackMe PoH ledger · **Address format:** `HMC-` + 16 hex (Ed25519) · **Decimals:** 8 (units)

Public site: [coin-hmc.html](https://hackme.tech/coin-hmc.html) · Live API: `GET https://hackme.tech/api/status` → `economics`

> **Economics v2 soft announce (2026-10-30):** live chain still uses the schedule below until a published activation height. Planned v2 changes (0.1 HMC/block, ~4y halvings, no hard max-supply fiction, pool from emission) are in [RFC_ECONOMICS_V2.md](RFC_ECONOMICS_V2.md) — **not activated yet**.

## Supply (live / v1 until activation)

| Parameter | Value | Source |
|-----------|-------|--------|
| **Max supply (live narrative)** | **100,000,000 HMC** | `internal/chain/economics.go` · locked tests · **v2 RFC removes hard-cap marketing at activation** |
| **Genesis mint** | **50,000 HMC** | One-time treasury credit at block #0 |
| **Genesis recipient** | `HMC-719006d93916ad52` (DevFee / ops treasury) | On-chain; explorer-verifiable |
| **Circulating** | `total_minted - burned` | `GET /api/status` -> `economics.circulating_hmc` |
| **Unit scale** | 1 HMC = 10^8 units | Same as SUP/HMS transfers |

There is **no** separate pre-mine bucket beyond the disclosed genesis treasury. Remaining supply enters via **PoH block rewards** and **order escrow** (prepaid by customers, not inflation).

## Emission schedule (base PoH blocks) — live

| Parameter | Live (v1) | v2 (RFC, not live) |
|-----------|-----------|---------------------|
| Initial base reward | **0.01 HMC** / empty PoH block | **0.1 HMC** |
| Halving interval | **2,102,400** (~2y @ 30s) | **4,204,800** (~4y) |
| Tail floor | **0.002 HMC** | **0.002 HMC** forever |
| Target block time | ~30 seconds | unchanged |
| Activation | — | announced height ≥ tip + 14d |

Formula: `reward_base(height) = max(tail_floor, initial_base / 2^epoch)` where `epoch = floor((height-1) / halving_interval)`.

**Order-linked blocks** do not mint free inflation — reward comes from **task escrow** prepaid by the order creator.

## Fee & burn mechanics

| Flow | Rate | Destination |
|------|------|-------------|
| Transfer fee split | 70% dev / 30% burn tally | `NetworkFeeDevShare` / `NetworkFeeBurnShare` |
| Order platform fee | 5% of prepaid | DevFee treasury |
| Order burn tally | 10% of prepaid (metadata) | Supply burn accounting at order open |

## Utility (why HMC is needed)

1. **Mining settlement** — coordinator accrual settles to on-chain HMC.
2. **B2B security audits** — customers escrow HMC for WASM fuzz / phasing campaigns.
3. **Network fees** — transfers and order creation debit HMC.
4. **Ecosystem rail** — SUP accrual is tied to honest HMC pool work; HMS market orders prepaid in HMC.

## Transparency endpoints

| Endpoint | Fields |
|----------|--------|
| `GET /api/status` | `economics.*`, `policy_hash`, tip height |
| `GET /api/metrics` | Window emission, burn impact (operator node) |
| `GET /api/address/HMC-…` | Balance, nonce |
| Explorer | https://hackme.tech/explorer-lite.html |

## Allocation summary

See [TOKEN_ALLOCATION_AND_VESTING.md](TOKEN_ALLOCATION_AND_VESTING.md) for the full cap table. HMC is **~99.95% miner + customer-driven emission** after genesis treasury.

## Listing documents

- One-pager: [EXCHANGE_LISTING_MEMO.md](internal/EXCHANGE_LISTING_MEMO.md) (HMC section)
- Technical pack: [EXCHANGE_LISTING_WALLET_PREP.md](internal/EXCHANGE_LISTING_WALLET_PREP.md)
- PDF export: [DOCUMENTATION_EXPORT.md](internal/DOCUMENTATION_EXPORT.md)
