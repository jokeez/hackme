# Miner pool economics (public pool)

English guide for anyone joining **https://hackme.tech** as a pool worker.

## Three layers (do not confuse them)

| Layer | What you earn | When it appears |
|--------|----------------|-----------------|
| **Coordinator** | Off-chain `payout_hmc` for **accepted work** | Dashboard **Unpaid worker accrual** / coordinator stats |
| **Settlement** | On-chain HMC to your `HMC-…` address | After operator `settle_worker_payouts` (timer ~2 min) |
| **Chain block subsidy** | PoH block reward | **Primary wallet of the block-producing node** — not auto-split to every GPU |

## Public pool payout formula (default policy)

When `payout_found_only=0` and `reward_auto=1` (fair public pool):

```
payout_per_submit ≈ (attempts / 1_000_000) × reward_per_m
reward_per_m ≈ base_reward_hmc × 1_000_000 / target_mod   (from canonical /api/metrics)
+ found_bonus_hmc   (extra when you submit a valid found hit)
```

- **attempts** are capped by your lease **batch size** (anti-abuse).
- **Hybrid signer** must be valid when strict mode is on.
- **Max 3 active leases** per worker id (fair share for home rigs).

## What you must configure

1. **`WORKER_PAYOUT_MAP`** — `your-worker-id=HMC-your-address` (must match operator map for settlement).
2. **`HACKME_PUBLIC_AUTHORITY_BASE=https://hackme.tech`**
3. Verify downloads SHA256 on **https://hackme.tech/downloads.html**

## What to expect on the dashboard

- **Unpaid worker accrual** grows while you submit accepted batches (not only on block finds).
- **Wallet balance** on a follower node may show canonical overlay; settlement credits your mapped address on-chain.
- Low share % with many VPS workers is normal; accrual follows **your** accepted attempts.

## Security vs “found-only” mode

| Mode | Miner UX | Operator risk |
|------|----------|----------------|
| `payout_found_only=1` | Accrual almost only on rare found hits | Lowest fake-attempt payout risk |
| `payout_found_only=0` + hybrid strict + lease caps | **Recommended for public pool** — steady accrual | Mitigated by signing + batch caps + monitoring |

Official public pool uses **fair attempt-based** accrual unless announced otherwise on News.

## Official links

- Economics page: https://hackme.tech/economics-model.html  
- Source: https://github.com/jokeez/hackme  
- Support: https://hackme.tech/contacts.html  
