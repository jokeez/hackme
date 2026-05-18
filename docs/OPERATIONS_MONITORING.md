# Operations — coordinator, settlement, incidents

## Coordinator health

- Public aggregate often proxied at **`GET /api/global/metrics`** (site/dashboard).
- Direct coordinator **`GET /api/network/stats`** (URL depends on deploy — e.g. `/pool/coordinator`).
- Watch **`signed_submits_accepted` / `signed_submits_rejected`**, worker counts, ban counters — spikes imply abuse or client mismatch.

## Settlement automation

- Bridge script: **`scripts/ops/settle_worker_payouts.sh`** (env-driven thresholds and payout maps).
- Health probe pattern: **`scripts/ops/settlement_healthcheck.sh`**.
- On systemd VPS: inspect timers/services referenced in project News/runbooks (`systemctl`, `journalctl`).

## Incidents (first response)

1. **Tip lag / fork hints** — `GET /api/p2p/sync`, `GET /api/status` (`tip_sync_source`, `network_mode_active`).
2. **Schema mismatch** — `/api/status` **`schema_version` ≠ `schema_expected`** → stop taking new orders, migrate DB or redeploy matching binary + `hackme.db` backup.
3. **Mass submit rejects** — coordinator logs + hybrid signer mode; compare worker version vs coordinator policy.
4. **Settlement stalled** — check MIN threshold env, nonce conflicts, wallet balance on sweep operator.

## Deeper validation

- **`docs/ULTIMATE_VALIDATION_RUNBOOK.md`** — long soak path.
- **`docs/CANONICAL_RELEASE_CHECKS.md`** — prod-base fuzz + private gate.
- **`scripts/ops/redteam_hard_mode.sh`** — one-command defensive red-team suite with aggregated PASS/FAIL summary (`reports/gates/<run_id>/summary.json`).
