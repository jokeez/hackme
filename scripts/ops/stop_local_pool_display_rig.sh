#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DESKTOP_ENV="${DESKTOP_ENV_FILE:-$ROOT/.env.desktop}"
if [[ -f "$DESKTOP_ENV" ]]; then
  set -a
  # shellcheck disable=SC1090
  . "$DESKTOP_ENV"
  set +a
  if [[ -n "${HACKME_ADMIN_TOKEN:-}" ]]; then
    curl -fsS -X POST "http://127.0.0.1:8080/api/worker/stop" \
      -H "X-Hackme-Admin-Token: ${HACKME_ADMIN_TOKEN}" >/dev/null 2>&1 || true
  fi
fi
pkill -f 'workerpoh.*worker-kapa-rig-' 2>/dev/null || true
pkill -f 'workerpoh.*worker-kapa-fair-' 2>/dev/null || true
echo "[display-rig] stopped local cosmetic pool workers"
