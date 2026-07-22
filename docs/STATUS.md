# HackMe RC status (operator snapshot)

**Release:** `0.1.0-rc12w` (Win/Linux/fuzz/ISO) · **Site:** https://hackme.tech · **Branch:** `main`  
**Updated:** 2026-07-22 (rc12w shipped · wallet Activity fix · site P0 audit · pool ~145 GH/s)  
**Git tip (local):** see `git rev-parse --short=12 HEAD`

| Highlight (as of 2026-07-22) | |
|------------------------------|--|
| **Live pool** | **~145 GH/s** · **11 workers** · verify with `run_pool_health_check.sh` |
| **Settlement** | HMC + SUP timers + autopilot · subsidy snapshot |
| **B2B / PoH** | Bootstrap PoH deep orders · scheduler `baseline` |
| **Research** | nghttp2 **14/14 CLEAN** · **libheif Day 2/14** in progress |
| **Tests** | `go test ./...` · `public_site_smoke.sh` · `version_consistency_gate` before cuts |

| Area | Verdict |
|------|---------|
| Public pool + coordinator | **Live** — confirm `signed_submits_accepted` ≫ rejected on VPS |
| HMC settlement | **Live** — settle scripts + systemd + autopilot |
| SUP (accrual + on-chain) | **Live** — settle SUP + timer |
| Win/Linux/fuzz/ISO (rc12w) | **Published** — GitHub + hackme.tech/dist |
| Security audit (prod) | **16/16 PASS** (prior gate; re-run before major cuts) |
| Miner launch gate | **GO** — `bash scripts/ops/run_miner_launch_gate.sh` |
| Fuzzing B2B | **Live** — wizard + pool-distributed workers + CI gate |
| OSS CVE Watch · nghttp2 | **Series complete 14/14 CLEAN** |
| OSS CVE Watch · libheif | **Day 2/14 running** · `TARGET=libheif` |
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
| `scripts/release/CURRENT_VERSION` | Win/Linux release channel (`0.1.0-rc12w`) |
| `scripts/release/CURRENT_ISO_VERSION` | HackMe OS ISO channel (`0.1.0-rc12w`) |
| `web/site/assets/app.js` → `RELEASE_VER` / `ISO_CHANNEL` | Site + dashboard download URLs |
| `main.go` → `Version` | Node binary embed (rebuild/deploy to match) |

Detail: [HACKME_RC12W.md](HACKME_RC12W.md) · Historical: [HACKME_RC11S.md](HACKME_RC11S.md) · Ops: [OPERATIONS_MONITORING.md](OPERATIONS_MONITORING.md)
