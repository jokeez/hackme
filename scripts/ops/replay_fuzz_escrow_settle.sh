#!/usr/bin/env bash
# Replay fuzz escrow settlements into coordinator outbox for a completed pool campaign.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

CID="${1:-${CAMPAIGN_ID:-}}"
COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
COORD_ADMIN=""
if [[ -f "$ROOT/.secrets/hackme_coordinator_admin_token" ]]; then
  COORD_ADMIN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_coordinator_admin_token")"
fi
[[ -n "$CID" ]] || { echo "usage: $0 <campaign_id>" >&2; exit 2; }
[[ -n "$COORD_ADMIN" ]] || fail "coordinator admin token required"

echo "[replay-settle] enqueue outbox for $CID"
curl -fsS --max-time 120 -X POST "${COORD_URL%/}/api/fuzz/pool/settle/replay" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: ${COORD_ADMIN}" \
  -d "{\"campaign_id\":\"${CID}\"}" | jq .

echo "[replay-settle] waiting for desktop pull (15s)..."
sleep 16

BASE="${BASE:-http://127.0.0.1:8080}"
ADMIN="$(resolve_admin_token "$ROOT" 2>/dev/null || true)"
if [[ -n "$ADMIN" ]]; then
  curl -fsS --max-time 15 -H "X-Hackme-Admin-Token: $ADMIN" \
    "${BASE}/api/fuzz/campaigns/${CID}" 2>/dev/null | jq '{id,status,summary}' || true
  sqlite3 "${HACKME_DB:-$ROOT/data/hackme.db}" \
    "SELECT runs_done,runs_paid_units/1e8,bounty_paid_units/1e8,status FROM fuzz_campaign_escrow WHERE campaign_id='${CID}';" || true
fi
pass "replay_fuzz_escrow_settle done for $CID"
