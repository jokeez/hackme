#!/usr/bin/env bash
# Print VPS operator checklist so unpaid accrual becomes on-chain balance on HMC-91fe….
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DESKTOP_ENV="${DESKTOP_ENV_FILE:-$ROOT/.env.desktop}"
set -a
# shellcheck disable=SC1090
[[ -f "$DESKTOP_ENV" ]] && . "$DESKTOP_ENV"
set +a
WALLET="${WORKER_PAYOUT_MAP#*=}"
WALLET="${WALLET:-HMC-91fe007e4036c602}"
cat <<EOF
[VPS settlement checklist]
1. On hackme.tech VPS, in /opt/hackme/.env.settlement (or equivalent):
   WORKER_PAYOUT_MAP=worker-kapa-pc=${WALLET}
   MIN_SETTLE_HMC=0.01
2. ADMIN_TOKEN must equal .env.vps HACKME_ADMIN_TOKEN:
   bash scripts/ops/sync_settlement_admin_token.sh
   bash scripts/ops/repair_worker_settlement_state.sh   # if payouts skip with delta=0
3. systemd: hackme-worker-settlement.timer → scripts/ops/settle_worker_payouts.sh
4. Recent repo fix: coordinator may return workers=null — script now synthesizes one row if WORKER_PAYOUT_MAP has exactly ONE worker_id (pool total). Deploy updated script from this repo to /opt/hackme.
5. Desktop accrual is off-chain until settle; balance ${WALLET} grows only after transfer_v1 from payer node.

Verify desktop: WATCH_SEC=30 bash scripts/ops/desktop_accrual_audit.sh
Manual dry log on VPS:
  COORD_URL=http://127.0.0.1:18081 CHAIN_BASE=http://127.0.0.1:18080 bash scripts/ops/settle_worker_payouts.sh
  (use env from .env.settlement; expect "synthetic row" in stderr if workers{} empty)
EOF
