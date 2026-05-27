# HackMe Support (SUP) — utility & emission spec

**Status:** prelaunch (announced 2026-05-27). On-chain genesis and trading pairs follow dedicated rollout; not a free airdrop coin.

## Positioning

| | HMC (primary) | SUP (support) |
|---|---------------|---------------|
| Role | Block reward, pool settlement, transfers | Loyalty, network health, listing-grade alt |
| Earn | Useful-PoW / coordinator attempts | **Only** while honestly mining HMC on official pool |
| Supply | Chain emission (halving + tail) | **Separate** capped supply, own halving |
| Easy to farm? | Work-based | **No** — quality gates + streaks + caps |

SUP is designed for **exchange listing**: fixed policy, on-chain transfers, public explorer fields, no “click to claim” faucet.

## Why it is hard to earn

1. **No faucet / no airdrop** — zero genesis handout to random wallets.
2. **Pool-only accrual** — SUP is minted into accrual only when:
   - Worker uses **hybrid-signed** submits on the official coordinator
   - Payout address is bound and not banned
3. **Base rate cap** — SUP per attempt is **≤ 10%** of the HMC attempt accrual equivalent (operator-tunable, default 8%).
4. **Quality multiplier** (0×–1.25×):
   - `0` if any bad strike / ban in rolling 24h
   - `0.25` if stale/unknown submit rate &gt; 5%
   - `1.0` baseline for clean signed work
   - `+0.25` only after **72h continuous** clean mining (same worker_id + payout)
5. **Found-hit rule** — SUP does **not** copy HMC found bonus; optional tiny SUP for verified useful finds only (operator policy).
6. **Per-epoch fleet cap** — global SUP accrual per day capped (% of daily HMC pool payout) so bot farms cannot drain.
7. **Settlement** — SUP accrues off-chain like HMC until `min_settle_sup` and settlement script; separate state file.

## Utility (why hold / trade)

1. **Security Audit discount** — pay up to 15% of order escrow in SUP (rest HMC).
2. **Pool priority** — higher lease weight in congested periods for wallets with SUP balance ≥ threshold.
3. **Reputation badge** — public “Support miner” tier on explorer / dashboard (top SUP earners, anti-abuse reviewed).
4. **Governance signal** — non-binding votes on coordinator params (reward_per_m, settle thresholds).
5. **Lane access** — early ORD / Alpha testnet slots for SUP holders (planned).

## Listing narrative

- Ticker: **SUP**
- Pair readiness: on-chain `transfer_v1` same as HMC
- Transparency: emission formula published on [hackme.tech/coins.html](https://hackme.tech/coins.html)
- Companion to HMC on aggregators (MiningPoolStats: secondary ticker under same project)

## Implementation phases

| Phase | Deliverable |
|-------|-------------|
| A (now) | Public spec, site Coins page, wallet hub placeholder, Telegram |
| B | Coordinator dual accrual fields + dashboard “HMC + SUP” |
| C | SUP chain genesis or shared-ledger asset id + settlement |
| D | CEX/DEX outreach with supply proof + 30d emission chart |

## Honest limits

- SUP is **not** backed by external revenue until B2B audit volume grows.
- Trading price is market-driven; protocol does not promise USD peg.
- ASIC-only farms without hybrid signed work earn **zero** SUP.
