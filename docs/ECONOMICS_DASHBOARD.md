# Economics Formula and Dashboard

This document fixes the public economics model used by the node and dashboard API.

**Regression lock:** Go tests in `internal/chain/economics_test.go` pin `lockedPolicyHash`, halving schedule, transfer fee shares, and fairness bounds; difficulty / retarget micro-steps are in `internal/chain/retarget_test.go`.

## 1) Public income formula

Total miner income is split into three parts:

- Empty mining (no paid order on solved block):
  - `reward_base(height) = max(tail_floor, initial_base / 2^epoch)`
  - `epoch = floor((block_index-1) / halving_interval_blocks)`
- Order mining (paid task solved):
  - `reward_order = manifest.reward_hmc` (validated by fairness guard at task creation)
- Coordinator payout (external pool logic):
  - `reward_pool = attempts_share + found_bonus` (configured by coordinator policy)

Where current chain constants are:

- `initial_base = 0.01 HMC`
- `halving_interval_blocks = 2,102,400` (~2 years at 30 sec target block time)
- `tail_floor = 0.002 HMC` (`RewardTailFloorHMC`)

## 2) Emission model selected now

Implemented model: **halving with tail emission floor**.

- Before floor is reached: base reward halves every epoch.
- After floor is reached: reward stays at tail floor.
- This keeps long-term miner incentive alive while preventing emission from collapsing to zero.

## 3) Dashboard fields (API `/api/metrics`)

Key economic fields now exposed:

- Schedule:
  - `econ_base_reward_now_hmc`
  - `econ_reward_halving_interval_blocks`
  - `econ_next_halving_block`
  - `econ_reward_tail_floor_hmc`
  - `econ_expected_empty_hmc_hour`
- Actual last 1h window:
  - `econ_window_blocks`
  - `econ_window_base_blocks`
  - `econ_window_order_blocks`
  - `econ_window_base_hmc`
  - `econ_window_order_hmc`
  - `econ_window_total_hmc`
  - `econ_window_order_share_pct`
- Supply / burn impact:
  - `econ_total_minted_hmc`
  - `econ_total_burned_hmc`
  - `econ_burn_impact_pct`
  - `econ_circulating_hmc`
  - `econ_mint_remaining_hmc`

## 4) Capacity intuition (network-wide)

With target 30s blocks, the network produces around `120` blocks/hour.

- Empty mining expected network emission:
  - `HMC/hour ~= reward_base(height) * 120`
- Per-miner expectation:
  - `miner_hmc_hour ~= network_hmc_hour * miner_share`
  - `miner_share ~= miner_effective_attempts / network_effective_attempts`

So thousands/millions of miners do **not** increase total emission rate; they only split the same block reward pie according to effective share.

## 5) Chain reward credit vs block metadata (operator-critical)

When `AppendPoHBlock` credits a PoH reward:

- Units are added to the node **primary wallet row** (`wallet WHERE id = 1`) and the matching `accounts` row for that address, so `/api/wallet` stays aligned with ledger state.
- The block JSON/header can still record **`minerAddress`** (signer at solve time). Credits intentionally follow **primary wallet**, not blindly the header address, so backup/rebind drift does not strand balances.

**Implication:** base or order-linked block subsidy on a given node accrues to **that node’s primary wallet**, not automatically to every GPU connected via the coordinator. Fleet rewards are expected through **coordinator accrual + settlement** (policy-dependent).

## 6) Coordinator vs classic Stratum PoW pool

HackMe’s coordinator exposes work stats, hybrid-signed submits, and payout counters (e.g. `total_payout_hmc`) that operators settle on-chain. That model is **not** identical to legacy Ethereum/Bitcoin pool semantics where each share maps trivially to block reward splits unless explicitly implemented in coordinator policy.

## 7) Orders as the primary product layer

Paid audit orders escrow rewards and enforce fairness guards at task creation. PoH blocks tied to an open order must match the task reward or the chain rejects the append — tying emission on those blocks to **order escrow**, not free-form minting.

## 8) Order creation debits vs transfer fees (do not conflate)

Implemented in `internal/chain/order_tasks.go` when creating a task:

- **Prepaid:** `prepaid = reward_hmc × target_solves` — held in task escrow for miner payouts.
- **Platform fee:** `order_fee = prepaid × OrderPlatformFeeRate` (`0.05`). Full fee amount is credited to **`DevFeeAddress`** (service/dev policy).
- **Wallet debit:** `total_debit = prepaid + order_fee` from the node primary wallet / matching account.
- **Burn accounting:** `burn = prepaid × OrderBurnRate` (`0.10`) updates aggregate burned supply metadata (`meta_total_burned_*`) at order open — this is **not** “30% of the platform fee”; it is a **separate 10% of prepaid** tally.

**Transfer (`fee_units`)** uses **`NetworkFeeBurnShare` / `NetworkFeeDevShare` (`0.30` / `0.70`)** — that split applies only to on-chain transfer fees, not to opening orders.
