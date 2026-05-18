#!/usr/bin/env bash
# One-shot: build desktop node, restart with .env.desktop, verify wallet, start worker, accrual audit.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
DESKTOP_ENV="${DESKTOP_ENV_FILE:-$ROOT/.env.desktop}"
LOG_DIR="${LOG_DIR:-$ROOT/logs/desktop}"
NODE_BIN="$LOG_DIR/hackme-node-desktop"
BASE="${BASE_URL:-http://127.0.0.1:8080}"

[[ -f "$DESKTOP_ENV" ]] || { echo "[desktop-finalize] missing $DESKTOP_ENV" >&2; exit 2; }
set -a
# shellcheck disable=SC1090
. "$DESKTOP_ENV"
set +a
SECRET_COORD="${SECRET_COORD:-$ROOT/.secrets/hackme_coordinator_admin_token}"
if [[ -z "${HACKME_POOL_COORDINATOR_TOKEN:-}" && -f "$SECRET_COORD" ]]; then
  export HACKME_POOL_COORDINATOR_TOKEN="$(tr -d '\r\n' <"$SECRET_COORD")"
fi

echo "[desktop-finalize] build node"
gpu_tags=""
if pkg-config --exists OpenCL 2>/dev/null || [[ -f /usr/include/CL/cl.h ]]; then gpu_tags="opencl"; fi
if [[ -n "$gpu_tags" ]]; then
  go build -trimpath -tags "$gpu_tags" -o "$NODE_BIN" .
else
  go build -trimpath -o "$NODE_BIN" .
fi

echo "[desktop-finalize] restart node"
if [[ -f "$LOG_DIR/node.pid" ]]; then
  old_pid="$(cat "$LOG_DIR/node.pid" 2>/dev/null || true)"
  [[ -n "$old_pid" ]] && kill "$old_pid" 2>/dev/null || true
  sleep 1
fi
mkdir -p "$LOG_DIR" "$(dirname "${HACKME_WORKER_SETTLEMENT_STATE_FILE:-$ROOT/logs/desktop/data/worker_settlement_state.json}")"
nohup "$NODE_BIN" >"$LOG_DIR/node.log" 2>&1 &
echo $! >"$LOG_DIR/node.pid"
for _ in $(seq 1 40); do
  curl -fsS --max-time 5 "$BASE/api/status?lite=1" >/dev/null 2>&1 && break
  sleep 0.5
done
curl -fsS --max-time 8 "$BASE/api/status?lite=1" >/dev/null

echo "[desktop-finalize] single GPU worker"
bash "$ROOT/scripts/ops/desktop_worker_reset.sh"

echo "[desktop-finalize] accrual audit (30s growth check)"
WATCH_SEC=30 bash "$ROOT/scripts/ops/desktop_accrual_audit.sh"

echo "[desktop-finalize] DONE — dashboard: $BASE"
