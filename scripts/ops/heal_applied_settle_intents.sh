#!/usr/bin/env bash
# Promote work-item settle_*_status to paid when the linked outbox row is already applied.
# Safe / idempotent. Fixes legacy ACK races that left queued intents blocking finalize gates.
#
#   NODE_SSH=hackme-vps bash scripts/ops/heal_applied_settle_intents.sh
#   DRY_RUN=1 NODE_SSH=hackme-vps bash scripts/ops/heal_applied_settle_intents.sh
set -euo pipefail

NODE_SSH="${NODE_SSH:-}"
COORD_DB="${COORD_DB:-/opt/hackme/data/coordinator_fuzz.db}"
DRY_RUN="${DRY_RUN:-0}"

_sql() {
  local sql="$1"
  if [[ -n "$NODE_SSH" ]]; then
    ssh -o BatchMode=yes -o ConnectTimeout=20 "$NODE_SSH" \
      "sqlite3 -cmd '.timeout 120000' \"${COORD_DB}\" \"${sql}\""
  else
    sqlite3 -cmd '.timeout 120000' "$COORD_DB" "$sql"
  fi
}

echo "[heal-settle] db=$COORD_DB dry_run=$DRY_RUN"
echo "[heal-settle] before:"
_sql "SELECT settle_run_status, COUNT(*) FROM fuzz_work_items WHERE settle_run_status IN ('queued','pending','queued_floor','pending_floor') GROUP BY 1;"

if [[ "$DRY_RUN" == "1" ]]; then
  _sql "SELECT COUNT(*) FROM fuzz_work_items WHERE settle_run_status IN ('queued','pending','queued_floor','pending_floor') AND settle_run_outbox_id IN (SELECT id FROM fuzz_settle_outbox WHERE status='applied');"
  echo "[heal-settle] DRY_RUN — no writes"
  exit 0
fi

_sql "BEGIN;
UPDATE fuzz_work_items SET settle_run_status='paid'
 WHERE settle_run_status IN ('queued','pending','queued_floor','pending_floor')
   AND settle_run_outbox_id IN (SELECT id FROM fuzz_settle_outbox WHERE status='applied');
UPDATE fuzz_work_items SET settle_finding_status='paid'
 WHERE settle_finding_status IN ('queued','pending')
   AND settle_finding_outbox_id IN (SELECT id FROM fuzz_settle_outbox WHERE status='applied');
COMMIT;"

echo "[heal-settle] after:"
_sql "SELECT settle_run_status, COUNT(*) FROM fuzz_work_items WHERE settle_run_status IN ('queued','pending','queued_floor','pending_floor','paid') GROUP BY 1;"
echo "[heal-settle] OK"
