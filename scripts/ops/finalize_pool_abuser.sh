#!/usr/bin/env bash
# Finalize pool abuser incident: revoke (idempotent), burn on-chain SUP clawback,
# prune settlement state + coordinator mirror, verify balances.
#
#   WORKER_ID=worker-hdssh01-public-rust \
#   ABUSER_WALLET=HMC-9e4e0f72e75deb59 \
#   bash scripts/ops/finalize_pool_abuser.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

WORKER_ID="${WORKER_ID:-worker-hdssh01-public-rust}"
ABUSER_WALLET="${ABUSER_WALLET:-HMC-9e4e0f72e75deb59}"
ABUSER_IP="${ABUSER_IP:-104.251.226.83}"
COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
CHAIN_BASE="${CHAIN_BASE:-https://hackme.tech}"
STATE_FILE="${STATE_FILE:-data/worker_settlement_state.json}"
MIRROR_FILE="${MIRROR_FILE:-data/worker_coordinator_mirror.json}"

COORD_ADMIN="${HACKME_COORDINATOR_ADMIN_TOKEN:-${COORD_ADMIN_TOKEN:-}}"
NODE_ADMIN="${HACKME_ADMIN_TOKEN:-${ADMIN_TOKEN:-}}"
[[ -n "$COORD_ADMIN" ]] || [[ ! -f "$ROOT/.secrets/hackme_coordinator_admin_token" ]] || \
  COORD_ADMIN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_coordinator_admin_token")"
[[ -n "$NODE_ADMIN" ]] || [[ ! -f "$ROOT/.secrets/hackme_admin_token" ]] || \
  NODE_ADMIN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_admin_token")"
[[ -n "$COORD_ADMIN" ]] || { echo "[finalize-abuser] COORD admin token required" >&2; exit 2; }
[[ -n "$NODE_ADMIN" ]] || { echo "[finalize-abuser] NODE admin token required for SUP burn" >&2; exit 2; }

log() { echo "[finalize-abuser] $*"; }

log "1/5 coordinator revoke + persistent permaban worker=$WORKER_ID ip=$ABUSER_IP"
revoke_body="$(jq -nc --arg w "$WORKER_ID" --arg ip "$ABUSER_IP" '{worker_id:$w, ip_key:$ip}')"
revoke_resp="$(curl -fsS --max-time 30 -X POST "${COORD_URL%/}/api/work/admin/revoke-worker" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: $COORD_ADMIN" \
  -d "$revoke_body")"
echo "$revoke_resp" | jq .

log "2/5 on-chain SUP clawback wallet=$ABUSER_WALLET"
before="$(curl -fsS --max-time 20 "${CHAIN_BASE%/}/api/address/${ABUSER_WALLET}")"
sup_before="$(jq -r '.balance_sup // 0' <<<"$before")"
log "SUP before burn: $sup_before"
if python3 - "$sup_before" <<'PY'
import sys
print("1" if float(sys.argv[1]) > 0.000001 else "0")
PY
then
  burn_resp="$(curl -fsS --max-time 30 -X POST "${CHAIN_BASE%/}/api/sup/burn" \
    -H "Content-Type: application/json" \
    -H "X-Hackme-Admin-Token: $NODE_ADMIN" \
    -d "$(jq -nc --arg from "$ABUSER_WALLET" --arg wid "$WORKER_ID" \
      '{from:$from, memo:("pool_abuse_clawback:"+ $wid)}')")"
  echo "$burn_resp" | jq .
else
  log "skip burn: SUP balance already zero"
fi

log "3/5 prune settlement state ($STATE_FILE)"
if [[ -f "$STATE_FILE" ]]; then
  tmp="$(mktemp)"
  jq --arg wid "$WORKER_ID" 'del(.workers[$wid])' "$STATE_FILE" >"$tmp" && mv "$tmp" "$STATE_FILE"
  log "removed worker from settlement state"
fi

log "4/5 prune coordinator mirror ($MIRROR_FILE)"
if [[ -f "$MIRROR_FILE" ]]; then
  tmp="$(mktemp)"
  jq --arg wid "$WORKER_ID" 'del(.workers[$wid])' "$MIRROR_FILE" >"$tmp" && mv "$tmp" "$MIRROR_FILE"
  log "removed worker from mirror"
fi

log "5/5 verify"
curl -fsS --max-time 20 "${CHAIN_BASE%/}/api/address/${ABUSER_WALLET}" | jq \
  '{address, balance_hmc, balance_sup, balance_units}'
curl -fsS --max-time 20 "${COORD_URL%/}/api/work/stats?details=1" \
  -H "X-Hackme-Admin-Token: $COORD_ADMIN" \
  | jq --arg w "$WORKER_ID" --arg addr "$ABUSER_WALLET" \
  '{total_payout_hmc, has_abuser:(.workers[$w]!=null), by_wallet_ok:true}'
curl -fsS --max-time 20 "${COORD_URL%/}/api/work/by-wallet?address=${ABUSER_WALLET}" \
  | jq '{address, workers_count, workers}'

log "DONE — incident tail closed for $WORKER_ID / $ABUSER_WALLET"
