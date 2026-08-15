# SUP (HackMe Support) — tokenomics

**Ticker:** SUP · **Ledger:** parallel on HackMe node (same `HMC-` addresses) · **Decimals:** 8 · **Max supply:** **21,000,000 SUP**

Public site: [coin-sup.html](https://hackme.tech/coin-sup.html) · Live API: `GET https://hackme.tech/api/sup/economics`

**Listing policy:** CEX/DEX outreach **after primary HMC lists** — companion ticker, not a standalone airdrop narrative.

## Supply

| Parameter | Value |
|-----------|-------|
| Max supply | **21,000,000 SUP** |
| Genesis handout to public | **None** (no faucet / airdrop) |
| Mint path | Coordinator accrual → `settle_worker_sup.sh` on-chain mint |
| Circulating | `total_minted_sup` from economics API |

## Emission (earn rules)

SUP is **only** minted when miners do **quality hybrid-signed work** on the official HMC coordinator.

| Gate | Effect |
|------|--------|
| Base rate | <= **8%** of HMC attempt accrual equivalent (operator-tunable, max 10%) |
| Quality multiplier | 0×–1.5× (strikes, stale rate, 72h streak bonus; hard cap 1.5×) |
| Fleet daily cap | Global SUP accrual capped vs daily HMC pool payout (~12% ratio) |
| Hybrid required | Unsigned / banned workers earn **zero** |

Full policy: [SUPPORT_COIN_UTILITY.md](SUPPORT_COIN_UTILITY.md) · Phase C on-chain: [SUP_PHASE_C.md](SUP_PHASE_C.md)

## Utility (why hold SUP)

| Utility | Status |
|---------|--------|
| Security Audit discount (up to 15% escrow in SUP) | Spec + rollout |
| Pool priority under congestion | Planned enforcement |
| Support-miner reputation tier | Explorer / dashboard |
| Governance signal (non-binding param votes) | Planned |
| Early ORD / Alpha slots | Planned |

SUP is **not** backed by external revenue until B2B audit volume grows. Price is market-driven.

## Transparency endpoints

| Endpoint | Fields |
|----------|--------|
| `GET /api/sup/economics` | `max_supply_sup`, `total_minted_sup`, `remaining_sup`, `mint_enabled`, `on_chain_settle_live` |
| `GET /api/work/stats` | `sup_policy`, `total_payout_sup` (coordinator) |
| Wallet | `balance_sup` on canonical wallet API |

## Allocation summary

| Category | % of max | Notes |
|----------|----------|-------|
| Miner emission (pool) | ~100% | No team pre-mine |
| Treasury / team | 0% | No genesis SUP to operators |
| Reserves | 0% | Emission is work-based only |

See [TOKEN_ALLOCATION_AND_VESTING.md](TOKEN_ALLOCATION_AND_VESTING.md).

## Listing timeline

1. **Now:** on-chain mint + public economics + coordinator `on_chain_settle=true`
2. **After HMC CEX:** companion listing + aggregators
3. **Phase D:** 30-day emission chart on site ([token-transparency.html](https://hackme.tech/token-transparency.html))
