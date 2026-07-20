# HackMe RC status (operator snapshot)

**Release:** `0.1.0-rc11s` · **Site:** https://hackme.tech · **Branch:** `main`  
**Updated:** 2026-07-20 (hygiene refresh · as-of live metrics — re-check with scripts below)  
**Git tip (local):** `4d34cd8` (fuzz settle queued-vs-paid + HMS payment edges after `91b99b4`)

| Highlight (as of 2026-07-20) | |
|------------------------------|--|
| **Live pool** | Hybrid signer strict · auto `target_mod` · external miners joining — verify with `run_pool_health_check.sh` |
| **Settlement** | HMC + SUP timers + autopilot · subsidy snapshot · catch-up guard fixed 20 Jul |
| **B2B / PoH** | Bootstrap PoH deep orders completing (`workerfuzz` · `pool_distributed`) · scheduler returns to `baseline` |
| **Research** | OSS CVE Watch **14/14 complete** CLEAN ([hub](https://hackme.tech/reports/oss-cve-watch/) · [day14](https://hackme.tech/reports/oss-cve-watch/day14.html)) · Day 15+ → libheif |
| **Tests** | Prefer `go test ./...` · `public_site_smoke.sh` · `version_consistency_gate` before release cuts |

| Area | Verdict |
|------|---------|
| Public pool + coordinator | **Live** — confirm `signed_submits_accepted` ≫ rejected on VPS |
| HMC settlement | **Live** — settle scripts + systemd + autopilot |
| SUP (accrual + on-chain) | **Live** — settle SUP + timer |
| Win/Linux/ISO (rc11s) | **Published** — verify SHA256 on downloads page |
| Security audit (prod) | **16/16 PASS** (prior gate; re-run before major cuts) |
| Miner launch gate | **GO** — `bash scripts/ops/run_miner_launch_gate.sh` |
| Fuzzing B2B | **Live** — wizard + pool-distributed workers + CI gate |
| OSS CVE Watch | **Series complete 14/14 CLEAN** · ~14.32B exec · [verdict](verdicts/OSS_CVE_WATCH_NGHTTP2_SERIES_VERDICT.md) · Day 15+ libheif |
| HMS | **Prelaunch** — not public; do not open to miners |

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

Detail: [HACKME_RC11S.md](HACKME_RC11S.md) · Historical: [HACKME_RC11R.md](HACKME_RC11R.md) · Ops: [OPERATIONS_MONITORING.md](OPERATIONS_MONITORING.md)
