#!/usr/bin/env bash
# Thin wrapper around Go settler (cmd/workersettle).
# Legacy bash settler was removed from git (scripts/archive is local-only).
# Build: go build -o bin/workersettle ./cmd/workersettle
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SETTLE_BIN="${SETTLE_BIN:-}"
if [[ -z "$SETTLE_BIN" ]]; then
  for c in "$ROOT_DIR/bin/workersettle" "$ROOT_DIR/workersettle"; do
    if [[ -x "$c" ]]; then
      SETTLE_BIN="$c"
      break
    fi
  done
fi

if [[ -n "$SETTLE_BIN" && -x "$SETTLE_BIN" ]]; then
  exec "$SETTLE_BIN" "$@"
fi

echo "[settle-workers] FATAL: missing workersettle binary (expected $ROOT_DIR/bin/workersettle)." >&2
echo "[settle-workers] Build: go build -o bin/workersettle ./cmd/workersettle" >&2
exit 1
