# HackMe RC status (operator snapshot)

**Release:** `0.1.0-rc11s` · **Site:** https://hackme.tech · **Branch:** `main`  
**Updated:** 2026-07-19 (pool growth · CVE Watch Day 13 published · Day 14 final fuzz next · B2B PoH path live)

| Highlight (2026-07-19) | |
|------------------------|--|
| **Live pool** | ~11 online / ~12–13 registered · ~100–145 GH/s · hybrid signer strict · auto `target_mod` · external miners joining |
| **Settlement** | HMC + SUP timers + autopilot **active** on VPS · unpaid fleet normally **&lt;3 HMC** between scans |
| **B2B / PoH** | Bootstrap PoH deep orders completing (`workerfuzz` on hub · `pool_distributed`) · scheduler returns to `baseline` |
| **Research** | OSS CVE Watch **Day 13/14** published CLEAN ([day13.html](https://hackme.tech/reports/oss-cve-watch/day13.html) · 0.36B exec · cum ~11.29B) · **Day 14 final fuzz** next |
| **Tests** | Prefer `go test ./...` · `public_site_smoke.sh` · `version_consistency_gate` before release cuts |

| Area | Verdict |
|------|---------|
| Public pool + coordinator | **Live** — `signed_submits_accepted` ≫ rejected |
| HMC settlement | **Live** — settle scripts + systemd + autopilot |
| SUP (accrual + on-chain) | **Live** — settle SUP + timer |
| Win/Linux/ISO (rc11s) | **Published** — verify SHA256 on downloads page |
| Security audit (prod) | **16/16 PASS** |
| Miner launch gate | **GO** — `bash scripts/ops/run_miner_launch_gate.sh` |
| Fuzzing B2B | **Live** — wizard + pool-distributed workers + CI gate |
| OSS CVE Watch | **Day 13 live** · Day 14 final fuzz next · gate refuses stubs |

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
