# Red-team security audit (pre–open source)

**Date:** 2026-05-18 (initial) · **Re-audit:** 2026-08-28 (pre-listing)  
**Scope:** node, coordinator, settlement, dashboard, public nginx  
**Verdict:** Safe to publish source **only with production hardening below**. Default dev configs are **not** safe on a public bind.

### Re-audit 2026-08-28 (pre-listing)

| Suite | Result | Notes |
|-------|--------|-------|
| `security_full_audit.sh` | **16/16 PASS** | Ephemeral node + public `hackme.tech/pool` probes |
| `redteam_hard_mode.sh` | **12/12 PASS** | Local node + ephemeral coordinator; RUN_ID `redteam_prelisting_20260828v4` |
| `pool_fuzz_escrow_redteam_gate.sh` | **PASS** | Hybrid signer enabled on ephemeral coordinator |
| Public `redteam_surface_smoke.sh` | **PASS** | All mutating routes reject unauth (403) |

Reports: `reports/tests/security_full_20260828T190633Z/` · `reports/gates/redteam_prelisting_20260828v4/`

---

## Executive summary

| Area | Pre-audit risk | After patches in tree |
|------|----------------|------------------------|
| Coordinator claim/submit without token | **Critical** on public bind | **Fail-closed** unless `HACKME_COORDINATOR_ALLOW_INSECURE=1` |
| `GET /api/work/stats?details=1` leak | **High** | Requires coordinator token; node forwards token |
| `POST /api/tx/send` remote auto-sign | **High** | Requires admin token (except loopback + token) |
| Desktop `/api/desktop/local-auth` token leak | **High** | Fail-closed: HTML embed + API return token only with `HACKME_DESKTOP_EXPOSE_ADMIN_TOKEN=1` |
| Fake attempts payout | **High** (economic) | **Operator policy** — use `PAYOUT_FOUND_ONLY` + hybrid strict |
| Settlement synthetic row | **High** (ops) | Script refuses multi-key map when `workers{}` empty |

Automated smoke: `scripts/tests/security_assertions.sh`, `scripts/tests/redteam_surface_smoke.sh` — **PASS** on loopback node with admin token set.

---

## Critical findings (must fix in production)

### C1 — Open coordinator on the internet

**Attack:** `POST /api/work/claim` + `submit` with inflated `attempts` → accrues `payout_hmc` without GPU work.

**Mitigation:**
- Set `HACKME_COORDINATOR_ADMIN_TOKEN` (strong random).
- Bind coordinator to `127.0.0.1:18081`; expose only via nginx with token on worker paths.
- Never set `HACKME_COORDINATOR_ALLOW_INSECURE=1` on VPS.
- Public pool: `HACKME_COORDINATOR_PAYOUT_FOUND_ONLY=1`, `HACKME_POOL_HYBRID_SIGNER_STRICT=1`.

### C2 — Node without admin token on `0.0.0.0`

**Attack:** All `requireAdminAuth` routes open (genesis, mining, worker, fuzz).

**Mitigation:** Keep default `HACKME_REQUIRE_ADMIN_TOKEN=1` (node **fatals** if token missing).

### C3 — Settlement pays coordinator accrual without re-proof

**Attack:** Inflated coordinator stats → `settle_worker_payouts.sh` drains treasury.

**Mitigation:** Monitor `total_payout_hmc` vs chain; payout policy above; single settlement cron + `flock`; verify `workers{}` before pay.

---

## High findings

| ID | Issue | Mitigation |
|----|--------|------------|
| H1 | Attempt-based payout trusts reported `attempts` (capped by batch) | `PAYOUT_FOUND_ONLY=1` for public pools |
| H2 | Hybrid signer optional | `HYBRID_SIGNER_STRICT=1` |
| H3 | Cheap `found=true` via `PohEval(n)=n*7+13` | Found-only payout + monitoring |
| H4 | Coordinator state in RAM (restart clears dedup) | Treat restart as accrual reset; reconcile settlement state |
| H5 | `WORKER_PAYOUT_MAP` misconfiguration | Ops discipline; audit map before cron |
| H6 | P2P open if `HACKME_P2P_TOKEN` unset | Always set P2P token when P2P is exposed |
| H7 | `tasks/from_code` = compiler execution | Admin-only; disable on public followers |

---

## Code hardening shipped in this tree

1. **Coordinator** — `HACKME_COORDINATOR_REQUIRE_ADMIN_TOKEN` (default on); fatal if token missing on non-loopback bind.
2. **Coordinator** — `GET /api/work/stats?details=1` requires admin token.
3. **Node** — `fetchCoordinatorWorkStats` sends coordinator token for `details=1`.
4. **Node** — `POST /api/tx/send` requires admin for non-loopback callers.
5. **Desktop** — admin token is **not** embedded in dashboard HTML and **not** returned by `/api/desktop/local-auth` unless `HACKME_DESKTOP_EXPOSE_ADMIN_TOKEN=1` (loopback + desktop mode).
6. **Worker ID** — `[a-zA-Z0-9._-]{1,128}` on claim/submit.
7. **worker_loop.sh** — unsigned submit JSON built with `jq`, not string concat.
8. **Coordinator** — optional `HACKME_COORDINATOR_WORKER_TOKEN` for claim/submit only; admin token required for `clear-abuse` and `stats?details=1`.

---

## Production environment variables

See `scripts/ops/public_pool_hardening.env.example`.

```bash
# Coordinator (VPS)
HACKME_COORDINATOR_ADMIN_TOKEN=<strong-secret>
HACKME_COORDINATOR_ADDR=127.0.0.1:18081
HACKME_COORDINATOR_PAYOUT_FOUND_ONLY=1
HACKME_POOL_HYBRID_SIGNER_ENABLED=1
HACKME_POOL_HYBRID_SIGNER_STRICT=1

# Node (command)
HACKME_ADMIN_TOKEN=<strong-secret>
HACKME_REQUIRE_ADMIN_TOKEN=1
HACKME_P2P_TOKEN=<strong-secret>   # if P2P enabled

# Never on public pool
# HACKME_COORDINATOR_ALLOW_INSECURE=1
# HACKME_REQUIRE_ADMIN_TOKEN=0
```

---

## Residual accepted risks (document honestly)

- No per-nonce re-verification of full batch (cost).
- No coordinator persistence (restart semantics).
- Settlement JSON state is file-based, not DB-transactional (mitigated by `flock` on one host).
- Public read APIs (`/api/status`, `/api/work/stats` without details) remain for transparency.

---

## Related docs

- [SECURITY.md](SECURITY.md) — threat model  
- [POOL_SECURITY_THREATS_VERDICT.md](POOL_SECURITY_THREATS_VERDICT.md) — payout abuse scenarios  
