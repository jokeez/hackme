#!/usr/bin/env bash
# Drain coordinator fuzz_settle_outbox rows that will never be pulled:
#   - campaign status cancelled/completed on coordinator
#   - optional: campaign_id prefix filter
#
# Hub (via ssh):
#   NODE_SSH=hackme-vps bash scripts/ops/drain_stale_fuzz_settle_outbox.sh
# Dry run:
#   DRY_RUN=1 NODE_SSH=hackme-vps bash scripts/ops/drain_stale_fuzz_settle_outbox.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COORD_DB="${COORD_SQL_DB:-/opt/hackme/data/coordinator_fuzz.db}"
DRY_RUN="${DRY_RUN:-0}"
PREFIX="${CAMPAIGN_PREFIX:-}"

log() { echo "[drain-settle-outbox] $*"; }

run_sql() {
  local sql="$1"
  if [[ -n "${NODE_SSH:-}" ]]; then
    ssh -o BatchMode=yes "$NODE_SSH" "sqlite3 -cmd '.timeout 60000' \"${COORD_DB}\" \"${sql}\""
  elif [[ -f "$COORD_DB" ]]; then
    sqlite3 -cmd '.timeout 60000' "$COORD_DB" "$sql"
  else
    log "no COORD_DB and no NODE_SSH" >&2
    return 1
  fi
}

where_extra=""
if [[ -n "$PREFIX" ]]; then
  where_extra=" AND o.campaign_id LIKE '${PREFIX}%'"
fi

pending="$(run_sql "SELECT COUNT(*) FROM fuzz_settle_outbox o WHERE o.status='pending'${where_extra};" 2>/dev/null || echo 0)"
log "pending before: ${pending}"

stale="$(run_sql "
SELECT COUNT(*)
FROM fuzz_settle_outbox o
JOIN fuzz_campaigns c ON c.id = o.campaign_id
WHERE o.status='pending'
  AND c.status IN ('cancelled','completed')
  ${where_extra};" 2>/dev/null || echo 0)"
log "stale (cancelled/completed campaigns): ${stale}"

if [[ "$DRY_RUN" == "1" ]]; then
  run_sql "
SELECT o.campaign_id, c.status, o.kind, COUNT(*) n
FROM fuzz_settle_outbox o
JOIN fuzz_campaigns c ON c.id = o.campaign_id
WHERE o.status='pending' AND c.status IN ('cancelled','completed')${where_extra}
GROUP BY o.campaign_id, c.status, o.kind
ORDER BY n DESC LIMIT 20;" || true
  log "DRY_RUN — no changes"
  exit 0
fi

n="$(run_sql "
UPDATE fuzz_settle_outbox
SET status='applied', applied_at=strftime('%s','now')
WHERE id IN (
  SELECT o.id FROM fuzz_settle_outbox o
  JOIN fuzz_campaigns c ON c.id = o.campaign_id
  WHERE o.status='pending' AND c.status IN ('cancelled','completed')${where_extra}
);
SELECT changes();" 2>/dev/null || echo 0)"
log "marked applied: ${n}"

pending_after="$(run_sql "SELECT COUNT(*) FROM fuzz_settle_outbox o WHERE o.status='pending'${where_extra};" 2>/dev/null || echo 0)"
log "pending after: ${pending_after}"
log "done"
