# HMS backend (committed)

> **Prelaunch.** HMS is **not deployed** on the hub VPS (`hackme.tech`). Use this doc for local dev only until HMS goes live on a separate backend.

Production HMS lane code lives in the **main repo** (not only `adapters/hms/`).

## Binaries

| Binary | Role |
|--------|------|
| `cmd/hmscoordinator` | Epochs, manifest Merkle, PoSt challenges, seal work, pool stats |
| `cmd/workerstorage` | Disk worker — register, answer challenges |
| `cmd/workerseal` | CPU seal grinder (dev); ASIC via Stratum on coordinator |

## Local dev

```bash
# Terminal 1
bash scripts/ops/hms_coordinator_up.sh

# Terminal 2 (optional)
go build -o bin/workerstorage ./cmd/workerstorage
HACKME_HMS_COORDINATOR_URL=http://127.0.0.1:18082 bin/workerstorage

# Terminal 3 (after epoch freeze — shorten epoch for test)
HMS_EPOCH_SECONDS=120 HMS_FREEZE_AFTER_SEC=60 HMS_SEAL_WINDOW_SEC=50 \
  go run ./cmd/workerseal
```

Stats: `curl -s http://127.0.0.1:18082/api/pool/stats | jq`

## Anti-abuse (MVP)

- Per-IP + per-worker rate limits (`AbuseGuard`)
- Quota min/max per storage worker
- Ed25519 on storage/seal submits (HTTP)
- Challenge expiry + strikes → epoch ban
- Duplicate seal nonce rejected
- **No seal ⇒ no payout** (`payouts_enabled` on epoch row)
- Chunk assign only before freeze

## ASIC (pilot)

Set on coordinator:

```bash
HMS_STRATUM_ENABLE=1
HMS_STRATUM_ADDR=:3334
HMS_STRATUM_INSECURE=1   # pilot only — use signed submits in prod
```

Point Antminer/stratum client at heavy VPS port 3334 when nginx exposes it.

## Deploy

Heavy VPS #2 only — see [HMS_PUBLIC_ROADMAP.md](HMS_PUBLIC_ROADMAP.md). Hub nginx later: `/pool/hms/` → `:18082`.
