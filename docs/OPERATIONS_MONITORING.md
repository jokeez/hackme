# Operations — coordinator, settlement, incidents

## Coordinator health

- **One-shot pool audit (recommended daily):** `bash scripts/ops/run_pool_health_check.sh` → `reports/pool-health-<ts>/` with `pool.json`, `difficulty.tsv`, process checks.
- Public aggregate often proxied at **`GET /api/global/metrics`** (site/dashboard).
- Direct coordinator **`GET /api/work/stats`** or **`/api/pool/stats`** (deploy path e.g. `/pool/coordinator`).
- Watch **`signed_submits_accepted` / `signed_submits_rejected`**, worker counts, ban counters — spikes imply abuse or client mismatch.

## Settlement automation

- Bridge script: **`scripts/ops/settle_worker_payouts.sh`** (env-driven thresholds and payout maps).
- SUP mint: **`scripts/ops/settle_worker_sup.sh`** (parallel timer on VPS).
- Desktop canonical cache: **`scripts/ops/sync_desktop_settlement_canonical.sh`** (or rely on node HTTP merge after `settlement_api.go` refresh).
- Health probe pattern: **`scripts/ops/settlement_healthcheck.sh`**.
- On systemd VPS: `hackme-worker-settlement.timer`, `hackme-worker-sup-settlement.timer` — `systemctl`, `journalctl`.

## Incidents (first response)

1. **Tip lag / fork hints** — `GET /api/p2p/sync`, `GET /api/status` (`tip_sync_source`, `network_mode_active`).
2. **Schema mismatch** — `/api/status` **`schema_version` ≠ `schema_expected`** → stop taking new orders, migrate DB or redeploy matching binary + `hackme.db` backup.
3. **Mass submit rejects** — coordinator logs + hybrid signer mode; compare worker version vs coordinator policy.
4. **Settlement stalled** — check MIN threshold env, nonce conflicts, wallet balance on sweep operator.

## Deeper validation

- **`docs/ULTIMATE_VALIDATION_RUNBOOK.md`** — long soak path.
- **`docs/CANONICAL_RELEASE_CHECKS.md`** — prod-base fuzz + private gate.
- **`scripts/ops/redteam_hard_mode.sh`** — one-command defensive red-team suite with aggregated PASS/FAIL summary (`reports/gates/<run_id>/summary.json`).
