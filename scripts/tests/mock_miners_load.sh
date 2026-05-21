#!/usr/bin/env bash
# Run mock virtual miners against a coordinator and verify pool stats.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT_DIR/scripts/tests/common.sh"

require_cmd python3
require_cmd curl
require_cmd jq

COORD="${COORD:-http://127.0.0.1:8081}"
NODE_BASE="${NODE_BASE:-http://127.0.0.1:8080}"
WORKERS="${WORKERS:-15}"
DURATION_SEC="${DURATION_SEC:-45}"
BATCH_SIZE="${BATCH_SIZE:-512}"
COORD_ADMIN_TOKEN="${COORD_ADMIN_TOKEN:-${ADMIN_TOKEN:-}}"

if [[ -z "$COORD_ADMIN_TOKEN" ]]; then
  if [[ -f "$ROOT_DIR/.secrets/hackme_coordinator_admin_token" ]]; then
    COORD_ADMIN_TOKEN="$(tr -d '\r\n' <"$ROOT_DIR/.secrets/hackme_coordinator_admin_token")"
  fi
fi

export COORD COORD_ADMIN_TOKEN WORKERS DURATION_SEC BATCH_SIZE NODE_BASE

echo "[mock-miners] coord=$COORD workers=$WORKERS duration=${DURATION_SEC}s"
python3 "$ROOT_DIR/scripts/tests/tools/mock_miners_pool.py" \
  --coord "$COORD" \
  --workers "$WORKERS" \
  --duration "$DURATION_SEC" \
  --batch "$BATCH_SIZE" \
  ${NODE_BASE:+--node "$NODE_BASE"}
