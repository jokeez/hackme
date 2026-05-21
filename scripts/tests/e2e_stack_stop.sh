#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
LOG_DIR="${E2E_LOG_DIR:-$ROOT_DIR/logs/e2e}"
for f in coordinator node; do
  pf="$LOG_DIR/$f.pid"
  if [[ -f "$pf" ]]; then
    pid="$(cat "$pf" 2>/dev/null || true)"
    kill "$pid" 2>/dev/null || true
    rm -f "$pf"
  fi
done
