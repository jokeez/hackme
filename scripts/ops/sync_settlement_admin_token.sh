#!/usr/bin/env bash
# Keep settlement ADMIN_TOKEN in sync with the chain node's HACKME_ADMIN_TOKEN.
# Mismatch causes: "admin authentication required" on /api/tx/send — payouts never land.
#
# On VPS:
#   bash /opt/hackme/scripts/ops/sync_settlement_admin_token.sh
#
# From dev machine:
#   NODE_SSH=hackme-vps bash scripts/ops/sync_settlement_admin_token.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
NODE_SSH="${NODE_SSH:-}"
DEPLOY="${NODE_DEPLOY_DIR:-/opt/hackme}"

if [[ -n "$NODE_SSH" ]]; then
  rsync -avz "$ROOT/scripts/ops/sync_settlement_admin_token.sh" \
    "$NODE_SSH:$DEPLOY/scripts/ops/" 2>/dev/null || true
  ssh -o BatchMode=yes -o ConnectTimeout=15 "$NODE_SSH" \
    "DEPLOY='$DEPLOY' bash '$DEPLOY/scripts/ops/sync_settlement_admin_token.sh'" 2>/dev/null || \
    ssh -o BatchMode=yes -o ConnectTimeout=15 "$NODE_SSH" \
    "DEPLOY='$DEPLOY' bash -s" <"$ROOT/scripts/ops/sync_settlement_admin_token.sh"
  exit $?
fi

deploy="${DEPLOY:-/opt/hackme}"
vps_env="${deploy}/.env.vps"
settle_env="${deploy}/.env.settlement"
secret="${deploy}/.secrets/hackme_admin_token"

if [[ ! -f "$vps_env" ]]; then
  echo "[sync-settle-token] missing ${vps_env}" >&2
  exit 1
fi
# shellcheck disable=SC1090
source "$vps_env"
node_token="${HACKME_ADMIN_TOKEN:-}"
if [[ -z "$node_token" ]]; then
  echo "[sync-settle-token] HACKME_ADMIN_TOKEN empty in ${vps_env}" >&2
  exit 1
fi

touch "$settle_env"
if grep -q '^ADMIN_TOKEN=' "$settle_env" 2>/dev/null; then
  sed -i "s|^ADMIN_TOKEN=.*|ADMIN_TOKEN=${node_token}|" "$settle_env"
else
  echo "ADMIN_TOKEN=${node_token}" >>"$settle_env"
fi

mkdir -p "$(dirname "$secret")"
printf '%s' "$node_token" >"$secret"
chmod 600 "$secret" 2>/dev/null || true
if id hackme &>/dev/null; then
  chown hackme:hackme "$secret" "$settle_env" 2>/dev/null || true
fi

chain_base="${CHAIN_BASE:-http://127.0.0.1:18080}"
code="$(curl -sS -o /dev/null -w '%{http_code}' -X POST \
  "${chain_base}/api/tx/send" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: ${node_token}" \
  -d '{"tx_type":"transfer_v1","from":"HMC-probe","to":"HMC-probe","amount_units":1,"fee_units":1000,"nonce":0,"timestamp_unix":1}' 2>/dev/null || echo 000)"
if [[ "$code" == "401" ]]; then
  echo "[sync-settle-token] ERROR: node still returns 401 with synced token" >&2
  exit 2
fi
echo "[sync-settle-token] OK: ADMIN_TOKEN synced (probe HTTP ${code})"
