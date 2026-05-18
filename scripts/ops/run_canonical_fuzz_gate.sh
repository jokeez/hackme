#!/usr/bin/env bash
set -euo pipefail
# Thin wrapper: run fuzz_release_gate against a canonical HTTP base (e.g. production API).
#
# Usage:
#   ADMIN_TOKEN='…' BASE='https://hackme.tech' bash scripts/ops/run_canonical_fuzz_gate.sh
#
# Creates fuzz campaigns on TARGET — use staging unless ops-approved.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

BASE="${BASE:-}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
COORD="${COORD:-}"

if [[ -z "$ADMIN_TOKEN" ]]; then
  echo "[canonical-fuzz-gate] set ADMIN_TOKEN or HACKME_ADMIN_TOKEN" >&2
  exit 1
fi
if [[ -z "$BASE" ]]; then
  echo "[canonical-fuzz-gate] set BASE (e.g. https://hackme.tech)" >&2
  exit 1
fi

# Keep optional nested private-stage check inside fuzz_release_gate aligned with BASE.
# For loopback canonical nodes this avoids defaulting COORD to :8081.
if [[ -z "$COORD" ]]; then
  case "$BASE" in
    http://127.0.0.1:18080|http://localhost:18080)
      COORD="http://127.0.0.1:18081"
      ;;
    https://hackme.tech|http://hackme.tech)
      COORD="https://hackme.tech/pool/coordinator"
      ;;
  esac
fi
if [[ -n "$COORD" ]]; then
  export COORD_URL="$COORD"
fi

echo "[canonical-fuzz-gate] BASE=$BASE COORD=${COORD:-auto/none} (will mutate fuzz campaigns on this host)"
exec bash scripts/ops/fuzz_release_gate.sh
