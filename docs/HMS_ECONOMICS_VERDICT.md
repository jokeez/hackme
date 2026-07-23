# HMS economics verdict (2026-05-30)

## Decision

| Parameter | Value | Notes |
|-----------|--------|--------|
| **Seal epoch base** | **0.5 HMS** / sealed epoch | Was 0.01; policy hash changes (`hms-lane-v2-base-0.5`) |
| **Prepaid bonus** | **1%** of Σ prepaid HMC in epoch | Unchanged |
| **Epoch length (prod)** | **3600 s** (`HMS_EPOCH_SECONDS`) | Pilot may use 120 s — emission KPI scales in API |
| **Storage / seal split** | **35% / 65%** | Unchanged |
| **Seal winner / participation** | **75% / 25%** of seal pool | Unchanged |
| **Storage tiers** | HDD 1.0 · SSD 1.15 · NVMe 1.35 | Unchanged |
| **Combo host bonus** | **+10%** storage weight | Unchanged |

## Emission math (base only)

At **1 h** epochs: `0.5 × 24 = 12 HMS/day` → **~4,380 HMS/year** (~0.021% of 21M max supply).

At **120 s** pilot epochs (same base constant): **~360 HMS/day** — acceptable for local stress; **do not** use 120 s epochs on mainnet with this base without lowering base or accepting high inflation.

Prepaid example: **100 HMC** prepaid in one epoch → **+1.0 HMS** to total budget (on top of 0.5 base).

## Per-role rough share (single winner, one seal worker, no storage)

| Pool | Share of 0.5 HMS |
|------|------------------|
| Seal pool (65%) | 0.325 HMS |
| Seal winner (75% of seal) | **~0.244 HMS** |
| Storage pool (35%) | 0.175 HMS (rolls to seal if no storage workers) |

## Verdict

| Area | Score | Status |
|------|-------|--------|
| **Tokenomics design** | **8.5/10** | Base emission now visible to miners; market still drives upside |
| **Local pilot** | **GO** | Coordinator + Stratum + disk worker + dashboard KPIs |
| **Production HMS** | **CONDITIONAL** | Needs dedicated VPS, `HMS_EPOCH_SECONDS=3600`, HMC market volume, listing |

### Risks (unchanged)

- Stock ASIC seal path still smoke / share reject until compatible jobs.
- Empty market → miners live on base + warm only.
- Policy is compile-time; changing base again requires new `seal_reward_policy` hash and coordinated deploy.
