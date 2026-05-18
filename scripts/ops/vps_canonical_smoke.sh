#!/usr/bin/env bash
# Full API/consistency smoke against the canonical VPS node + coordinator (requires SSH to VPS for tokens).
#
# Prereq: passwordless SSH (see ~/.ssh/config Host hackme-vps).
#
# Usage:
#   bash scripts/ops/vps_canonical_smoke.sh
# Optional:
#   VPS_SSH=hackme-vps NODE_BASE=http://132.243.112.100:18080 COORD_BASE=http://132.243.112.100:18081

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[vps-canonical-smoke] missing: $1" >&2
    exit 1
  }
}

require_cmd ssh
require_cmd curl
require_cmd jq
require_cmd bash

VPS_SSH="${VPS_SSH:-hackme-vps}"
NODE_BASE="${NODE_BASE:-http://132.243.112.100:18080}"
COORD_BASE="${COORD_BASE:-http://132.243.112.100:18081}"
REMOTE_ENV="${REMOTE_ENV:-/opt/hackme/.env.vps}"
REMOTE_COORD_ENV="${REMOTE_COORD_ENV:-/opt/hackme/.env.coord}"

BASE_RID="$(date -u +%Y%m%dT%H%M%SZ)"

fetch_env_kv() {
  local key="$1"
  ssh -o BatchMode=yes "$VPS_SSH" "grep -E '^${key}=' \"$REMOTE_ENV\" 2>/dev/null | head -1 | cut -d= -f2- | tr -d '\"'"
}

fetch_coord_kv() {
  local key="$1"
  ssh -o BatchMode=yes "$VPS_SSH" "grep -E '^${key}=' \"$REMOTE_COORD_ENV\" 2>/dev/null | head -1 | cut -d= -f2- | tr -d '\"'"
}

echo "[vps-canonical-smoke] ssh probe $VPS_SSH"
ssh -o BatchMode=yes "$VPS_SSH" "hostname"

ADMIN_TOKEN="$(fetch_env_kv HACKME_ADMIN_TOKEN)"
P2P_TOKEN="$(fetch_env_kv HACKME_P2P_TOKEN)"
COORD_ADMIN_TOKEN="$(fetch_coord_kv HACKME_COORDINATOR_ADMIN_TOKEN)"

if [[ -z "$ADMIN_TOKEN" ]]; then
  echo "[vps-canonical-smoke] could not read HACKME_ADMIN_TOKEN from $VPS_SSH:$REMOTE_ENV" >&2
  exit 1
fi

export ADMIN_TOKEN
export HACKME_ADMIN_TOKEN="$ADMIN_TOKEN"

echo "[vps-canonical-smoke] go test ./..."
go test ./...

echo "[vps-canonical-smoke] read-only probes"
curl -fsS "$NODE_BASE/api/status" | jq '{tip_height,mining,economics:.economics.total_minted_hmc}'
curl -fsS "$NODE_BASE/api/global/metrics" | jq '{ok,global_source,stale_sec,tip:.chain.tip_height}'

RUN_ID="${BASE_RID}a" BASE="$NODE_BASE" ADMIN_TOKEN="$ADMIN_TOKEN" bash scripts/tests/language_from_code_matrix.sh
RUN_ID="${BASE_RID}b" BASE="$NODE_BASE" ADMIN_TOKEN="$ADMIN_TOKEN" bash scripts/tests/orders_multilang_audit.sh
RUN_ID="${BASE_RID}c" BASE="$NODE_BASE" ADMIN_TOKEN="$ADMIN_TOKEN" bash scripts/tests/language_chaos_security.sh
RUN_ID="${BASE_RID}d" BASE="$NODE_BASE" ADMIN_TOKEN="$ADMIN_TOKEN" bash scripts/tests/orders_matrix.sh
RUN_ID="${BASE_RID}e" BASE="$NODE_BASE" bash scripts/tests/difficulty_health.sh
RUN_ID="${BASE_RID}f" BASE="$NODE_BASE" ADMIN_TOKEN="$ADMIN_TOKEN" bash scripts/tests/adversarial_api_matrix.sh

if [[ -n "${P2P_TOKEN:-}" ]]; then
  RUN_ID="${BASE_RID}g" BASE="$NODE_BASE" ADMIN_TOKEN="$ADMIN_TOKEN" P2P_TOKEN="$P2P_TOKEN" bash scripts/tests/p2p_smoke.sh
else
  echo "[vps-canonical-smoke] WARN: no P2P_TOKEN; skip p2p_smoke"
fi

if [[ -n "${COORD_ADMIN_TOKEN:-}" ]]; then
  RUN_ID="${BASE_RID}h" COORD="$COORD_BASE" COORD_ADMIN_TOKEN="$COORD_ADMIN_TOKEN" bash scripts/tests/coordinator_matrix.sh
else
  echo "[vps-canonical-smoke] WARN: no coordinator admin token; skip coordinator_matrix"
fi

echo "[vps-canonical-smoke] PASS — reports under reports/tests/${BASE_RID}*"
