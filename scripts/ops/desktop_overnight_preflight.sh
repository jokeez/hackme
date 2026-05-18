#!/usr/bin/env bash
# Desktop preflight before overnight soak: build, tests, node health, single worker.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
DESKTOP_ENV="${DESKTOP_ENV_FILE:-$ROOT/.env.desktop}"
LOG_DIR="${LOG_DIR:-$ROOT/logs/desktop}"
NODE_BIN="$LOG_DIR/hackme-node-desktop"
BASE="${BASE_URL:-http://127.0.0.1:8080}"

[[ -f "$DESKTOP_ENV" ]] || { echo "[overnight-preflight] missing $DESKTOP_ENV" >&2; exit 2; }
set -a
# shellcheck disable=SC1090
. "$DESKTOP_ENV"
set +a
export HACKME_POOL_COORDINATOR_TOKEN="${HACKME_POOL_COORDINATOR_TOKEN:-$(tr -d '\r\n' <"$ROOT/.secrets/hackme_coordinator_admin_token" 2>/dev/null || true)}"
ADMIN_TOKEN="${HACKME_ADMIN_TOKEN:-}"

echo "[overnight-preflight] go test -short (root packages, isolated env)"
(
  unset HACKME_PUBLIC_AUTHORITY_BASE HACKME_DESKTOP_MODE HACKME_CANONICAL_CHAIN_URL \
    HACKME_P2P_PEERS HACKME_POOL_COORDINATOR_URL HACKME_CHAIN_LEADER_LOCAL_POH 2>/dev/null || true
  go test -short -count=1 ./...
)

echo "[overnight-preflight] build desktop node"
gpu_tags=""
if pkg-config --exists OpenCL 2>/dev/null || [[ -f /usr/include/CL/cl.h ]]; then gpu_tags="opencl"; fi
if [[ -n "$gpu_tags" ]]; then
  go build -trimpath -tags "$gpu_tags" -o "$NODE_BIN" .
else
  go build -trimpath -o "$NODE_BIN" .
fi

if ! curl -fsS --max-time 5 "$BASE/api/status?lite=1" >/dev/null 2>&1; then
  echo "[overnight-preflight] starting node"
  mkdir -p "$LOG_DIR"
  nohup "$NODE_BIN" >"$LOG_DIR/hackme-node.log" 2>&1 &
  echo $! >"$LOG_DIR/node.pid"
fi
for _ in $(seq 1 60); do
  curl -fsS --max-time 8 "$BASE/api/status?lite=1" >/dev/null 2>&1 && break
  sleep 0.5
done
curl -fsS --max-time 8 "$BASE/api/status?lite=1" >/dev/null || {
  echo "[overnight-preflight] node not healthy at $BASE" >&2
  exit 1
}

echo "[overnight-preflight] fuzz_runtime_gate"
ADMIN_TOKEN="$ADMIN_TOKEN" BASE="$BASE" bash "$ROOT/scripts/tests/fuzz_runtime_gate.sh"

echo "[overnight-preflight] worker reset (single GPU worker)"
bash "$ROOT/scripts/ops/desktop_worker_reset.sh"

echo "[overnight-preflight] PASS"
