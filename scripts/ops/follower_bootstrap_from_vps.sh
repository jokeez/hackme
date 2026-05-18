#!/usr/bin/env bash
set -euo pipefail

# One-shot follower bootstrap from a live VPS leader:
# 1) create fresh backup on VPS
# 2) copy backup tar to local machine
# 3) restore local DB + start follower in one flow

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[follower-bootstrap] missing command: $1" >&2
    exit 1
  }
}

require_cmd bash
require_cmd ssh
require_cmd scp
require_cmd ls

VPS_SSH="${VPS_SSH:-}"
VPS_PROJECT_DIR="${VPS_PROJECT_DIR:-/opt/hackme}"
VPS_DB_PATH="${VPS_DB_PATH:-/opt/hackme/data/hackme.db}"
LOCAL_BACKUP_DIR="${LOCAL_BACKUP_DIR:-$ROOT_DIR/backups}"
LEADER_URL="${LEADER_URL:-}"
ADVERTISE_URL="${ADVERTISE_URL:-}"
BIND_ADDR="${BIND_ADDR:-0.0.0.0:8080}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
P2P_TOKEN="${P2P_TOKEN:-${HACKME_P2P_TOKEN:-}}"
RUN_AUTOPILOT="${RUN_AUTOPILOT:-0}"
AUTO_START_MINING_WHEN_SYNCED="${AUTO_START_MINING_WHEN_SYNCED:-0}"
RESET_NODE_SEED="${RESET_NODE_SEED:-1}"

if [[ -z "$VPS_SSH" ]]; then
  echo "[follower-bootstrap] VPS_SSH is required (e.g. root@132.243.112.100)" >&2
  exit 1
fi
if [[ -z "$LEADER_URL" ]]; then
  echo "[follower-bootstrap] LEADER_URL is required (e.g. http://132.243.112.100:18080)" >&2
  exit 1
fi
if [[ -z "$ADVERTISE_URL" ]]; then
  echo "[follower-bootstrap] ADVERTISE_URL is required (e.g. http://192.168.1.113:8080)" >&2
  exit 1
fi
if [[ -z "$ADMIN_TOKEN" || -z "$P2P_TOKEN" ]]; then
  echo "[follower-bootstrap] ADMIN_TOKEN and P2P_TOKEN are required" >&2
  exit 1
fi

if [[ -z "${COORD_TOKEN:-}" ]]; then
  echo "[follower-bootstrap] COORD_TOKEN unset; reading coordinator admin token from VPS (SSH)"
  # Coordinator listens as process name `coordinator`; token is HACKME_COORDINATOR_ADMIN_TOKEN (not HACKME_ADMIN_TOKEN).
  # Avoid pgrep -f …coord… — it matches operator shells.
  COORD_TOKEN="$(ssh "$VPS_SSH" 'c="$(pgrep -xo coordinator 2>/dev/null | awk "{print \$1}")"; test -n "$c" && tr "\0" "\n" </proc/$c/environ 2>/dev/null | grep -m1 ^HACKME_COORDINATOR_ADMIN_TOKEN= | cut -d= -f2-' | tr -d '\r')"
  if [[ -z "${COORD_TOKEN:-}" ]]; then
    COORD_TOKEN="$(ssh "$VPS_SSH" 'hn=$(pgrep -xo hackme-node 2>/dev/null | awk "{print \$1}"); test -n "$hn" && tr "\0" "\n" </proc/$hn/environ | grep -m1 ^HACKME_POOL_COORDINATOR_TOKEN= | cut -d= -f2-' | tr -d '\r')"
  fi
  if [[ -z "${COORD_TOKEN:-}" ]]; then
    COORD_TOKEN="$(ssh "$VPS_SSH" 'hn=$(pgrep -xo hackme-node 2>/dev/null | awk "{print \$1}"); test -n "$hn" && tr "\0" "\n" </proc/$hn/environ | grep -m1 ^HACKME_ADMIN_TOKEN= | cut -d= -f2-' | tr -d '\r')"
  fi
  export COORD_TOKEN
fi

echo "[follower-bootstrap] building minersign helper (hybrid signed submits)"
(cd "$ROOT_DIR" && go build -o minersign ./cmd/minersign)

mkdir -p "$LOCAL_BACKUP_DIR"

echo "[follower-bootstrap] creating fresh DB backup on VPS: $VPS_SSH"
remote_tar="$(ssh "$VPS_SSH" "cd \"$VPS_PROJECT_DIR\" && DB_PATH=\"$VPS_DB_PATH\" BACKUP_DIR=\"$VPS_PROJECT_DIR/backups\" bash scripts/ops/backup_db.sh >/dev/null && ls -1t \"$VPS_PROJECT_DIR\"/backups/hackme-db-*.tar.gz | head -n1")"
remote_tar="$(printf '%s' "$remote_tar" | tr -d '\r' | xargs)"
if [[ -z "$remote_tar" ]]; then
  echo "[follower-bootstrap] failed to detect remote backup tar path" >&2
  exit 1
fi
echo "[follower-bootstrap] latest remote tar: $remote_tar"

local_tar="$LOCAL_BACKUP_DIR/$(basename "$remote_tar")"
echo "[follower-bootstrap] downloading snapshot to: $local_tar"
scp "$VPS_SSH:$remote_tar" "$local_tar"

echo "[follower-bootstrap] launching local follower from fresh snapshot"
COORD_TOKEN="${COORD_TOKEN:-}" \
ROLE=follower \
LEADER_URL="$LEADER_URL" \
ADVERTISE_URL="$ADVERTISE_URL" \
BIND_ADDR="$BIND_ADDR" \
ADMIN_TOKEN="$ADMIN_TOKEN" \
P2P_TOKEN="$P2P_TOKEN" \
LEADER_DB_TAR="$local_tar" \
RESET_NODE_SEED="$RESET_NODE_SEED" \
RUN_AUTOPILOT="$RUN_AUTOPILOT" \
AUTO_START_MINING_WHEN_SYNCED="$AUTO_START_MINING_WHEN_SYNCED" \
bash "$ROOT_DIR/scripts/ops/demo_participant_up.sh"

