#!/bin/bash
# Systemd digger entry for hub named fuzz workers.
# Use /bin/bash (not env) so ProtectSystem=full units still start when PATH is odd.
set -euo pipefail
ROOT="${HACKME_REPO_ROOT:-/opt/hackme}"
cd "$ROOT"
export HACKME_REPO_ROOT="$ROOT"
export COORD_URL="${COORD_URL:-http://127.0.0.1:18081}"
if [[ -z "${COORD_TOKEN:-}" && -f .secrets/hackme_coordinator_worker_token ]]; then
  COORD_TOKEN="$(tr -d '\r\n' < .secrets/hackme_coordinator_worker_token)"
fi
export COORD_TOKEN
export HACKME_WORKER_HUNT_TIMEOUT_MS="${HACKME_WORKER_HUNT_TIMEOUT_MS:-300000}"
# COORD_TOKEN is exported above: workerfuzz defaults -token from it, and keeping the
# token off argv keeps it out of /proc/<pid>/cmdline, which any local account can read
# (CWE-214 — same class as the workerpoh argv finding, which was fixed in Go but left
# this shipped unit passing the flag).
exec "$ROOT/bin/workerfuzz" -coord "$COORD_URL" -worker "${WORKER_ID:?}" -timeout-ms "${WORKERFUZZ_TIMEOUT_MS:-2000}"
