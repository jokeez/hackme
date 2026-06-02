# HMS ASIC pilot (local, before Heavy VPS #2)

## Verdict (read first)

| Path | Works today? | Notes |
|------|----------------|-------|
| **`tools/hms_stratum_asic_sim`** | ✅ Yes | Full SealHash grind + `mining.submit` — use this to validate coordinator Stratum |
| **Stock Antminer / Whatsminer (BTC firmware)** | ❌ No | Firmware double-SHA256 **block headers**, not `SealHash(epoch‖root‖pool‖nonce)` |
| **Custom gateway / Braiins custom job** | 🔜 Production | Required for real ASIC fleet on HMS seal |

Stratum on `:3334` is an **HMS seal gateway**, not a Bitcoin pool. Connection test (subscribe/authorize/TCP) works on real ASIC; **valid seal shares** need simulator or custom firmware.

## Local run

```bash
# Terminal 1 — coordinator + storage + stratum (no CPU seal competitor)
bash scripts/ops/hms_asic_pilot.sh

# Or manual:
bash scripts/ops/hms_local_pilot.sh stop
# edit: skip workerseal in pilot, or stop seal PID after start
```

```bash
# Terminal 2 — after ~90s (seal window open)
go run ./tools/hms_stratum_asic_sim -addr 127.0.0.1:3334 -worker asic-sim-1
```

## Antminer pool settings (connection smoke only)

| Field | Value |
|-------|--------|
| Pool URL | `stratum+tcp://127.0.0.1:3334` (LAN) or VPS public IP when deployed |
| Worker | `asic1` or any name |
| Password | `x` |
| Algorithm | SHA256 (connection only — **will not find HMS seals** on stock firmware) |

Env on coordinator:

```bash
HMS_STRATUM_ENABLE=1
HMS_STRATUM_ADDR=:3334
HMS_STRATUM_INSECURE=1   # pilot only; prod requires signed seal submits
```

## `mining.notify` field mapping

| Stratum slot | HMS meaning |
|--------------|-------------|
| `job_id` | `hms-<epoch>-<sub>` |
| `prevhash` (params[1]) | `manifest_root` hex (32 bytes) |
| `nbits` (params[6]) | `seal_target` hex (32 bytes) |
| `mining.submit` nonce | params[4] (Antminer) or params[1] (simple) |

Simulator and gateway must grind **`SealHash`** per `internal/hms/seal.go`.

## Disk storage (same pilot)

Storage worker uses `data/hms_storage_pilot/` on your machine (quota auto from free space). PoSt proofs run during **ingest** phase (before freeze).
