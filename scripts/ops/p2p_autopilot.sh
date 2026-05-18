#!/usr/bin/env bash
set -euo pipefail

# Keeps follower node in sync with leader with minimal operator actions.
# Safe for private-network staging where sync/apply is intentionally controlled.
#
# Behavior:
# - Poll /api/p2p/sync
# - If lag > 0 and not blocked -> run /api/p2p/sync/run
# - If fork_detected_no_reorg_v1 -> optionally stop local mining and exit
# - If synced and AUTO_START_MINING_WHEN_SYNCED=1 -> starts mining once (command node needs HACKME_CHAIN_LEADER_LOCAL_POH=1)
#
# Usage:
#   BASE=http://127.0.0.1:8080 ADMIN_TOKEN=... bash scripts/ops/p2p_autopilot.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

BASE="${BASE:-http://127.0.0.1:8080}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
DEPTH_LIMIT="${DEPTH_LIMIT:-64}"
MAX_APPLY="${MAX_APPLY:-64}"
LOOPS="${LOOPS:-0}"           # 0 = infinite
SLEEP_SEC="${SLEEP_SEC:-3}"
STOP_MINING_ON_FORK="${STOP_MINING_ON_FORK:-1}"
AUTO_START_MINING_WHEN_SYNCED="${AUTO_START_MINING_WHEN_SYNCED:-0}"
# When fork_detected: set STOP_NODE_BEFORE_LOCAL_RESEED=1 and optionally RESTART_NODE_CMD to run
# scripts/ops/reseed_follower_pick_best_backup.sh (best tarball under backups/). Leader-newer snapshots
# still require VPS SSH (follower_bootstrap_from_vps.sh).
RESEED_FROM_LOCAL_BACKUPS="${RESEED_FROM_LOCAL_BACKUPS:-0}"
STOP_NODE_BEFORE_LOCAL_RESEED="${STOP_NODE_BEFORE_LOCAL_RESEED:-0}"
RESTART_NODE_CMD="${RESTART_NODE_CMD:-}"

if [[ -z "$ADMIN_TOKEN" ]]; then
  echo "[p2p-autopilot] ADMIN_TOKEN (or HACKME_ADMIN_TOKEN) is required" >&2
  exit 1
fi

echo "[p2p-autopilot] BASE=$BASE DEPTH_LIMIT=$DEPTH_LIMIT MAX_APPLY=$MAX_APPLY LOOPS=$LOOPS"
echo "[p2p-autopilot] STOP_MINING_ON_FORK=$STOP_MINING_ON_FORK AUTO_START_MINING_WHEN_SYNCED=$AUTO_START_MINING_WHEN_SYNCED"

did_autostart=0
iter=0
while true; do
  iter=$((iter + 1))
  if [[ "$LOOPS" != "0" && "$iter" -gt "$LOOPS" ]]; then
    echo "[p2p-autopilot] completed loops=$LOOPS"
    exit 0
  fi

  sync_json="$(curl -fsS "$BASE/api/p2p/sync?depth_limit=$DEPTH_LIMIT" || true)"
  enabled="$(jq -r '.enabled // false' <<<"$sync_json" 2>/dev/null || echo "false")"
  lag="$(jq -r '.lag_blocks // 0' <<<"$sync_json" 2>/dev/null || echo "0")"
  blocked="$(jq -r '.sync_blocked // false' <<<"$sync_json" 2>/dev/null || echo "false")"
  blocked_code="$(jq -r '.sync_blocked_code // ""' <<<"$sync_json" 2>/dev/null || echo "")"
  needed="$(jq -r '.sync_needed // false' <<<"$sync_json" 2>/dev/null || echo "false")"

  echo "[p2p-autopilot] loop=$iter enabled=$enabled lag=$lag needed=$needed blocked=$blocked code=${blocked_code:-none}"

  if [[ "$blocked" == "true" && "$blocked_code" == "fork_detected_no_reorg_v1" ]]; then
    if [[ "$STOP_MINING_ON_FORK" == "1" ]]; then
      curl -fsS -X POST "$BASE/api/mining/stop" -H "X-Hackme-Admin-Token: $ADMIN_TOKEN" >/dev/null 2>&1 || true
      echo "[p2p-autopilot] mining stopped because fork detected"
    fi
    if [[ "$RESEED_FROM_LOCAL_BACKUPS" == "1" && "$STOP_NODE_BEFORE_LOCAL_RESEED" == "1" ]]; then
      echo "[p2p-autopilot] running local backup reseed (pick highest tip tarball)"
      curl -fsS -X POST "$BASE/api/worker/stop" -H "X-Hackme-Admin-Token: $ADMIN_TOKEN" >/dev/null 2>&1 || true
      sleep 1
      pkill -TERM hackme-node 2>/dev/null || true
      sleep 2
      pkill -KILL hackme-node 2>/dev/null || true
      sleep 1
      if bash "$ROOT_DIR/scripts/ops/reseed_follower_pick_best_backup.sh"; then
        echo "[p2p-autopilot] restore OK — restarting node if RESTART_NODE_CMD set"
        if [[ -n "$RESTART_NODE_CMD" ]]; then
          # shellcheck disable=SC2086
          eval $RESTART_NODE_CMD &
          sleep 4
          echo "[p2p-autopilot] re-enter sync loop after local reseed"
          sleep "$SLEEP_SEC"
          continue
        fi
        echo "[p2p-autopilot] set RESTART_NODE_CMD to auto-start hackme-node after restore" >&2
      fi
    fi
    echo "[p2p-autopilot] STOP: fork_detected_no_reorg_v1 -> reseed follower DB from leader (or refresh backups/*.tar.gz from VPS)" >&2
    exit 2
  fi

  if [[ "$enabled" == "true" && "$blocked" != "true" && "$lag" != "0" ]]; then
    run_json="$(curl -fsS -X POST \
      -H "X-Hackme-Admin-Token: $ADMIN_TOKEN" \
      "$BASE/api/p2p/sync/run?depth_limit=$DEPTH_LIMIT&max_apply=$MAX_APPLY" || true)"
    code="$(jq -r '.code // ""' <<<"$run_json" 2>/dev/null || true)"
    applied="$(jq -r '.apply.applied // 0' <<<"$run_json" 2>/dev/null || echo "0")"
    echo "[p2p-autopilot] sync/run applied=$applied code=${code:-none}"
  fi

  if [[ "$AUTO_START_MINING_WHEN_SYNCED" == "1" && "$did_autostart" == "0" ]]; then
    lag_now="$(curl -fsS "$BASE/api/p2p/sync?depth_limit=$DEPTH_LIMIT" | jq -r '.lag_blocks // 0' 2>/dev/null || echo "1")"
    blocked_now="$(curl -fsS "$BASE/api/p2p/sync?depth_limit=$DEPTH_LIMIT" | jq -r '.sync_blocked // false' 2>/dev/null || echo "true")"
    if [[ "$lag_now" == "0" && "$blocked_now" != "true" ]]; then
      if curl -fsS -X POST "$BASE/api/mining/start" -H "X-Hackme-Admin-Token: $ADMIN_TOKEN" >/dev/null 2>&1; then
        did_autostart=1
        echo "[p2p-autopilot] mining auto-started after sync"
      fi
    fi
  fi

  sleep "$SLEEP_SEC"
done
