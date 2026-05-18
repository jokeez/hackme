# HackMe Invariants Check (quick operator guide)

This guide provides a repeatable way to validate core math/rules after manual UI/API testing.

## 1) Base check (status + economics)

```bash
bash scripts/check_invariants.sh
```

What this validates:
- node responds on `BASE` (`http://127.0.0.1:8080` by default)
- genesis is initialized
- economics consistency:
  - `circulating_hmc == total_minted_hmc - total_burned_hmc`
  - `mint_remaining_hmc == max_supply_hmc - total_minted_hmc`
  - no negative values

## 2) Transfer check (optional, strict)

Use after sending a transfer and capturing hash/context:

```bash
BASE=http://127.0.0.1:8080 \
TX_HASH=743ffe... \
FROM=HMC-... \
TO=HMC-... \
EXPECT_AMOUNT=100000 \
EXPECT_FEE=1000 \
bash scripts/check_invariants.sh
```

What this validates (when tx is `included`):
- tx exists and payload matches expected `from/to/amount/fee`
- helps detect accidental mismatches in manual runs

If tx is still `pending`, script prints a warning and skips strict transfer checks.

## 3) Order check (optional)

Use after creating an order:

```bash
BASE=http://127.0.0.1:8080 \
ORDER_ID=order-demo-1 \
EXPECT_REWARD_HMC=0.02 \
EXPECT_TARGET_SOLVES=3 \
EXPECT_DIFFICULTY=10 \
bash scripts/check_invariants.sh
```

What this validates:
- order exists in `GET /api/tasks`
- selected fields match expected values
- progress math:
  - `0 <= progress_count <= target_solves`
  - `progress_pct ~= progress_count / target_solves * 100`
- if `progress_count >= target_solves`, status must be `completed`

## Notes

- Requires: `curl`, `jq`, `python3`.
- You can combine transfer + order env vars in one run.
- This script does not mutate state; it only reads API responses.
