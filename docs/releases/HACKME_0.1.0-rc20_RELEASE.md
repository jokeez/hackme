# HackMe 0.1.0-rc20

**Channel:** `0.1.0-rc20` · **Date:** 2026-11-17

## Highlights

- **Paid pool Orders** — customer Fund → hub prepaid → Dig/Hunt on the public pool (hub escrow).
- **Authoritative reports** — Orders mirrors proxy `report` / `gate` / `pulse` / `proof-bundle` to the hub (no empty local mirror).
- **Honest prepaid UI** — when `HACKME_ORDERS_HUB_URL` is set and the hub is unreachable, spendable shows `0` (`hub_prepaid_unavailable`); no local-mirror fake budget.
- **E2E gates** — customer pool / Hunt / demo scripts fail closed without miner pickup (override only with `ALLOW_SOFT_PICKUP=1`); fund waits for on-chain inclusion.

## Upgrade

- Downloads / self-update / `apt install hackme-node`
- Verify `SHA256SUMS` before install
- Orders: Windows → Orders profile · Linux → `bash start_hackme_orders.sh`

Economics v2 rules are unchanged (live since block 550,000).
