# Open pool — conditions for miners

<div align="center">

**HackMe Network** · release **0.1.0-rc11l** (launch candidate)

[hackme.tech](https://hackme.tech) · [Downloads](https://hackme.tech/downloads.html) · [Pool stats](https://hackme.tech/pool/coordinator/api/pool/stats) · [Telegram](https://t.me/hackme_tech) · [Bitcointalk](https://bitcointalk.org/index.php?topic=5583373.0)

</div>

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

1. Download: https://hackme.tech/downloads.html → **HackMe-Setup.exe** (recommended) or portable zip.
2. Run the installer — pool token is **preconfigured**.
3. Launch **HackMe Miner** from the Start menu or desktop shortcut.
4. Dashboard → verify worker connects to `https://hackme.tech/pool/coordinator`.
5. Register payout wallet (`HMC-…`) on the coordinator for your worker ID.

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
