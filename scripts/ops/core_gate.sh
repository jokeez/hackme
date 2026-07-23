#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

RUN_ID="${RUN_ID:-core_gate_$(date -u +%Y%m%dT%H%M%SZ)}"
BASE="${BASE:-http://127.0.0.1:8080}"
COORD="${COORD:-http://127.0.0.1:8081}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
P2P_MAX_UNSTABLE="${P2P_MAX_UNSTABLE:-1}"
P2P_MAX_BAD="${P2P_MAX_BAD:-1}"
P2P_REQUIRE_HEALTHY="${P2P_REQUIRE_HEALTHY:-0}"

if [[ -z "$ADMIN_TOKEN" ]]; then
  echo "[core-gate] ADMIN_TOKEN (or HACKME_ADMIN_TOKEN) is required" >&2
  exit 1
fi
if [[ "$ADMIN_TOKEN" == *"..."* || "$ADMIN_TOKEN" == *"PUT_FULL_TOKEN_HERE"* || "$ADMIN_TOKEN" == *"CHANGE_ME"* ]]; then
  echo "[core-gate] ADMIN_TOKEN looks like placeholder; set real token" >&2
  exit 1
fi

cd "$ROOT_DIR"
echo "[core-gate] RUN_ID=$RUN_ID BASE=$BASE COORD=$COORD P2P_MAX_UNSTABLE=$P2P_MAX_UNSTABLE P2P_MAX_BAD=$P2P_MAX_BAD P2P_REQUIRE_HEALTHY=$P2P_REQUIRE_HEALTHY"
MODE=full RUN_ID="$RUN_ID" BASE="$BASE" COORD="$COORD" ADMIN_TOKEN="$ADMIN_TOKEN" P2P_MAX_UNSTABLE="$P2P_MAX_UNSTABLE" P2P_MAX_BAD="$P2P_MAX_BAD" P2P_REQUIRE_HEALTHY="$P2P_REQUIRE_HEALTHY" scripts/tests/run_daily.sh
jq '.' "reports/tests/$RUN_ID/summary_all.json"
echo "[core-gate] PASS if total_fails == 0"

