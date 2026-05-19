#!/usr/bin/env bash
# Stop duplicate pool workers on desktop and start a single GPU worker via local node API.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

DESKTOP_ENV_FILE="${DESKTOP_ENV_FILE:-$ROOT_DIR/.env.desktop}"
BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
SECRET_COORD="${SECRET_COORD:-$ROOT_DIR/.secrets/hackme_coordinator_admin_token}"

set -a
# shellcheck disable=SC1090
[[ -f "$DESKTOP_ENV_FILE" ]] && . "$DESKTOP_ENV_FILE"
set +a

if [[ -z "${HACKME_ADMIN_TOKEN:-}" ]]; then
  echo "[worker-reset] HACKME_ADMIN_TOKEN missing in $DESKTOP_ENV_FILE" >&2
  exit 2
fi
if [[ -z "${HACKME_POOL_COORDINATOR_TOKEN:-}" && -f "$SECRET_COORD" ]]; then
  export HACKME_POOL_COORDINATOR_TOKEN="$(tr -d '\r\n' <"$SECRET_COORD")"
fi
if [[ -z "${HACKME_POOL_COORDINATOR_TOKEN:-}" ]]; then
  echo "[worker-reset] set HACKME_POOL_COORDINATOR_TOKEN or create $SECRET_COORD" >&2
  exit 2
fi

echo "[worker-reset] stopping node-managed worker..."
curl -fsS -X POST "$BASE_URL/api/worker/stop" \
  -H "X-Hackme-Admin-Token: ${HACKME_ADMIN_TOKEN}" >/dev/null 2>&1 || true

echo "[worker-reset] killing stray worker_loop / workerpoh processes..."
pkill -f 'scripts/ops/worker_loop.sh' 2>/dev/null || true
pkill -f 'scripts/ops/worker_autostart.sh' 2>/dev/null || true
pkill -f 'workerpoh-opencl' 2>/dev/null || true
pkill -f 'workerpoh ' 2>/dev/null || true
sleep 2

echo "[worker-reset] clearing stale pool worker logs..."
: >"$ROOT_DIR/logs/worker_participant.log" 2>/dev/null || true

COORD_URL="${HACKME_POOL_COORDINATOR_URL:-https://hackme.tech/pool/coordinator}"
WORKER_ID="${WORKER_ID:-worker-kapa-pc}"

echo "[worker-reset] starting single worker id=$WORKER_ID coord=$COORD_URL (batch=1048576 remote-fair)"
start_json="$(python3 - "$COORD_URL" "$WORKER_ID" "${HACKME_POOL_COORDINATOR_TOKEN:-}" <<'PY'
import json, sys
coord, wid, tok = sys.argv[1:4]
body = {"coord_url": coord, "worker_id": wid, "batch_size": 1048576}
if tok.strip():
    body["coord_token"] = tok.strip()
print(json.dumps(body))
PY
)"
resp="$(curl -fsS -X POST "$BASE_URL/api/worker/start" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: ${HACKME_ADMIN_TOKEN}" \
  -d "$start_json")"
echo "$resp" | jq . 2>/dev/null || echo "$resp"

echo "[worker-reset] worker status:"
curl -fsS "$BASE_URL/api/worker/status" | jq . 2>/dev/null || true
