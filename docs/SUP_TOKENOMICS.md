# SUP (HackMe Support) — tokenomics

**Ticker:** SUP · **Ledger:** parallel on HackMe node (same `HMC-` addresses) · **Decimals:** 8 · **Max supply (today):** **21,000,000 SUP** · **Economics v2 (at activation):** **21,000 SUP** hard cap + block emit (see [RFC_ECONOMICS_V2.md](RFC_ECONOMICS_V2.md))

Public site: [coin-sup.html](https://hackme.tech/coin-sup.html) · Live API: `GET https://hackme.tech/api/sup/economics`

**Listing policy:** CEX/DEX outreach **after primary HMC lists** — companion ticker, not a standalone airdrop narrative.

## Supply

| Parameter | Value |
|-----------|-------|
| Max supply (pre-v2) | **21,000,000 SUP** |
| Max supply (v2, when activated) | **21,000 SUP** |
| Genesis handout to public | **None** (no faucet / airdrop) |
| Mint / settle path (today) | Coordinator accrual → settlement mint / transfer |
| Mint path (v2) | Empty-block emit only; admin mint retired; pool pays from settlement wallet |
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
| Order fee payable in SUP (escrow stays HMC) | **Code on `main`** — activates with Economics v2 height (soft-announced; not live tip yet) |
| On-chain transfers | Live (`transfer_sup` / settle-by-transfer path for v2) |
| ~~Security Audit discount (15% escrow)~~ / ~~pool priority~~ | Not enforced; dropped in v2 |
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
