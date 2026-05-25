# Production Master Roadmap — HackMe Pool

**Current state (private 3-rig):** OPERATIONAL. See `reports/ideal-LATEST/IDEAL_VERDICT.md`.

This document is the **logical test order** after private pool validation.

---

## Phase 0 — Done (private pool finale)

| Item | Status |
|------|--------|
| PC + 2 VPS workers in pool | Done |
| Settlement → HMC-91fe | Done |
| Fuzzing orders (manual) | Proven |
| Dashboard / fuzz UI | Fixed |
| Baseline / compare scripts | `compare_baseline_4h.sh`, `pool_ideal_finalize.sh` |

---

## Phase 1 — Red team & network failure (next, ~1–3 days)

**Goal:** prove the stack fails closed and recovers.

| Test | Command | Target |
|------|---------|--------|
| Surface red team (unauth rejects) | `BASE=https://hackme.tech bash scripts/tests/redteam_surface_smoke.sh` | Public nginx |
| Surface red team (local node) | `BASE=http://127.0.0.1:8080 bash scripts/tests/redteam_surface_smoke.sh` | Desktop |
| Full red team suite | `bash scripts/ops/redteam_hard_mode.sh` | Ephemeral local stack (18080/18081) |
| Network soak | `BASE=https://hackme.tech DURATION_SEC=3600 INTERVAL_SEC=30 bash scripts/ops/network_stability_soak.sh` | Public uptime |
| Coordinator matrix | `COORD=https://hackme.tech/pool/coordinator bash scripts/tests/coordinator_matrix.sh` | Pool API |
| Security assertions | `BASE=http://127.0.0.1:8080 bash scripts/tests/security_assertions.sh` | Economics invariants |

**Chaos to add manually:** stop coordinator 60s, stop nginx, flush conntrack, verify workers backoff and resume.

---

## Phase 2 — Many miners & payouts (scale lab, ~1–2 weeks)

**Goal:** understand claim/submit limits, settlement at volume, payout fairness.

| Test | Command | Notes |
|------|---------|-------|
| Local swarm | `bash scripts/ops/simulate_pool_swarm_local.sh` | Many synthetic workers locally |
| Mega stress | `bash scripts/tests/mega_stress.sh` | Coordinator load |
| Settlement batch | `FORCE_SETTLE_ALL=1 bash scripts/ops/settle_worker_payouts.sh` on VPS | After swarm |
| Top pool gate | `PROFILE=canary BASE=https://hackme.tech COORD=... bash scripts/ops/top_pool_readiness_gate.sh` | KPI thresholds |

**Architecture (1M workers):** README § Pool scale blueprint — Phase B–D (edge fan-in, shard coordinators, async read model). Current = **Phase A** (single coordinator, caps 600 claim/min/worker).

---

## Phase 3 — Fuzzing at scale

| Test | Command |
|------|---------|
| Orders matrix | `BASE=http://127.0.0.1:18080 ADMIN_TOKEN=... bash scripts/tests/orders_matrix.sh` |
| Multilang orders | `bash scripts/tests/orders_multilang_audit.sh` |
| Fuzzing soak | `bash scripts/ops/fuzzing_soak_prep.sh` (VPS treasury) |
| Monitor share | `bash scripts/ops/pool_multi_rig_monitor.sh` during open orders |

Coordinator must have `HACKME_COORDINATOR_ORDERS_URL` + `ORDERS_PRIORITY=1` (already on VPS).

---

## Phase 4 — Security & release gate

| Gate | Command |
|------|---------|
| Ultimate max | `bash scripts/ops/ultimate_max_gate.sh` |
| Public release readiness | `bash scripts/ops/public_release_readiness.sh` |
| VPS canonical smoke | `bash scripts/ops/vps_canonical_smoke.sh` |
| Pool security verdict | Read `docs/POOL_SECURITY_THREATS_VERDICT.md` |

---

## Phase 5 — Millions of miners (production architecture)

Not a single-machine test. Requires:

1. **Regional coordinators** or shard by `hash(worker_id)`
2. **Settlement worker** as queue (not synchronous tx per payout)
3. **GLOBAL metrics** from materialized view (not live scan of 1M workers)
4. **Worker bundle** — binary + systemd (MSK pattern), no Go on VPS
5. **Rate limits** per IP + per worker_id (already in coordinator)

---

## One command (local aggregate gate)

```bash
bash scripts/ops/production_master_gate.sh
```

Produces: `reports/master-gate-<timestamp>/MASTER_VERDICT.md`
