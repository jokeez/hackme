#!/usr/bin/env bash
# Ephemeral isolated hackme-node for gates/smokes (127.0.0.1 by default).
#
# Usage:
#   source scripts/tests/ephemeral_hackme_node.sh
#   ephemeral_hackme_node_start
#   # … tests with BASE=$EPHEMERAL_BASE ADMIN_TOKEN=$EPHEMERAL_ADMIN_TOKEN
#   ephemeral_hackme_node_stop
#
# Or: ephemeral_hackme_node_start && trap ephemeral_hackme_node_stop EXIT
set -euo pipefail

_EPHEMERAL_NODE_ROOT="${_EPHEMERAL_NODE_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")/../.." && pwd)}"
EPHEMERAL_BIND="${EPHEMERAL_BIND:-127.0.0.1:18099}"
EPHEMERAL_PORT="${EPHEMERAL_PORT:-${EPHEMERAL_BIND##*:}}"
EPHEMERAL_BASE="${EPHEMERAL_BASE:-http://127.0.0.1:${EPHEMERAL_PORT}}"
EPHEMERAL_DATA_DIR="${EPHEMERAL_DATA_DIR:-}"
EPHEMERAL_NODE_PID="${EPHEMERAL_NODE_PID:-}"
EPHEMERAL_ADMIN_TOKEN="${EPHEMERAL_ADMIN_TOKEN:-}"
EPHEMERAL_LOG="${EPHEMERAL_LOG:-}"

ephemeral_hackme_node_start() {
  if curl -fsS --max-time 3 "${EPHEMERAL_BASE}/api/status?lite=1" >/dev/null 2>&1; then
    return 0
  fi
  if ss -ltn 2>/dev/null | awk -v p=":${EPHEMERAL_PORT}" '$4 ~ (p "$") {found=1} END{exit (found?0:1)}'; then
    echo "[ephemeral-node] port ${EPHEMERAL_PORT} in use but /api/status down" >&2
    return 1
  fi

  EPHEMERAL_DATA_DIR="${EPHEMERAL_DATA_DIR:-$(mktemp -d /tmp/hackme-ephemeral-XXXXXX)}"
  mkdir -p "$EPHEMERAL_DATA_DIR"
  if [[ -z "$EPHEMERAL_ADMIN_TOKEN" ]]; then
    if command -v openssl >/dev/null 2>&1; then
      EPHEMERAL_ADMIN_TOKEN="HMC_ADMIN_$(openssl rand -hex 16)"
    else
      EPHEMERAL_ADMIN_TOKEN="HMC_ADMIN_$(printf '%x%x' "$RANDOM" "$RANDOM")"
    fi
  fi
  EPHEMERAL_LOG="${EPHEMERAL_LOG:-$_EPHEMERAL_NODE_ROOT/logs/ephemeral_node_${EPHEMERAL_PORT}_$(date -u +%Y%m%dT%H%M%SZ).log}"
  mkdir -p "$(dirname "$EPHEMERAL_LOG")"

  BIN="$_EPHEMERAL_NODE_ROOT/hackme-node"
  if [[ ! -x "$BIN" ]]; then
    (cd "$_EPHEMERAL_NODE_ROOT" && go build -o "$BIN" .)
  fi

  nohup env \
    HACKME_DATA_DIR="$EPHEMERAL_DATA_DIR" \
    HACKME_BIND_ADDR="$EPHEMERAL_BIND" \
    HACKME_ADMIN_TOKEN="$EPHEMERAL_ADMIN_TOKEN" \
    HACKME_FUZZ_AUTORUN=0 \
    HACKME_CHAIN_LEADER_LOCAL_POH=0 \
    HACKME_DESKTOP_MODE=1 \
    "$BIN" >"$EPHEMERAL_LOG" 2>&1 &
  EPHEMERAL_NODE_PID=$!
  export EPHEMERAL_NODE_PID EPHEMERAL_ADMIN_TOKEN EPHEMERAL_BASE EPHEMERAL_DATA_DIR EPHEMERAL_LOG

  for _ in $(seq 1 60); do
    if curl -fsS --max-time 2 "${EPHEMERAL_BASE}/api/status?lite=1" >/dev/null 2>&1; then
      echo "[ephemeral-node] up ${EPHEMERAL_BASE} pid=${EPHEMERAL_NODE_PID} data=${EPHEMERAL_DATA_DIR}" >&2
      if [[ "${EPHEMERAL_GENESIS_MINT:-1}" == "1" ]]; then
        curl -fsS --max-time 30 -X POST "${EPHEMERAL_BASE}/api/genesis" \
          -H "X-Hackme-Admin-Token: ${EPHEMERAL_ADMIN_TOKEN}" \
          -H "Content-Type: application/json" -d '{}' >/dev/null 2>&1 || true
      fi
      return 0
    fi
    if ! kill -0 "$EPHEMERAL_NODE_PID" 2>/dev/null; then
      echo "[ephemeral-node] process exited early; log tail:" >&2
      tail -30 "$EPHEMERAL_LOG" >&2 || true
      return 1
    fi
    sleep 0.5
  done
  echo "[ephemeral-node] timeout waiting for ${EPHEMERAL_BASE}" >&2
  return 1
}

ephemeral_hackme_node_stop() {
  if [[ -n "${EPHEMERAL_NODE_PID:-}" ]] && kill -0 "$EPHEMERAL_NODE_PID" 2>/dev/null; then
    kill "$EPHEMERAL_NODE_PID" 2>/dev/null || true
    wait "$EPHEMERAL_NODE_PID" 2>/dev/null || true
  fi
  EPHEMERAL_NODE_PID=""
}
