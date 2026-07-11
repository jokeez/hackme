# HackMe RC status (operator snapshot)

**Release:** `0.1.0-rc11s` · **Site:** https://hackme.tech · **Branch:** `main`  
**Updated:** 2026-07-11 (rc11s production baseline · mining canonical overlay · settle drain)

| Highlight (2026-07-07) | |
|------------------------|--|
| **Live pool** | 5–6 workers · ~38–53 GH/s · hybrid signer strict · auto `target_mod` retarget |
| **Settlement** | HMC + SUP timers **active** on VPS · unpaid fleet normally **&lt;2 HMC** between 2‑min scans |
| **Desktop fix** | Node fetches canonical settlement over HTTP (no stale unpaid display) |
| **B2B** | Customer order + pool fuzz escrow **live** same day |
| **Miner inbound** | Organic GPU lead (7742 + RTX 5090) — honest pool economics Q&A |
| **Research** | OSS CVE Watch day 1 (nghttp2) · Bitcoin30 hub archived on site |
| **Tests** | `go test ./...` **PASS** · `public_site_smoke.sh` **PASS** · `version_consistency_gate` **PASS** |

| Area | Verdict |
|------|---------|
| Public pool + coordinator | **Live** — `signed_submits_accepted` ≫ rejected |
| HMC settlement | **Live** — `settle_worker_payouts.sh` + systemd timer |
| SUP (accrual + on-chain) | **Live** — `settle_worker_sup.sh` + timer |
| Win/Linux/ISO (rc11s) | **Published** — verify SHA256 on downloads page |
| Security audit (prod) | **16/16 PASS** |
| Miner launch gate | **GO** — `bash scripts/ops/run_miner_launch_gate.sh` |
| Fuzzing B2B | **Live** — wizard + pool + CI gate |

## Pool health (how to measure)

```bash
# Local + public API snapshot, difficulty samples, process check
bash scripts/ops/run_pool_health_check.sh

# Public coordinator (no secrets)
curl -fsS https://hackme.tech/pool/coordinator/api/work/stats \
  | jq '{workers_online,pool_hashrate_gh_s,target_mod,reward_per_m,total_payout_hmc}'

# Desktop settlement / unpaid
curl -fsS http://127.0.0.1:8080/api/worker/settlement \
  | jq '{wallet_unpaid_hmc,fleet_unpaid_hmc,wallet_unpaid_sup,threshold_ready}'
```

Reports land in `reports/pool-health-<timestamp>/` (see `difficulty.tsv` for M drift).

## Out of scope (preview / later)

1. **HMS** — prelaunch; not for public miners.
2. HMAI / Alpha vectors in dashboard are **preview** — only **HMC** pool is mineable today.
3. **CEX listing** — SUP/HMC tradable on-chain; exchange pairs after HMC lists.

## Version source of truth

| File | Role |
|------|------|
| `scripts/release/CURRENT_VERSION` | Win/Linux release channel (`0.1.0-rc11s`) |
| `scripts/release/CURRENT_ISO_VERSION` | HackMe OS ISO channel (`0.1.0-rc11s`) |
| `web/site/assets/app.js` → `RELEASE_VER` / `ISO_CHANNEL` | Site + dashboard download URLs |
| `main.go` → `Version` | Node binary embed (rebuild/deploy to match) |

Detail: [HACKME_RC11S.md](HACKME_RC11S.md) · Ops: [OPERATIONS_MONITORING.md](OPERATIONS_MONITORING.md)
