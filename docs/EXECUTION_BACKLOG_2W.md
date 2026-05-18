# HackMe 2-Week Execution Backlog

This backlog focuses on release readiness beyond test execution.

## Week 1 — Stability + Ops Readiness

1. **Release readiness gate in CI/manual routine**
   - Run: `scripts/tests/release_readiness_gate.sh`
   - Goal: deterministic READY/NOT_READY verdict from full/adv/pre/mega runs.

2. **Database backup/restore drill**
   - Backup: `scripts/ops/backup_db.sh`
   - Restore: `scripts/ops/restore_db.sh`
   - Goal: complete one successful restore rehearsal and restart node.

3. **Safe stress mode policy**
   - Keep `ORDERS_MODE=nospend` as default for stress runs.
   - Use `ORDERS_MODE=spend` only in explicit economy tests.
   - Goal: no accidental wallet drain during load tests.

4. **Multi-node rehearsal baseline**
   - Run `rehearsal_onboarding.sh` with at least 2 nodes.
   - Goal: all nodes PASS status/metrics/wallet checks.

Operational convenience for dev/staging:

- Use `scripts/ops/dev_cleanup_orders_wallet.sh` in dry-run first.
- Apply mode (`APPLY=1`) is intended only for non-production environments.

## Week 2 — Network Expansion Readiness

5. **Scale staircase**
   - Run mega stress with increasing worker profiles (e.g. 1x, 1.5x, 2x).
   - Goal: document degradation thresholds (5xx, network errors, metrics health).

6. **P2P/chain sync verification pass**
   - Validate consistent chain progression and no obvious reorg anomalies in multi-node setup.
   - Goal: confidence before broader private expansion.

7. **Incident response runbook**
   - Document stop/restart, backup restore, quick gate checks.
   - Goal: operators can recover from node failure without ad-hoc actions.

8. **Go/No-Go decision**
   - Inputs: full/adv/pre/mega + rehearsal + restore drill.
   - Output: explicit decision record READY / NOT_READY with reasons.

