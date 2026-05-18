#!/usr/bin/env bash
set -euo pipefail

# Bring up a "real demo participant" node:
# - keeps chain state from leader snapshot (restore optional)
# - rotates local node identity (new node_ed25519.seed)
# - connects to leader as follower peer
# - optionally runs sync autopilot and starts mining after sync
#
# Typical flow:
# 1) On leader: create DB tar
#    DB_PATH=./data/hackme.db BACKUP_DIR=./backups bash scripts/ops/backup_db.sh
# 2) Copy tar to participant machine and set LEADER_DB_TAR=...
# 3) Run this script on participant:
#    ROLE=follower LEADER_URL=http://<leader-ip>:<port> ADVERTISE_URL=http://<me-ip>:<port> \
#    ADMIN_TOKEN=... P2P_TOKEN=... LEADER_DB_TAR=./backups/hackme-db-*.tar.gz \
#    bash scripts/ops/demo_participant_up.sh

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[demo-participant-up] missing command: $1" >&2
    exit 1
  }
}

require_cmd bash
require_cmd curl

ROLE="${ROLE:-follower}"
LEADER_URL="${LEADER_URL:-}"
ADVERTISE_URL="${ADVERTISE_URL:-}"
BIND_ADDR="${BIND_ADDR:-0.0.0.0:8080}"
LOCAL_BASE="${LOCAL_BASE:-http://127.0.0.1:${BIND_ADDR##*:}}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
P2P_TOKEN="${P2P_TOKEN:-${HACKME_P2P_TOKEN:-}}"
TOKEN_SECRET="${TOKEN_SECRET:-}"
COORD_URL="${COORD_URL:-${HACKME_POOL_COORDINATOR_URL:-}}"
COORD_TOKEN="${COORD_TOKEN:-${COORD_ADMIN_TOKEN:-${HACKME_POOL_COORDINATOR_TOKEN:-}}}"
LEADER_DB_TAR="${LEADER_DB_TAR:-}"
RESET_NODE_SEED="${RESET_NODE_SEED:-1}"
RUN_AUTOPILOT="${RUN_AUTOPILOT:-1}"
AUTO_START_MINING_WHEN_SYNCED="${AUTO_START_MINING_WHEN_SYNCED:-1}"
AUTOPILOT_LOOPS="${AUTOPILOT_LOOPS:-120}"
DB_PATH="${DB_PATH:-$ROOT_DIR/data/hackme.db}"

validate_restored_db() {
  if ! command -v sqlite3 >/dev/null 2>&1; then
    echo "[demo-participant-up] WARN: sqlite3 not found; skip DB validation"
    return 0
  fi
  if [[ ! -f "$DB_PATH" ]]; then
    echo "[demo-participant-up] ERROR: restored DB not found: $DB_PATH" >&2
    return 1
  fi
  local blocks max_h genesis_row
  blocks="$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM blocks;" 2>/dev/null || echo "0")"
  max_h="$(sqlite3 "$DB_PATH" "SELECT COALESCE(MAX(block_index),-1) FROM blocks;" 2>/dev/null || echo "-1")"
  genesis_row="$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM blocks WHERE block_index=0;" 2>/dev/null || echo "0")"
  echo "[demo-participant-up] restored DB stats: blocks=$blocks max_h=$max_h genesis_rows=$genesis_row"
  if [[ "$genesis_row" == "0" || "$max_h" == "-1" ]]; then
    echo "[demo-participant-up] ERROR: restored snapshot has no genesis; aborting startup" >&2
    return 1
  fi
  return 0
}

if [[ "$ROLE" != "follower" ]]; then
  echo "[demo-participant-up] ROLE must be follower for participant demo mode" >&2
  exit 1
fi
if [[ -z "$LEADER_URL" ]]; then
  echo "[demo-participant-up] LEADER_URL is required (e.g. http://132.243.112.100:18080)" >&2
  exit 1
fi
if [[ -z "$ADVERTISE_URL" ]]; then
  echo "[demo-participant-up] ADVERTISE_URL is required (your public node URL)" >&2
  exit 1
fi
if [[ -z "$ADMIN_TOKEN" || -z "$P2P_TOKEN" ]]; then
  if [[ -z "$TOKEN_SECRET" ]]; then
    echo "[demo-participant-up] set ADMIN_TOKEN+P2P_TOKEN or TOKEN_SECRET" >&2
    exit 1
  fi
  ADMIN_TOKEN="HMC_ADMIN_$(printf '%s' "admin|$TOKEN_SECRET" | sha256sum | awk '{print $1}' | cut -c1-32)"
  P2P_TOKEN="$(printf '%s' "p2p|$TOKEN_SECRET" | sha256sum | awk '{print $1}' | cut -c1-48)"
fi

if [[ -n "$LEADER_DB_TAR" ]]; then
  echo "[demo-participant-up] restoring leader DB snapshot: $LEADER_DB_TAR"
  BACKUP_TAR="$LEADER_DB_TAR" DB_PATH="$DB_PATH" bash "$ROOT_DIR/scripts/ops/restore_db.sh"
  validate_restored_db
else
  echo "[demo-participant-up] LEADER_DB_TAR not set: using existing local DB as-is"
fi

if [[ "$RESET_NODE_SEED" == "1" ]]; then
  echo "[demo-participant-up] resetting node seed for unique participant identity"
  rm -f "$ROOT_DIR/data/node_ed25519.seed"
fi

echo "[demo-participant-up] starting follower node against leader: $LEADER_URL"
ROLE=follower \
ADVERTISE_URL="$ADVERTISE_URL" \
BIND_ADDR="$BIND_ADDR" \
PEERS="$LEADER_URL" \
LOCAL_BASE="$LOCAL_BASE" \
ADMIN_TOKEN="$ADMIN_TOKEN" \
P2P_TOKEN="$P2P_TOKEN" \
COORD_URL="$COORD_URL" \
COORD_TOKEN="$COORD_TOKEN" \
ENABLE_MINING=0 \
bash "$ROOT_DIR/scripts/ops/node_easy_up.sh"

if [[ "$RUN_AUTOPILOT" == "1" ]]; then
  echo "[demo-participant-up] running sync autopilot (loops=$AUTOPILOT_LOOPS)"
  BASE="$LOCAL_BASE" \
  ADMIN_TOKEN="$ADMIN_TOKEN" \
  LOOPS="$AUTOPILOT_LOOPS" \
  AUTO_START_MINING_WHEN_SYNCED="$AUTO_START_MINING_WHEN_SYNCED" \
  bash "$ROOT_DIR/scripts/ops/p2p_autopilot.sh"
fi

echo
echo "[demo-participant-up] done. quick checks:"
echo "curl -sS \"$LOCAL_BASE/api/status\" | jq '{tip_height,tip_hash,node_address,mining}'"
echo "curl -sS \"$LOCAL_BASE/api/p2p/sync?depth_limit=64\" | jq '{sync_needed,lag_blocks,sync_blocked,budget_reason}'"
