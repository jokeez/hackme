#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

BASE="${BASE:-http://127.0.0.1:8080}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
DEPTH_LIMIT="${DEPTH_LIMIT:-64}"
MAX_APPLY="${MAX_APPLY:-20}"
LOOPS="${LOOPS:-60}"
SLEEP_SEC="${SLEEP_SEC:-2}"

if [[ -z "$ADMIN_TOKEN" ]]; then
  echo "[follower-sync] ADMIN_TOKEN (or HACKME_ADMIN_TOKEN) is required" >&2
  exit 1
fi

echo "[follower-sync] BASE=$BASE DEPTH_LIMIT=$DEPTH_LIMIT MAX_APPLY=$MAX_APPLY LOOPS=$LOOPS"
for i in $(seq 1 "$LOOPS"); do
  run_json="$(curl -fsS -X POST \
    -H "X-Hackme-Admin-Token: $ADMIN_TOKEN" \
    "$BASE/api/p2p/sync/run?depth_limit=$DEPTH_LIMIT&max_apply=$MAX_APPLY" || true)"
  code="$(jq -r '.code // ""' <<<"$run_json" 2>/dev/null || true)"
  applied="$(jq -r '.apply.applied // 0' <<<"$run_json" 2>/dev/null || echo "0")"
  lag="$(curl -fsS "$BASE/api/p2p/sync?depth_limit=$DEPTH_LIMIT" | jq -r '.lag_blocks // 0' 2>/dev/null || echo "0")"
  echo "[follower-sync] loop=$i applied=$applied lag=$lag code=${code:-none}"
  if [[ "$code" == "fork_detected_no_reorg_v1" ]]; then
    echo "[follower-sync] STOP: fork detected. Action: reseed follower DB from leader." >&2
    exit 2
  fi
  if [[ "$code" == "sync_apply_disabled_no_state_replay_v1" ]]; then
    echo "[follower-sync] STOP: sync apply disabled by policy (set HACKME_P2P_SYNC_STATE_REPLAY_ENABLED=1 in controlled environment)." >&2
    exit 4
  fi
  if [[ "$lag" == "0" ]]; then
    echo "[follower-sync] synced (lag=0)"
    exit 0
  fi
  sleep "$SLEEP_SEC"
done

echo "[follower-sync] loops exhausted, lag is still non-zero" >&2
exit 3
