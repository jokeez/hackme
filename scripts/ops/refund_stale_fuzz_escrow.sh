#!/usr/bin/env bash
# Refund open fuzz escrow for campaigns already cancelled/completed (one-time ledger repair).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

BASE="${BASE:-http://127.0.0.1:8080}"
ADMIN="$(resolve_admin_token "$ROOT" 2>/dev/null || true)"
DB="${HACKME_DB:-$ROOT/data/hackme.db}"
DRY="${DRY_RUN:-0}"

require_cmd curl
require_cmd jq
require_cmd sqlite3

if [[ ! -f "$DB" ]]; then
  fail "database not found: $DB"
fi

open_n="$(sqlite3 "$DB" "SELECT COUNT(*) FROM fuzz_campaign_escrow e JOIN fuzz_campaigns c ON c.id=e.campaign_id WHERE e.status IN ('open','bounty_paid') AND c.status IN ('cancelled','completed');")"
locked="$(sqlite3 "$DB" "SELECT COALESCE(SUM(e.budget_units - e.runs_paid_units - e.bounty_paid_units)/1e8,0) FROM fuzz_campaign_escrow e JOIN fuzz_campaigns c ON c.id=e.campaign_id WHERE e.status IN ('open','bounty_paid') AND c.status IN ('cancelled','completed');")"

echo "[escrow-cleanup] stale open escrows: ${open_n} (~${locked} HMC unpaid slices)"
if [[ "${open_n:-0}" -eq 0 ]]; then
  pass "no stale open escrows"
  exit 0
fi

if [[ "$DRY" == "1" ]]; then
  sqlite3 -header -column "$DB" \
    "SELECT c.id, c.title, c.status AS campaign_status, e.status AS escrow_status, ROUND(e.budget_units/1e8,4) AS budget_hmc FROM fuzz_campaign_escrow e JOIN fuzz_campaigns c ON c.id=e.campaign_id WHERE e.status IN ('open','bounty_paid') AND c.status IN ('cancelled','completed') LIMIT 20;"
  echo "[escrow-cleanup] DRY_RUN=1 — no changes"
  exit 0
fi

if curl -fsS --max-time 5 "${BASE}/api/status?lite=1" >/dev/null 2>&1 && [[ -n "$ADMIN" ]]; then
  echo "[escrow-cleanup] via live node API ${BASE}"
  resp="$(curl -fsS --max-time 120 -X POST "${BASE}/api/fuzz/escrow/cleanup-stale" \
    -H "X-Hackme-Admin-Token: ${ADMIN}")"
  echo "$resp" | jq .
  pass "escrow cleanup via API"
  exit 0
fi

fail "node down at $BASE or missing admin token — start desktop node or set BASE/ADMIN_TOKEN"
