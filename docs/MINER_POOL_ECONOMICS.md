# Miner pool economics (public pool)

English guide for anyone joining **https://hackme.tech** as a pool worker.

## Three layers (do not confuse them)

| Layer | What you earn | When it appears |
|--------|----------------|-----------------|
| **Coordinator** | Off-chain accrual for **verified Blake3 shares** (share-PPS) | Dashboard **Unpaid worker accrual** / coordinator stats |
| **Settlement** | On-chain HMC to your `HMC-…` address | After operator Go settler `workersettle` / `settle_worker_payouts` (timer ~2 min); ledger is SQLite **uint64 units** (1 HMC = 10⁸) |
| **Chain block subsidy** | PoH block reward | **Primary wallet of the block-producing node** — not auto-split to every GPU |

Dig/Hunt escrow (customer orders) is a **separate** rail: PayRun / bounty from campaign escrow, not the Blake3 share-PPS formula below.

## Public pool payout (live — share-PPS)

Public pool default is **`SHARE_STRICT=1`** (rc18+): pay **verified** `share_nonces` only. Soft attempt-pay is historical / lab.

```
share_mod ≈ target_mod / share_divisor   (default divisor 1024)
instant_price_units = HMCToUnits(base_reward_hmc × share_mod / target_mod)
pay_price_units     = EMA(instant_price_units)   # HACKME_COORDINATOR_SHARE_PRICE_EMA=1 (default)
payout_units        = accepted_shares × pay_price_units
+ found_bonus_units (extra when you submit a valid found hit)
```

- Client-reported `hashrate_gh_s` / raw **attempts do not pay** under share-PPS.
- Pool GH/s for load comes from **shares / wall**, not fantasy client GH/s.
- **Hybrid signer** must be valid when strict mode is on.
- **Max 3 active leases** per worker id (fair share for home rigs).
- Live knobs (operator): `HACKME_COORDINATOR_SHARE_STRICT=1`, `HACKME_COORDINATOR_SHARE_PRICE_EMA=1`, `HACKME_COORDINATOR_SHARE_PRICE_EMA_ALPHA=0.25`, `REWARD_AUTO=0`.
- Share-price samples land in coordinator `share_price_journal` and settlement `settle_ledger` (units).
- Pool **Difficulty (M)** defaults to solvency scale `Hs × block_sec` (pay-per-hash = base/M). Daily budget (~28.8 HMC) is a backstop. Lab-only: `HACKME_COORDINATOR_POOL_LOAD_SOLVENCY=0` uses GH×floor. Share starve-guard keeps ~32 shares/lease when M/div ≫ batch.

## Worker configuration

1. **`WORKER_PAYOUT_MAP`** — `your-worker-id=HMC-your-address` (must match operator map for settlement), **or** hybrid-signed `payout_address` on submits.
2. **`HACKME_PUBLIC_AUTHORITY_BASE=https://hackme.tech`**
3. Verify downloads SHA256 on **https://hackme.tech/downloads.html**

## What to expect on the dashboard

- **Unpaid worker accrual** grows when the coordinator **accepts verified shares** (and rare found bonuses).
- **Wallet balance** on a follower node may show canonical overlay; settlement credits your mapped address on-chain.
- Low share % with many VPS workers is normal; accrual follows **your** accepted shares.

## Security vs older modes

| Mode | Miner UX | Status |
|------|----------|--------|
| **`SHARE_STRICT=1` (share-PPS)** | Steady accrual for proven shares | **Live public default** |
| Soft attempt-pay (`SHARE_STRICT` off) | Accrual from reported attempts | Historical / lab only |
| `payout_found_only=1` | Accrual almost only on rare found hits | Optional / high-friction |

Official public pool uses **share-PPS** unless News announces otherwise.

## Official links

- Economics page: https://hackme.tech/economics-model.html  
- Source: https://github.com/jokeez/hackme  
- Support: https://hackme.tech/contacts.html  
