#!/usr/bin/env bash
# Release expired/stale fuzz work leases + repair zombies for one campaign.
#
#   CAMPAIGN_ID=campaign-bootstrap-yyjson-... NODE_SSH=hackme-vps \
#     bash scripts/ops/unstick_fuzz_campaign_leases.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CID="${CAMPAIGN_ID:-${1:-}}"
COORD_DB="${COORD_SQL_DB:-/opt/hackme/data/coordinator_fuzz.db}"
COORD_URL="${COORD_URL:-http://127.0.0.1:18081}"

[[ -n "$CID" ]] || { echo "usage: CAMPAIGN_ID=... $0" >&2; exit 2; }

read_secret() {
  local f="$1"
  [[ -f "$f" && -r "$f" ]] || return 1
  tr -d '\r\n' <"$f"
}

COORD_ADMIN="${COORD_ADMIN_TOKEN:-${COORD_ADMIN:-}}"
if [[ -z "$COORD_ADMIN" ]]; then
  for f in /etc/hackme/coordinator-cleanup.env "$ROOT/.secrets/hackme_coordinator_admin_token"; do
    if [[ "$f" == *.env && -r "$f" ]]; then
      # shellcheck disable=SC1090
      set -a && . "$f" && set +a
      COORD_ADMIN="${COORD_ADMIN_TOKEN:-${COORD_ADMIN:-}}"
      [[ -n "$COORD_ADMIN" ]] && break
    elif tok="$(read_secret "$f" 2>/dev/null)"; then
      COORD_ADMIN="$tok"
      break
    fi
  done
fi

log() { echo "[unstick-leases] $*"; }

run_remote() {
  if [[ -n "${NODE_SSH:-}" ]]; then
    ssh -o BatchMode=yes "$NODE_SSH" "$@"
  else
    "$@"
  fi
}

run_sql() {
  local sql="$1"
  run_remote "sqlite3 -cmd '.timeout 60000' \"${COORD_DB}\" \"${sql}\""
}

log "campaign=$CID"
run_sql "SELECT status,COUNT(*) FROM fuzz_work_items WHERE campaign_id='${CID}' GROUP BY status;" || true

if [[ -n "$COORD_ADMIN" ]]; then
  log "POST repair-zombies"
  if [[ -n "${NODE_SSH:-}" ]]; then
    ssh -o BatchMode=yes "$NODE_SSH" \
      "curl -fsS -X POST '${COORD_URL}/api/fuzz/pool/campaigns/repair-zombies?limit=200' \
        -H 'X-Hackme-Admin-Token: ${COORD_ADMIN}'" | jq -c . || log "repair-zombies skipped"
  else
    curl -fsS -X POST "${COORD_URL}/api/fuzz/pool/campaigns/repair-zombies?limit=200" \
      -H "X-Hackme-Admin-Token: ${COORD_ADMIN}" | jq -c . || log "repair-zombies skipped"
  fi
fi

released="$(run_sql "
UPDATE fuzz_work_items
SET status='pending', lease_owner='', lease_until=0, updated_at=strftime('%s','now')
WHERE campaign_id='${CID}' AND status='leased' AND lease_until < strftime('%s','now');
SELECT changes();" 2>/dev/null || echo 0)"
log "released expired leases: ${released}"

run_sql "SELECT status,COUNT(*) FROM fuzz_work_items WHERE campaign_id='${CID}' GROUP BY status;" || true

prog_url="${COORD_URL}/api/fuzz/pool/campaigns/progress?id=${CID}"
if [[ -n "${NODE_SSH:-}" ]]; then
  ssh -o BatchMode=yes "$NODE_SSH" "curl -fsS '${prog_url}'" | jq -c '{runs_done,findings,status}' || true
else
  curl -fsS "$prog_url" | jq -c '{runs_done,findings,status}' || true
fi
log "done"
