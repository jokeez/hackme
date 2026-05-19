#!/usr/bin/env bash
# Generate a coordinator worker token (claim/submit only) for remote VPS miners.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT="${1:-$ROOT/.secrets/hackme_coordinator_worker_token}"
mkdir -p "$(dirname "$OUT")"
if [[ -f "$OUT" && "${FORCE:-0}" != "1" ]]; then
  echo "exists: $OUT (set FORCE=1 to overwrite)" >&2
  exit 1
fi
tok="$(openssl rand -hex 32)"
printf '%s' "$tok" >"$OUT"
chmod 600 "$OUT"
echo "wrote $OUT (${#tok} hex chars)"
echo "Hub: HACKME_COORDINATOR_WORKER_TOKEN=<same value> in coordinator env"
echo "Workers: worker_vps_deploy.sh uses this file automatically"
