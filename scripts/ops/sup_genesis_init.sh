#!/usr/bin/env bash
# Initialize SUP on-chain ledger (max supply meta + mint enabled). Idempotent.
set -euo pipefail
CHAIN_BASE="${CHAIN_BASE:-http://127.0.0.1:18080}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ADMIN_SECRET_FILE="${ADMIN_SECRET_FILE:-${ROOT_DIR}/.secrets/hackme_admin_token}"
if [[ -z "$ADMIN_TOKEN" && -r "$ADMIN_SECRET_FILE" ]]; then
  ADMIN_TOKEN="$(tr -d '\r\n' <"$ADMIN_SECRET_FILE")"
fi
if [[ -z "$ADMIN_TOKEN" ]]; then
  echo "[sup-genesis] ADMIN_TOKEN required" >&2
  exit 1
fi
resp="$(curl -fsS -X POST "${CHAIN_BASE}/api/sup/genesis" \
  -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{}')"
echo "$resp" | jq .
echo "[sup-genesis] OK — set HACKME_SUP_ON_CHAIN_SETTLE=1 on coordinator after verifying economics"
