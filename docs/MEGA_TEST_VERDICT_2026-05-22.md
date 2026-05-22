# Mega test verdict — 2026-05-22

**Branch:** `cursor/iso-audit-build-02a1` · **VPS:** `132.243.112.100` · **Run:** `20260522T1338Z`–`1340Z`

---

## Executive summary

| Verdict | Meaning |
|---------|---------|
| **SHIP (pool + site)** | All automated gates green; prod pool ~61–106 GH/s; retarget M dynamic. |
| **CARE** | `coordinator_matrix` fails unsigned submit under `hybrid_signer_strict` — expected, not a regression. |
| **OPS** | Local P2P SQLite sync still blocked (handshake timeout to :18080); canonical height via HTTP overlay OK. |

---

## Deploy (this session)

| Step | Result |
|------|--------|
| `deploy_hackme_node.sh` → VPS | **PASS** — tip **24612+**, coordinator health OK |
| `deploy_hackme_site.sh` | **PASS** — https://hackme.tech **200** |
| `dashboard.html` (Hash % / Payout % KPIs) | **Deployed** via node rsync |
| Nginx `work/by-wallet` | **Active** on VPS |

---

## Mega test matrix

| Test | Result | Notes |
|------|--------|-------|
| `go test ./...` | **PASS** | Full suite ~20s |
| `nightly_chaos_guard.sh` | **PASS** | 5000 payouts, crypto chaos, init-worker, security |
| `coordinator_mega_stress.sh` (STRESS_QUICK) | **PASS** | 50 workers, 90s, 0% hard errors, READY |
| `production_master_gate.sh` | **PASS** | **8/8** steps (ideal, journey, fuzz, redteam×2, security, soak, go short) |
| `difficulty_health.sh` (prod pool) | **PASS** | observed block ~22s, M in bounds |
| `redteam_surface_smoke` (hackme.tech) | **PASS** |
| `check_invariants.sh` (hackme.tech/pool) | **PASS** tip **24621** |
| `hybrid_signer_smoke.sh` (prod strict) | **PASS** |
| `init_worker_test.sh` | **PASS** incl. `zk_empty_ini` |
| `pool_fairness_audit.sh` | **PASS** | M drifts with fleet; payout model documented |
| `coordinator_matrix.sh` (prod) | **EXPECTED** | 3/7 fail: `signature_required` under strict hybrid |

---

## Live pool snapshot (post-mega)

| Metric | Value |
|--------|--------|
| Pool GH/s | **~61** (kapa-pc ~59 GH/s dominant) |
| `target_mod` | **~15M** (retarget active; was 38M+ under peak) |
| `reward_per_m` | **~0.00066** (scales with M) |
| Workers | kapa-pc, desktop, vps-62, vps-msk |
| `found_hits` | **7+** |

---

## Dashboard / telemetry fixes (deployed)

- Miners table: **Hash %** vs **Payout %** (no more confusion).
- KPI **Work residue** = `attempts mod M`; **Difficulty** shows pool `target_mod` + load hint + `reward/M`.
- `GET /api/work/by-wallet?address=HMC-…` on prod.

---

## Known non-blockers

1. **P2P local DB** — peer handshake to VPS :18080 times out from desktop; use `follower_bootstrap_from_vps.sh` for full mirror.
2. **coordinator_matrix** — update harness to accept 403 `signature_required` when strict (test debt).
3. **ISO** — not rebuilt on kapa-pc (no local `dist/*.iso`); VPS copy from prior deploy remains.

---

## Commands to re-run

```bash
# Full local mega pack
go test ./... -count=1
bash scripts/tests/nightly_chaos_guard.sh
STRESS_QUICK=1 bash scripts/tests/coordinator_mega_stress.sh
bash scripts/ops/production_master_gate.sh

# Prod probes
BASE=https://hackme.tech/pool bash scripts/tests/difficulty_health.sh
bash scripts/ops/pool_fairness_audit.sh
```

---

*Experimental RC — verify explorer payouts before farm scale.*
