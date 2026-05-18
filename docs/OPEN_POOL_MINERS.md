# Open pool — conditions for miners

Public coordinator: `https://hackme.tech/pool/coordinator`

## Fair pool (operator defaults on hackme.tech)

| Setting | Value | Why miners like it |
|---------|-------|-------------------|
| `HACKME_COORDINATOR_LEASE_SEC` | **90** | GPU batches finish without `lease_expired` |
| `HACKME_COORDINATOR_MAX_ACTIVE_LEASES_PER_WORKER` | **3** | No rig monopolizes all slots |
| `HACKME_COORDINATOR_CLAIM_PER_MIN` | **600** | Enough claims for many rigs |
| Canary monitor throttle | **1.5s** cooldown | Home miners get fair share |
| Settlement | timer ~2 min, min **0.0005 HMC** | Small accruals still pay out |

## New miner (Windows)

1. Download: `https://hackme.tech/` → release ZIP (`hackme_*_windows.zip`).
2. Unzip; copy `env.public_pool.example` → `.env` next to `hackme.exe`.
3. Run `start_hackme_public_pool.bat` or `start_hackme_dashboard.bat`.
4. Dashboard → **Start pool worker** (needs coordinator token from operator, or public signup flow).
5. Wallet address in hybrid submits or `WORKER_PAYOUT_MAP` on settlement host.

## New miner (Linux / second PC)

```bash
bash scripts/ops/new_miner_journey_gate.sh
# WORKER_ID=worker-my-pc-02 WORKER_SMOKE_SEC=90 bash scripts/ops/new_miner_journey_gate.sh
```

## Verify pool health

```bash
bash scripts/ops/miner_happiness_check.sh
```

## Check progress vs baseline (after 1–2 days)

```bash
bash scripts/ops/compare_baseline_2d.sh
```
