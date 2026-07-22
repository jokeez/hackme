#!/usr/bin/env bash
# Per-instance launcher for bootstrap workerfuzz (sourced by systemd EnvironmentFile).
set -euo pipefail
INSTALL="${BOOTSTRAP_INSTALL:-/opt/hackme-bootstrap}"
BIN="${WORKERFUZZ_BIN:-$INSTALL/bin/workerfuzz}"
exec "$BIN" \
  -worker "${WORKER_ID:?WORKER_ID required}" \
  -timeout-ms "${WORKERFUZZ_TIMEOUT_MS:-800}"
