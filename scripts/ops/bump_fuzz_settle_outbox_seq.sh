#!/usr/bin/env bash
# Bump hub coordinator fuzz_settle_outbox sqlite_sequence above legacy applied marks.
#
# Why: before event_id became outbox:<campaign_id>:<id>, customer nodes that
# bootstrap-applied outbox:1..N made low hub outbox ids no-op on settle while
# pull still ACKed. New code emits campaign-scoped ids; this bump is belt-and-
# suspenders for any leftover legacy-format path and keeps hub ids high.
#
# Does NOT wipe DBs. Safe to re-run (idempotent floor).
#
# Usage:
#   scripts/ops/bump_fuzz_settle_outbox_seq.sh /path/to/coordinator.db [floor]
#   FLOOR=60000 scripts/ops/bump_fuzz_settle_outbox_seq.sh /path/to/coordinator.db
#
# Optional: derive floor from a customer node applied table:
#   MAX=$(sqlite3 customer.db "SELECT COALESCE(MAX(CAST(substr(event_id,8) AS INTEGER)),0)
#     FROM fuzz_settle_applied WHERE event_id GLOB 'outbox:[0-9]*'
#     AND event_id NOT LIKE 'outbox:%:%';")
#   scripts/ops/bump_fuzz_settle_outbox_seq.sh hub.db $((MAX+1000))
#
# Replay unpaid escrow after deploy (new event ids):
#   scripts/ops/replay_fuzz_escrow_settle.sh <campaign_id>
set -euo pipefail

DB="${1:-}"
FLOOR="${2:-${FLOOR:-60000}}"

if [[ -z "$DB" || ! -f "$DB" ]]; then
  echo "usage: $0 <coordinator.db> [floor]" >&2
  exit 2
fi
if ! [[ "$FLOOR" =~ ^[0-9]+$ ]] || [[ "$FLOOR" -lt 1 ]]; then
  echo "floor must be a positive integer (got $FLOOR)" >&2
  exit 2
fi
command -v sqlite3 >/dev/null 2>&1 || { echo "sqlite3 required" >&2; exit 2; }

echo "[bump-outbox-seq] db=$DB floor=$FLOOR"
BEFORE="$(sqlite3 "$DB" "SELECT COALESCE(seq,0) FROM sqlite_sequence WHERE name='fuzz_settle_outbox';" 2>/dev/null || echo 0)"
BEFORE="${BEFORE:-0}"
echo "[bump-outbox-seq] before seq=$BEFORE"

sqlite3 "$DB" <<SQL
CREATE TABLE IF NOT EXISTS fuzz_settle_outbox (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  campaign_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  miner_address TEXT NOT NULL DEFAULT '',
  severity TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'pending',
  created_at INTEGER NOT NULL,
  applied_at INTEGER,
  work_item_id INTEGER NOT NULL DEFAULT 0
);
INSERT INTO sqlite_sequence(name, seq)
  SELECT 'fuzz_settle_outbox', $FLOOR
  WHERE NOT EXISTS (SELECT 1 FROM sqlite_sequence WHERE name='fuzz_settle_outbox');
UPDATE sqlite_sequence
   SET seq = CASE WHEN seq < $FLOOR THEN $FLOOR ELSE seq END
 WHERE name='fuzz_settle_outbox';
SQL

AFTER="$(sqlite3 "$DB" "SELECT seq FROM sqlite_sequence WHERE name='fuzz_settle_outbox';")"
echo "[bump-outbox-seq] after seq=$AFTER"
if [[ "$AFTER" -lt "$FLOOR" ]]; then
  echo "[bump-outbox-seq] ERROR: seq $AFTER < floor $FLOOR" >&2
  exit 1
fi
echo "[bump-outbox-seq] ok (no wipe). Deploy build with outbox:<campaign>:<id> event_ids, then replay unpaid campaigns if needed."
