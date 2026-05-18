#!/usr/bin/env bash
# Desktop "final" preflight: rebuild node, restart, core fuzz + from_code preflight smoke.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
DESKTOP_ENV="${DESKTOP_ENV_FILE:-$ROOT/.env.desktop}"
[[ -f "$DESKTOP_ENV" ]] || { echo "missing $DESKTOP_ENV" >&2; exit 2; }
set -a; # shellcheck disable=SC1090
. "$DESKTOP_ENV"
set +a
ADMIN_TOKEN="${HACKME_ADMIN_TOKEN:-}"
BASE="${BASE_URL:-http://127.0.0.1:8080}"
LOG_DIR="${LOG_DIR:-$ROOT/logs/desktop}"
NODE_BIN="$LOG_DIR/hackme-node-desktop"

echo "[desktop-final] build node"
gpu_tags=""
if pkg-config --exists OpenCL 2>/dev/null || [[ -f /usr/include/CL/cl.h ]]; then gpu_tags="opencl"; fi
if [[ -n "$gpu_tags" ]]; then
  go build -trimpath -tags "$gpu_tags" -o "$NODE_BIN" .
else
  go build -trimpath -o "$NODE_BIN" .
fi

echo "[desktop-final] restart node"
bash "$ROOT/scripts/ops/desktop_mode_stop.sh"
nohup "$NODE_BIN" >"$LOG_DIR/node.log" 2>&1 &
echo $! >"$LOG_DIR/node.pid"
for _ in $(seq 1 40); do
  curl -fsS "$BASE/api/status" >/dev/null 2>&1 && break
  sleep 0.5
done

echo "[desktop-final] from_code preflight (iostream rejected)"
resp="$(curl -sS -X POST -H "X-Hackme-Admin-Token: $ADMIN_TOKEN" -H "Content-Type: application/json" \
  -d '{"language":"cpp","code":"#include <iostream>\nint main(){}","reward_hmc":1,"difficulty_score":1,"target_solves":1}' \
  "$BASE/api/tasks/from_code")"
echo "$resp" | jq -e '.code == "app_not_task_code"' >/dev/null

echo "[desktop-final] fuzz_runtime_gate"
ADMIN_TOKEN="$ADMIN_TOKEN" BASE="$BASE" bash "$ROOT/scripts/tests/fuzz_runtime_gate.sh"

echo "[desktop-final] PASS — node at $BASE log=$LOG_DIR/node.log"
