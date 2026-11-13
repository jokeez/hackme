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

# One ledger only. Without an explicit STATE_DB, workersettle derives a DB beside
# STATE_FILE (worker_settlement_state.db) with separate watermarks and re-pays accrual
# another ledger already settled (2026-10-29..11-12: ~1153 HMC paid twice). Refuse.
if [[ -z "${STATE_DB:-}${HACKME_WORKER_SETTLEMENT_DB:-}" ]]; then
  echo "[settle-workers] FATAL: STATE_DB is not set — refusing to run on a derived ledger (source .env.settlement)." >&2
  exit 1
fi
if [[ -n "$SETTLE_BIN" && -x "$SETTLE_BIN" ]]; then
  exec "$SETTLE_BIN" "$@"
fi

echo "[settle-workers] FATAL: missing workersettle binary (expected $ROOT_DIR/bin/workersettle)." >&2
echo "[settle-workers] Build: go build -o bin/workersettle ./cmd/workersettle" >&2
exit 1
