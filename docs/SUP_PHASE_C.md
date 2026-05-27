# SUP Phase C — on-chain ledger

## What shipped

- `accounts.balance_sup_units` + `sup_next_nonce` (migration `migrateSUPLedger`)
- `transfer_sup_v1` mempool (`sup_tx_pool` / `sup_tx_history`)
- Admin **mint** from emission cap: `POST /api/sup/mint`
- Genesis: `POST /api/sup/genesis` (or `scripts/ops/sup_genesis_init.sh`)
- Settlement: `scripts/ops/settle_worker_sup.sh` (coordinator `payout_sup` → on-chain mint)
- Wallet API: `balance_sup`, `balance_sup_units`, `sup_next_nonce`
- Public economics: `GET /api/sup/economics`

## Operator sequence (VPS chain host)

```bash
export CHAIN_BASE=http://127.0.0.1:18080
export COORD_URL=http://127.0.0.1:18081
bash scripts/ops/sup_genesis_init.sh
export HACKME_SUP_ON_CHAIN_SETTLE=1   # coordinator stats + redeploy
bash scripts/ops/settle_worker_sup.sh
```

Optional timer (after HMC settlement timer is stable):

```bash
# cron: */15 * * * * cd /opt/hackme && MIN_SETTLE_SUP=0.01 bash scripts/ops/settle_worker_sup.sh
```

## Policy unchanged

- **CEX/DEX listing** for SUP remains **after primary HMC/HCM is listed**
- Mint only credits earned accrual (settlement script); no public faucet
- Max supply default: **21,000,000 SUP** (`meta sup_max_supply_units`)

## Remaining (Phase D + utility)

- MiningPoolStats / exchange outreach
- Orders audit discount in SUP (utility U.1)
- Pool priority by SUP balance (utility U.2)
- 30-day public emission chart page
