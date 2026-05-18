#!/usr/bin/env bash
set -euo pipefail

# One-command strict bring-up:
# - optionally starts main node and coordinator
# - runs strict preflight
# - runs strict recovery loop (which runs strict super-gate)
#
# Usage:
#   ADMIN_TOKEN=... scripts/ops/stack_strict_up.sh
# Optional:
#   START_MAIN=1 START_COORD=1
#   MAIN_ADDR=0.0.0.0:8080 COORD_ADDR=0.0.0.0:8081
#   BASE=http://127.0.0.1:8080 COORD=http://127.0.0.1:8081
#   AUTO_START_COORD=1 RETRIES=3 RETRY_SLEEP_SEC=5 ATTEMPT_SYNC=1

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[stack-strict] missing command: $1" >&2
    exit 1
  }
}

require_cmd curl
require_cmd jq

ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
if [[ -z "$ADMIN_TOKEN" ]]; then
  echo "[stack-strict] ADMIN_TOKEN (or HACKME_ADMIN_TOKEN) is required" >&2
  exit 1
fi
if [[ "$ADMIN_TOKEN" == *"..."* || "$ADMIN_TOKEN" == *"CHANGE_ME"* || "$ADMIN_TOKEN" == *"ТУТ_ПОЛНЫЙ_ТОКЕН"* ]]; then
  echo "[stack-strict] ADMIN_TOKEN looks like placeholder" >&2
  exit 1
fi

START_MAIN="${START_MAIN:-0}"
START_COORD="${START_COORD:-0}"
MAIN_ADDR="${MAIN_ADDR:-0.0.0.0:8080}"
COORD_ADDR="${COORD_ADDR:-0.0.0.0:8081}"
BASE="${BASE:-http://127.0.0.1:8080}"
COORD="${COORD:-http://127.0.0.1:8081}"

HACKME_P2P_TOKEN="${HACKME_P2P_TOKEN:-}"
HACKME_P2P_DISCOVERY="${HACKME_P2P_DISCOVERY:-1}"
HACKME_P2P_ADVERTISE_URL="${HACKME_P2P_ADVERTISE_URL:-}"
HACKME_P2P_PEERS="${HACKME_P2P_PEERS:-}"

AUTO_START_COORD="${AUTO_START_COORD:-1}"
RETRIES="${RETRIES:-3}"
RETRY_SLEEP_SEC="${RETRY_SLEEP_SEC:-5}"
ATTEMPT_SYNC="${ATTEMPT_SYNC:-1}"
RUN_ID="${RUN_ID:-stack_strict_up_$(date -u +%Y%m%dT%H%M%SZ)}"

LOG_DIR="${LOG_DIR:-$ROOT_DIR/logs/stack-strict}"
mkdir -p "$LOG_DIR"

main_pid=""
coord_pid=""

cleanup() {
  local ec=$?
  if [[ "${KEEP_RUNNING_ON_EXIT:-1}" != "1" ]]; then
    [[ -n "$main_pid" ]] && kill "$main_pid" >/dev/null 2>&1 || true
    [[ -n "$coord_pid" ]] && kill "$coord_pid" >/dev/null 2>&1 || true
    wait >/dev/null 2>&1 || true
  fi
  exit "$ec"
}
trap cleanup INT TERM EXIT

wait_http_ok() {
  local name="$1"
  local url="$2"
  local max="${3:-40}"
  local i=1
  while (( i <= max )); do
    if curl -fsS "$url" >/dev/null 2>&1; then
      echo "[stack-strict] ${name} healthy: ${url}"
      return 0
    fi
    sleep 1
    i=$((i + 1))
  done
  echo "[stack-strict] ${name} health failed: ${url}" >&2
  return 1
}

if [[ "$START_COORD" == "1" ]]; then
  echo "[stack-strict] starting coordinator ${COORD_ADDR}"
  (
    cd "$ROOT_DIR"
    HACKME_COORDINATOR_ADDR="$COORD_ADDR" \
    HACKME_COORDINATOR_ADMIN_TOKEN="$ADMIN_TOKEN" \
    go run ./cmd/coordinator
  ) >"$LOG_DIR/coordinator.log" 2>&1 &
  coord_pid=$!
  wait_http_ok "coordinator" "${COORD}/api/network/stats"
fi

if [[ "$START_MAIN" == "1" ]]; then
  echo "[stack-strict] starting main node ${MAIN_ADDR}"
  (
    cd "$ROOT_DIR"
    HACKME_BIND_ADDR="$MAIN_ADDR" \
    HACKME_ADMIN_TOKEN="$ADMIN_TOKEN" \
    HACKME_P2P_TOKEN="$HACKME_P2P_TOKEN" \
    HACKME_P2P_DISCOVERY="$HACKME_P2P_DISCOVERY" \
    HACKME_P2P_ADVERTISE_URL="$HACKME_P2P_ADVERTISE_URL" \
    HACKME_P2P_PEERS="$HACKME_P2P_PEERS" \
    HACKME_SANDBOX_LOCKED="1" \
    HACKME_FUZZ_AUTORUN="1" \
    go run .
  ) >"$LOG_DIR/main.log" 2>&1 &
  main_pid=$!
  wait_http_ok "main" "${BASE}/api/status"
fi

echo "[stack-strict] run strict preflight"
RUN_ID="${RUN_ID}_preflight" BASE="${BASE}" COORD="${COORD}" AUTO_START_COORD="${AUTO_START_COORD}" \
  scripts/ops/strict_network_preflight.sh || true

echo "[stack-strict] run strict recovery/gate"
ADMIN_TOKEN="${ADMIN_TOKEN}" BASE="${BASE}" COORD="${COORD}" RUN_ID="${RUN_ID}_recover" \
RETRIES="${RETRIES}" RETRY_SLEEP_SEC="${RETRY_SLEEP_SEC}" ATTEMPT_SYNC="${ATTEMPT_SYNC}" RUN_PREFLIGHT=0 \
  scripts/ops/fuzz_strict_recover.sh

echo "[stack-strict] strict profile reached"
echo "[stack-strict] logs:"
echo "  $LOG_DIR/main.log"
echo "  $LOG_DIR/coordinator.log"
