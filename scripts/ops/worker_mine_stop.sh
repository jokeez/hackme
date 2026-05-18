#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

LOCAL_BASE="${LOCAL_BASE:-http://127.0.0.1:8080}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"

echo "[worker-mine-stop] stopping worker loop"
pkill -f "scripts/ops/worker_loop.sh" >/dev/null 2>&1 || true

if [[ -n "$ADMIN_TOKEN" ]]; then
  echo "[worker-mine-stop] ensuring local node mining OFF"
  curl -fsS -X POST "${LOCAL_BASE}/api/mining/stop" \
    -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" >/dev/null 2>&1 || true
fi

echo "[worker-mine-stop] done"
