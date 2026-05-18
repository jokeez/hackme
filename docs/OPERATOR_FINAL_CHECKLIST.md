# Operator final checklist — chain, pool, fuzz, deploy

Single-page checklist before treating a release as “final” and when diagnosing prod. For economics semantics (block reward vs coordinator vs orders), see **`docs/ECONOMICS_DASHBOARD.md`** §5–8 and the public page **`web/site/economics-model.html`** (deployed at `/economics-model.html`). **Verdicts / public-launch boundaries (RU):** **`docs/PUBLIC_LAUNCH_VERDICT.md`**. Quick repo slice: **`bash scripts/ops/public_release_readiness.sh`**. **Pool threat model vs code:** **`docs/POOL_SECURITY_THREATS_VERDICT.md`**.

---

## 1) Mental model (30 seconds)

| Layer | Role |
|--------|------|
| **Chain** | PoH blocks, difficulty retarget every **5** blocks vs **5×30s** ideal window (`internal/chain/retarget.go`). Base reward credits **primary wallet** on the producing node; not automatic per-GPU pool shares. |
| **Coordinator** | Worker leases, accepted submits, payout accrual (`total_payout_hmc`, etc.); settled on-chain per your automation. |
| **Orders / fuzz** | Paid workload + WASM gates; primary product demand path. |

---

## 2) Gates (run before public deploy)

| Gate | Command / note |
|------|----------------|
| **Predeploy** | `ADMIN_TOKEN=… LOCAL_BASE=http://127.0.0.1:8080 VPS_BASE=http://<canon>:18080 COORD_URL=http://<coord>:18081 bash scripts/ops/predeploy_gate.sh` — optional `RUN_CORE_GATE=1`, `RUN_HYBRID_SIGNER_SMOKE=1`. |
| **Fuzz release** | Running node + admin: `ADMIN_TOKEN=… BASE=http://127.0.0.1:8080 bash scripts/ops/fuzz_release_gate.sh`. See script header for optional skips. |
| **Canon / prod fuzz** | `ADMIN_TOKEN=… BASE=https://hackme.tech bash scripts/ops/run_canonical_fuzz_gate.sh` — mutates fuzz data on target; staging preferred. See **`docs/CANONICAL_RELEASE_CHECKS.md`**. |
| **Private stage (schema)** | `ADMIN_TOKEN=… BASE=… COORD=… bash scripts/ops/private_stage_gate.sh` — expects `schema_version == schema_expected` on upgraded binaries. |
| **Language static** (optional first) | `MODE=lang_static bash scripts/tests/run_daily.sh` or `STATIC_ONLY=1 bash scripts/tests/run_language_production_pack.sh`. |
| **Heavy validation** | `docs/ULTIMATE_VALIDATION_RUNBOOK.md` — use when you need one massive pass (timeboxed). |

---

## 3) Deploy (typical)

| Target | Command |
|--------|---------|
| **Static site only** | `NODE_SSH=<ssh-alias> SKIP_DIST=1 bash scripts/ops/deploy_hackme_site.sh` |
| **Site + release bundles** | `NODE_SSH=<ssh-alias> SKIP_DIST=0 bash scripts/ops/deploy_hackme_site.sh` |
| **Full public stack** | `NODE_SSH=<ssh-alias> bash scripts/ops/deploy_hackme_public_stack.sh` — review `SKIP_*` / `SYNC_DIST` in script header. |

---

## 4) Post-deploy smoke (manual)

- `https://hackme.tech/` HTTP 200; hard-refresh after CSS/HTML changes.
- `/economics-model.html` if you changed economics copy.
- Canonical node: `GET /api/status` — **`schema_version` == `schema_expected`**; tip; **`economics`** populated locally or overlaid from remote canon when configured.
- Coordinator: work stats / worker health per your dashboard or `GET /api/work/stats` via proxied node if configured.

---

## 5) Settlement & pool hygiene

- **`scripts/ops/settle_worker_payouts.sh`** — bridge coordinator accrual → on-chain payouts (see script/env comments).
- **`scripts/ops/settlement_healthcheck.sh`** — periodic sanity if you use systemd timers (see News/runbooks for VPS layout).

---

## 6) Fuzz emphasis (product path)

- Prefer keeping **`fuzz_release_gate.sh`** green on the same base URL you ship.
- Tie roadmap UI/API work to **measurable campaigns** (telemetry, exports, acceptance criteria for auditors).
- Read **`docs/FUZZ_PRODUCT_GUIDE.md`** for autorunner env (`HACKME_FUZZ_*`), customer report tokens, **`/gate`** for CI, and housekeeping endpoints.
- Public mirror: **`web/site/fuzz-guide.html`** (deploy at `/fuzz-guide.html`).

---

## 7) When something looks “wrong”

1. Split **follower vs canonical** (`network_mode_active`, `tip_sync_source` in status).
2. Compare **accrued coordinator payout** vs **wallet after settlement**.
3. Check **reject counters** (stale, replay, hybrid signer) before blaming chain economics.

## 8) Customer fuzz deliverables & ops

- Deliverables checklist: **`docs/CUSTOMER_FUZZ_DELIVERABLES.md`**
- Coordinator/settlement/incidents: **`docs/OPERATIONS_MONITORING.md`**
