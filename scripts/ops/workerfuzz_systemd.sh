#!/usr/bin/env bash
set -euo pipefail
cd /opt/hackme
set -a
# shellcheck disable=SC1091
[[ -f /opt/hackme/.env.workerfuzz ]] && . /opt/hackme/.env.workerfuzz
set +a
exec /opt/hackme/bin/workerfuzz \
  -worker "${WORKER_ID:-vps-canary-fuzz-01}" \
  -timeout-ms "${WORKERFUZZ_TIMEOUT_MS:-800}"
