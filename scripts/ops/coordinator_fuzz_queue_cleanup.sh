#!/usr/bin/env bash
# Cancel gate/stale pool fuzz campaigns and purge pending work on cancelled campaigns.
#
#   bash scripts/ops/coordinator_fuzz_queue_cleanup.sh
#   NODE_SSH=hackme-vps bash scripts/ops/coordinator_fuzz_queue_cleanup.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
COORD_URL="${COORD_URL%/}"
COORD_ADMIN="${COORD_ADMIN_TOKEN:-}"
COORD_DB="${COORD_SQL_DB:-}"

if [[ -z "$COORD_ADMIN" && -f "$ROOT/.secrets/hackme_coordinator_admin_token" ]]; then
  COORD_ADMIN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_coordinator_admin_token")"
fi
[[ -n "$COORD_ADMIN" ]] || { echo "[fuzz-queue-cleanup] missing coordinator admin token" >&2; exit 1; }

hdr=(-H "X-Hackme-Admin-Token: ${COORD_ADMIN}" -H "Content-Type: application/json")

log() { echo "[fuzz-queue-cleanup] $*"; }

run_remote_sql() {
  local sql="$1"
  if [[ -n "${NODE_SSH:-}" ]]; then
    ssh -o BatchMode=yes "$NODE_SSH" "sqlite3 /opt/hackme/data/coordinator.db \"$sql\""
  elif [[ -n "$COORD_DB" && -f "$COORD_DB" ]]; then
    sqlite3 "$COORD_DB" "$sql"
  else
    return 1
  fi
}

run_coord_post() {
  local path="$1"
  if [[ -n "${NODE_SSH:-}" ]]; then
    ssh -o BatchMode=yes "$NODE_SSH" \
      "curl -fsS -X POST http://127.0.0.1:18081${path} -H 'X-Hackme-Admin-Token: ${COORD_ADMIN}' -H 'Content-Type: application/json'"
  else
    curl -fsS -X POST "${COORD_URL}${path}" "${hdr[@]}"
  fi
}

log "POST cleanup-gates"
run_coord_post "/api/fuzz/pool/campaigns/cleanup-gates" | jq -c .

log "POST cleanup-stale min_age_sec=300"
run_coord_post "/api/fuzz/pool/campaigns/cleanup-stale?min_age_sec=300" | jq -c .

if n="$(run_remote_sql "UPDATE fuzz_work_items SET status='cancelled', updated_at=strftime('%s','now') WHERE status IN ('pending','leased') AND campaign_id IN (SELECT id FROM fuzz_campaigns WHERE status='cancelled'); SELECT changes();" 2>/dev/null)"; then
  log "SQL cancelled pending items on cancelled campaigns: ${n:-0}"
else
  log "skip SQL purge (set NODE_SSH or COORD_SQL_DB)"
fi

pending="$(curl -fsS "${COORD_URL}/api/fuzz/pool/stats" 2>/dev/null | jq -r '.work_pending // 0')"
running="$(curl -fsS "${COORD_URL}/api/fuzz/pool/stats" 2>/dev/null | jq -r '.campaigns_running // 0')"
if [[ -n "${NODE_SSH:-}" ]]; then
  read -r pending running < <(ssh -o BatchMode=yes "$NODE_SSH" \
    "curl -fsS http://127.0.0.1:18081/api/fuzz/pool/stats | jq -r '[.work_pending,.campaigns_running]|@tsv'")
fi
log "pool stats: campaigns_running=${running} work_pending=${pending}"
log "done"
