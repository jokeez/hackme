#!/usr/bin/env bash
set -euo pipefail

# predeploy_gate.sh
# One-command practical gate before public deploy.
#
# Includes:
# 1) go test ./...
# 2) canonical proxy parity smoke
# 3) worker-mode health check
#
# Optional:
#   RUN_CORE_GATE=1 to include scripts/ops/core_gate.sh
#
# Usage:
#   ADMIN_TOKEN=... LOCAL_BASE=http://127.0.0.1:8080 VPS_BASE=http://<vps>:18080 COORD_URL=http://<vps>:18081 \
#   REQUIRE_WALLET_SOURCE=1 bash scripts/ops/predeploy_gate.sh

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

LOCAL_BASE="${LOCAL_BASE:-http://127.0.0.1:8080}"
VPS_BASE="${VPS_BASE:-http://127.0.0.1:18080}"
COORD_URL="${COORD_URL:-http://127.0.0.1:18081}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
REQUIRE_WALLET_SOURCE="${REQUIRE_WALLET_SOURCE:-1}"
RUN_CORE_GATE="${RUN_CORE_GATE:-0}"
RUN_HYBRID_SIGNER_SMOKE="${RUN_HYBRID_SIGNER_SMOKE:-0}"
RUN_ID="${RUN_ID:-predeploy_gate_$(date -u +%Y%m%dT%H%M%SZ)}"
SKIP_GO_TEST="${SKIP_GO_TEST:-0}"

if [[ -z "$ADMIN_TOKEN" ]]; then
  echo "[predeploy-gate] ADMIN_TOKEN (or HACKME_ADMIN_TOKEN) is required" >&2
  exit 1
fi

echo "[predeploy-gate] RUN_ID=$RUN_ID"
echo "[predeploy-gate] LOCAL_BASE=$LOCAL_BASE VPS_BASE=$VPS_BASE COORD_URL=$COORD_URL"

if [[ "$SKIP_GO_TEST" == "1" ]]; then
	echo "[predeploy-gate] step 1/4: go test ./... (skipped SKIP_GO_TEST=1)"
else
	echo "[predeploy-gate] step 1/4: go test ./..."
	go test ./... -count=1
fi

echo "[predeploy-gate] step 2/4: canonical proxy smoke"
LOCAL_BASE="$LOCAL_BASE" VPS_BASE="$VPS_BASE" REQUIRE_WALLET_SOURCE="$REQUIRE_WALLET_SOURCE" \
  bash scripts/tests/canonical_proxy_smoke.sh

echo "[predeploy-gate] step 3/4: worker-mode health"
VPS_BASE="$VPS_BASE" COORD_URL="$COORD_URL" LOCAL_BASE="$LOCAL_BASE" REQUIRE_WORKER_ACTIVITY=1 \
  bash scripts/ops/worker_mode_health.sh

echo "[predeploy-gate] step 4/4: optional core gate"
if [[ "$RUN_CORE_GATE" == "1" ]]; then
  RUN_ID="${RUN_ID}_core" BASE="$LOCAL_BASE" COORD="$COORD_URL" ADMIN_TOKEN="$ADMIN_TOKEN" \
    bash scripts/ops/core_gate.sh
else
  echo "[predeploy-gate] skipped (set RUN_CORE_GATE=1 to enable full matrix)"
fi

if [[ "$RUN_HYBRID_SIGNER_SMOKE" == "1" ]]; then
  echo "[predeploy-gate] hybrid signer smoke"
  COORD_URL="$COORD_URL" COORD_TOKEN="$ADMIN_TOKEN" REQUIRE_HYBRID=1 \
    bash scripts/tests/hybrid_signer_smoke.sh
else
  echo "[predeploy-gate] hybrid signer smoke skipped (set RUN_HYBRID_SIGNER_SMOKE=1)"
fi

echo "[predeploy-gate] PASS"
