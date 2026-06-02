# HMS tokenomics (on-chain + lane kernel)

HMS is a **parallel ledger** on the HackMe node (same `HMC-` address format). Storage lane rewards settle via admin mint after epoch seal; transfers and fees apply on PoH blocks like SUP.

## Supply

| Parameter | Value |
|-----------|--------|
| Max supply | **21,000,000 HMS** |
| Unit scale | 1 HMS = 10⁸ units |
| Genesis treasury float | **0.5%** of max supply |

## Epoch budget (after each sealed epoch)

Total budget per sealed epoch:

```
total = 0.5 HMS (base) + 1% × Σ prepaid HMC (market orders with chunks in epoch)
```

At production epoch length (**3600 s**), base-only network emission is **~12 HMS/day** (~4,380 HMS/year). Shorter pilot epochs scale linearly (see `network_base_hms_per_day` in `lane_economics`).

Split:

| Pool | Share | Who |
|------|-------|-----|
| **Storage** | **35%** | Online storage workers (PoSt-eligible) |
| **Seal** | **65%** | Seal winner + participation (Stratum shares) |

If no eligible storage workers in an epoch, the storage slice is **added back to the seal pool**.

### Storage pool (GB·epoch × tier)

Weight per worker:

```
weight = quota_gb × epoch_hours × tier_multiplier × (1 + 10% if combo)
```

| Tier | Multiplier | Detection |
|------|------------|-----------|
| HDD | 1.00 | `lsblk` rotational / model |
| SSD | 1.15 | default SATA/SAS SSD |
| NVMe | 1.35 | `nvme*` device or model |

**Combo (+10%)**: same `host_label` has an **online seal worker** and this storage worker in the epoch (disk + ASIC on one host).

Payout: proportional split of the storage pool by weight.

### Seal pool (unchanged hybrid)

| Share | Rate | Recipient |
|-------|------|-----------|
| Winner | **75%** | First valid seal nonce |
| Participation | **25%** | Seal workers with `shares_ok > 0`, by shares |

### Warm (between seal windows)

- Tiny HMS from accepted warm shares and measured TH (see coordinator `warm.go`).
- Not a substitute for epoch storage/seal pools.

## Market (clients pay HMC)

| Parameter | Value |
|-----------|--------|
| Storage | **0.002 HMC / GB·month** |
| Platform fee | **5%** |
| Burn | **10%** of storage subtotal |

Prepaid HMC **increases epoch HMS budget** (1% of prepaid → bonus HMS units), linking B2B demand to miner emission.

## Burns & fees (on-chain HMS)

- Service burn API, transfer fee **30% burn / 70% treasury**.
- Min transfer fee: **1000 units** (0.00001 HMS).

## Policy hashes

- `seal_reward_policy` — seal + split rate
- `storage_reward_policy` — tier + combo kernel

Both exposed in `GET /api/pool/stats` → `lane_economics`.

## API

| Endpoint | Notes |
|----------|--------|
| `GET /api/pool/stats` | Live metrics + `lane_economics` |
| `GET /api/local/disks` | Disk picker + `storage_tier` |
| `POST /api/storage/register` | `storage_tier`, `host_label`, `disk_id` |
| `GET /api/seal/payouts?epoch_id=N` | Seal + storage units per worker |
