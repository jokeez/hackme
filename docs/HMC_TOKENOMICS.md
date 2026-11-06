# HMC (HackMe Coin) — tokenomics

**Ticker:** HMC · **Chain:** native HackMe PoH ledger · **Address format:** `HMC-` + 64 hex from block 550,000 (full SHA-256 of the Ed25519 key); legacy `HMC-` + 16 hex stays valid, bound to its key · **Decimals:** 8 (units)

Public site: [coin-hmc.html](https://hackme.tech/coin-hmc.html) · Live API: `GET https://hackme.tech/api/status` → `economics`

> **Economics v2 activates at block 550,000** (≈ 2026-11-15; compiled into 0.1.0-rc19): 0.1 HMC/block, ~4-year halvings counted from genesis, permanent 0.002 HMC tail, no hard cap, real order burns, SUP block emission, long addresses and `chain_id` `hackme-mainnet-1`. Full text: [RFC_ECONOMICS_V2.md](RFC_ECONOMICS_V2.md). Tables below show v1 (until #549,999) and v2 (from #550,000).

## Supply

| Parameter | Value | Source |
|-----------|-------|--------|
| **Max supply** | v1: 100,000,000 HMC ceiling · **v2: no hard cap** (emission falls to a permanent 0.002 HMC/block tail; `max_supply_hmc` reports 0) | `internal/chain/economics.go` · `economics_v2.go` |
| **Genesis mint** | **50,000 HMC** | One-time treasury credit at block #0 |
| **Genesis recipient** | `HMC-719006d93916ad52` (DevFee / ops treasury) | On-chain; explorer-verifiable |
| **Circulating** | `total_minted - burned` | `GET /api/status` -> `economics.circulating_hmc` |
| **Unit scale** | 1 HMC = 10^8 units | Same as SUP/HMS transfers |

There is **no** separate pre-mine bucket beyond the disclosed genesis treasury. Remaining supply enters via **PoH block rewards** and **order escrow** (prepaid by customers, not inflation).

## Emission schedule (base PoH blocks)

| Parameter | v1 (until #549,999) | v2 (from #550,000) |
|-----------|-----------|---------------------|
| Initial base reward | **0.01 HMC** / empty PoH block | **0.1 HMC** |
| Halving interval | **2,102,400** (~2y @ 30s) | **4,204,800** (~4y) |
| Tail floor | **0.002 HMC** | **0.002 HMC** forever |
| Target block time | ~30 seconds | unchanged |
| Activation | genesis | block **550,000** (published with 0.1.0-rc19) |

Formula: `reward_base(height) = max(tail_floor, initial_base / 2^epoch)` where `epoch = floor((height-1) / halving_interval)`. v2 epochs count from genesis: block 550,000 is in epoch 0 (0.1 HMC); the first v2 halving is at block 4,204,801.

**Order-linked blocks** do not mint free inflation — reward comes from **task escrow** prepaid by the order creator.

## Fee & burn mechanics

| Flow | Rate | Destination |
|------|------|-------------|
| Transfer fee split | 70% dev / 30% burn tally | `NetworkFeeDevShare` / `NetworkFeeBurnShare` |
| Order platform fee | 5% of prepaid (v1 and v2) | DevFee treasury |
| Order burn — v1 | 10% of prepaid, accounting tally only | Supply burn accounting at order open |
| Order burn — v2 | **5% of prepaid, really debited** on top of escrow; 2.5% after the first halving, 1.25% from the second on | Burned (removed from circulation) |
| Order fee asset — v2 | HMC or SUP (escrow itself always HMC) | `fee_asset` on the order |

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
