# GHS ↔ Dig/Hunt coupling

**Status:** shipped on `main` (A canary live; **B** marketplace ETA/capacity UI)  
**Honesty:** GPU hashrate (GH/s) still pays **PoH HMC/SUP**. Dig/Hunt still pays **customer escrow**. ASAN/WASM stay on **CPU**.

## What changed

| Layer | Behavior |
|-------|----------|
| **Hybrid dig boost** | When live PoH GH/s ≥ ~70% of calib, claim gap halves → more Dig/Hunt shards without killing GPU |
| **Hybrid backpressure** | Unchanged idea: GH/s ≪ calib → pause dig (protect mining) |
| **Claim GHS priority** | When hybrid workers (Blake3 share-implied GH/s ≥ `HACKME_FUZZ_GHS_HYBRID_MIN`, default **0.002**) are online, dig-only (~0 GH) get ~25% admit slots; hybrids always claim |
| **Fleet capacity / ETA** | `GET /api/fuzz/pool/stats` + campaign progress/list expose `fleet_capacity`, `est_shards_per_hour`, `eta_sec_fleet` |
| **Marketplace UI (B)** | Campaign board + dashboard mining panel show live hybrid/dig capacity and per-order fleet ETA |

HTML finding reports stay findings-only (no GH/s in artifacts). Fleet ETA is order progress / marketplace only.

## Escape hatches

```bash
HACKME_FUZZ_CLAIM_GHS_PRIORITY=0          # disable claim soft-priority
HACKME_FUZZ_CLAIM_GHS_DIG_ONLY_PCT=25     # dig-only admit % when hybrids online
HACKME_FUZZ_GHS_HYBRID_MIN=0.002          # Blake3 share-implied floor for hybrid Dig capacity
HACKME_WORKER_HYBRID_FUZZ_DIG_BOOST_PCT=70
HACKME_WORKER_HYBRID_FUZZ_DIG_BOOST_PCT=101  # >100 disables boost (do not clamp)
HACKME_WORKER_HYBRID_FUZZ_BACKPRESSURE_PCT=10  # dig profile default; 0 disables pauses only
```

Hybrid claim/capacity requires **recent PoH GH/s** (`LastPoHSeenUnix`) **and** (for fleet Dig capacity) **recent fuzz** (`LastFuzzSeenUnix`). Fuzz heartbeats alone cannot keep hybrid priority after mining stops; PoH-only miners do not throttle dig-only fleets.

## Bench

```bash
bash scripts/tests/ghs_dig_coupling_bench.sh
go test ./internal/poolfuzz/ -run FleetETA -count=1
```

## Why this helps

- **Customers:** ETA reflects real pool GPU+dig capacity; orders prefer live hybrid rigs.
- **Miners:** Healthy GHS digs faster (more escrow) without fake “GPU ASAN”.
- **Pool:** Dig-only Sybils no longer monopolize leases while GPUs are online.
