#!/usr/bin/env bash
# Phase 1 operator gate: VPS settlement + public probes + security smokes.
#
#   bash scripts/ops/operator_phase1_gate.sh
#   NODE_SSH=hackme-vps PUBLIC_BASE=https://hackme.tech bash scripts/ops/operator_phase1_gate.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
PUBLIC_BASE="${PUBLIC_BASE:-https://hackme.tech}"
NODE_SSH="${NODE_SSH:-hackme-vps}"
export PUBLIC_BASE PUBLIC_RO_BASE="$PUBLIC_BASE" BASE="$PUBLIC_BASE" CURL_MAX_TIME="${CURL_MAX_TIME:-45}"

run() {
  echo ""
  echo "======== $(date -u +%Y-%m-%dT%H:%M:%SZ) $* ========"
  "$@"
}

run bash scripts/ops/mps_listing_readiness.sh --vps
run ssh -o BatchMode=yes "$NODE_SSH" 'sudo bash /opt/hackme/scripts/ops/vps_host_sanity.sh'
run ssh -o BatchMode=yes "$NODE_SSH" 'bash /opt/hackme/scripts/ops/settlement_healthcheck.sh'
run env CURL_MAX=60 bash scripts/ops/exchange_listing_smoke.sh
run bash scripts/ops/public_release_readiness.sh
run bash scripts/tests/redteam_surface_smoke.sh
run CURL_MAX_TIME=45 bash scripts/tests/security_assertions.sh
run bash scripts/tests/public_site_smoke.sh
run bash scripts/tests/transfers_matrix.sh
COORD="${COORD:-$PUBLIC_BASE/pool/coordinator}" COORD_ADMIN_TOKEN="${COORD_ADMIN_TOKEN:-$(tr -d '\r\n' <"${ROOT}/.secrets/hackme_coordinator_admin_token" 2>/dev/null || true)}" \
  bash scripts/tests/coordinator_matrix.sh

echo ""
echo "[phase1-gate] ALL STEPS PASS"
