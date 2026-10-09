#!/usr/bin/env bash
# Cancel gate/stale pool fuzz campaigns and purge pending work on cancelled campaigns.
#
# Hub (local coordinator):
#   bash scripts/ops/coordinator_fuzz_queue_cleanup.sh
#
# Remote via ssh:
#   NODE_SSH=hackme-vps bash scripts/ops/coordinator_fuzz_queue_cleanup.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

COORD_URL="${COORD_URL:-http://127.0.0.1:18081}"
COORD_URL="${COORD_URL%/}"
COORD_ADMIN="${COORD_ADMIN_TOKEN:-${COORD_ADMIN:-}}"
COORD_DB="${COORD_SQL_DB:-/opt/hackme/data/coordinator_fuzz.db}"

read_secret_file() {
  local f="$1"
  [[ -f "$f" && -r "$f" ]] || return 1
  tr -d '\r\n' <"$f"
}

if [[ -z "$COORD_ADMIN" ]]; then
  for f in \
    /etc/hackme/coordinator-cleanup.env \
    "$ROOT/.secrets/hackme_coordinator_admin_token"; do
    if [[ "$f" == *.env ]]; then
      # shellcheck disable=SC1090
      [[ -r "$f" ]] && set -a && . "$f" && set +a
      COORD_ADMIN="${COORD_ADMIN_TOKEN:-${COORD_ADMIN:-}}"
      [[ -n "$COORD_ADMIN" ]] && break
      continue
    fi
    if tok="$(read_secret_file "$f" 2>/dev/null)"; then
      COORD_ADMIN="$tok"
      break
    fi
  done
fi

[[ -n "$COORD_ADMIN" ]] || {
  echo "[fuzz-queue-cleanup] missing COORD_ADMIN_TOKEN (set env or /etc/hackme/coordinator-cleanup.env)" >&2
  exit 1
}

hdr=(-H "X-Hackme-Admin-Token: ${COORD_ADMIN}" -H "Content-Type: application/json")

log() { echo "[fuzz-queue-cleanup] $*"; }

run_remote_sql() {
  local sql="$1"
  if [[ -n "${NODE_SSH:-}" ]]; then
    ssh -o BatchMode=yes "$NODE_SSH" "sqlite3 -cmd '.timeout 60000' \"${COORD_DB}\" \"${sql}\""
  elif [[ -n "$COORD_DB" && -f "$COORD_DB" && -r "$COORD_DB" ]]; then
    sqlite3 -cmd '.timeout 60000' "$COORD_DB" "$sql"
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

log "POST cleanup-stale min_age_sec=1800 (includes repair-zombies on coordinator)"
run_coord_post "/api/fuzz/pool/campaigns/cleanup-stale?min_age_sec=1800" | jq -c .

log "POST repair-zombies limit=50"
run_coord_post "/api/fuzz/pool/campaigns/repair-zombies?limit=50" | jq -c . || log "repair-zombies skipped (upgrade coordinator)"

# Belt-and-suspenders SQL: reclaim expired leases, cancel stuck expired-lease zombies,
# purge open work on closed campaigns.
run_sql_changes() {
  local label="$1"
  local sql="$2"
  local out
  if out="$(run_remote_sql "PRAGMA busy_timeout=60000; ${sql}" 2>/dev/null)"; then
    # sqlite may print pragma echo lines; take the last integer token.
    local n
    n="$(printf '%s\n' "$out" | awk '/^[0-9]+$/ {v=$1} END{print v+0}')"
    log "SQL ${label}: ${n}"
  else
    log "SQL ${label}: skipped"
  fi
}

run_sql_changes "reclaimed expired leases" \
  "UPDATE fuzz_work_items SET status='pending', lease_owner='', lease_until=0, updated_at=strftime('%s','now') WHERE status='leased' AND lease_until>0 AND lease_until<strftime('%s','now'); SELECT changes();"

run_sql_changes "cancelled stuck expired-lease campaigns" \
  "UPDATE fuzz_campaigns SET status='cancelled', completed_at=strftime('%s','now') WHERE status='running' AND json_extract(config_json, '\$.pool_distributed') IN (1,'true','1') AND (strftime('%s','now') - created_at) >= 3600 AND id IN (SELECT c.id FROM fuzz_campaigns c WHERE c.status='running' AND COALESCE((SELECT COUNT(*) FROM fuzz_work_items w WHERE w.campaign_id=c.id AND w.status='done'),0)=0 AND COALESCE((SELECT COUNT(*) FROM fuzz_work_items w WHERE w.campaign_id=c.id AND w.status='pending'),0)=0 AND COALESCE((SELECT COUNT(*) FROM fuzz_work_items w WHERE w.campaign_id=c.id AND w.status='leased'),0)>0 AND COALESCE((SELECT COUNT(*) FROM fuzz_work_items w WHERE w.campaign_id=c.id AND w.status='leased' AND w.lease_until>=strftime('%s','now')),0)=0); SELECT changes();"

run_sql_changes "cancelled open items on closed campaigns" \
  "UPDATE fuzz_work_items SET status='cancelled', updated_at=strftime('%s','now') WHERE status IN ('pending','leased','replay_pending') AND campaign_id IN (SELECT id FROM fuzz_campaigns WHERE status IN ('cancelled','completed','paused')); SELECT changes();"

run_sql_changes "failed replay jobs on closed campaigns" \
  "UPDATE fuzz_hunt_replay_queue SET status='failed', last_error='campaign closed', verifier_id='', updated_at=strftime('%s','now') WHERE status IN ('pending','processing') AND campaign_id IN (SELECT id FROM fuzz_campaigns WHERE status IN ('cancelled','completed','paused')); SELECT changes();"

# Optional post-cleanup snapshot (COORD_URL may be unset on oneshot units — default loopback).
COORD_URL="${COORD_URL:-http://127.0.0.1:18081}"
stats_url="${COORD_URL}/api/fuzz/pool/stats"
pending=0
running=0
if [[ -n "${NODE_SSH:-}" ]]; then
  read -r pending running < <(ssh -o BatchMode=yes "$NODE_SSH" \
    "curl -fsS http://127.0.0.1:18081/api/fuzz/pool/stats | jq -r '[.work_pending,.campaigns_running]|@tsv'" 2>/dev/null) || true
else
  pending="$(curl -fsS "${stats_url}" 2>/dev/null | jq -r '.work_pending // 0' 2>/dev/null || echo 0)"
  running="$(curl -fsS "${stats_url}" 2>/dev/null | jq -r '.campaigns_running // 0' 2>/dev/null || echo 0)"
fi
log "pool stats: campaigns_running=${running:-0} work_pending=${pending:-0}"
log "done"
