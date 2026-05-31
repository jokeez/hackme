# Operator verdict — HackMe public pool (2026-05-19)

> **Historical snapshot.** For the current release verdict see **[STATUS.md](STATUS.md)** and **[OPERATOR_FINAL_CHECKLIST.md](OPERATOR_FINAL_CHECKLIST.md)**.

Snapshot for the operator: what works, what was fixed, what to do next.

## Verdict: **GO for public mining** (with ops discipline)

| Area | Status | Notes |
|------|--------|--------|
| Public site | OK | https://hackme.tech — downloads, docs, economics, security rewards |
| Open source | OK | https://github.com/jokeez/hackme |
| Bitcointalk ANN | OK | Topic 5583373 |
| Fair pool economics | OK | Attempt-based accrual (`payout_found_only=0`), hybrid signer strict |
| Desktop mining | OK | Worker connects to public coordinator; canonical wallet sync |
| Settlement | **Fixed** | Was blocked by wrong `ADMIN_TOKEN` + over-counted state file |
| Security rewards | OK | Modest HMC tiers documented |
| CEX / MPS listing | **Later** | After stable payouts + visible community hashrate |

## Settlement (critical path)

1. Miners earn **off-chain accrual** on the coordinator (`payout_hmc`).
2. VPS runs `settle_worker_payouts.sh` → `transfer_v1` from payer node wallet.
3. Miner **on-chain balance** grows only after tx is included in canonical chain.

**Failure modes (now documented + scripted):**

- `ADMIN_TOKEN` in `.env.settlement` ≠ `HACKME_ADMIN_TOKEN` in `.env.vps` → `401 admin authentication required`.
- `settled_hmc` in `data/worker_settlement_state.json` > coordinator `payout_hmc` → script skips (`delta=0`).

**Remediation:**

```bash
NODE_SSH=hackme-vps bash scripts/ops/vps_settlement_bootstrap.sh
```

Or step-by-step: `sync_settlement_admin_token.sh` → `repair_worker_settlement_state.sh` → `settle_worker_payouts.sh`.

## Infrastructure

- **One VPS** is enough for coordinator + chain + settlement + nginx.
- Extra VPS: backup node / geo mirror / canary — **not** required for “more GPU mining” on the operator side.
- Miner hashrate comes from **community GPUs**, not from buying more small VPS.

## Growth order

1. Keep settlement green (`settlement_healthcheck.sh` in timer/cron).
2. MiningPoolStats — [MININGPOOLSTATS_LISTING.md](MININGPOOLSTATS_LISTING.md).
3. Telegram / ANN updates when economics or security policy changes.
4. CEX outreach only after sustained pool activity and clear disclosure (RC, not investment product).

## Do not skip

- Rotate any secret that ever lived in git history (`data/` seeds were removed; consider `git filter-repo` if keys were exposed).
- Treasury backup off-repo (Desktop backup script exists).
- Verify download SHA256 for every release.

*This file is an operator snapshot, not legal or financial advice.*
