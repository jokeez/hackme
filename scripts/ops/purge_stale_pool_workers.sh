#!/usr/bin/env bash
# Prune offline stale workers from coordinator memory (settlement state keeps history).
#
# Usage:
#   bash scripts/ops/purge_stale_pool_workers.sh
#   PREFIX=worker-desk- STALE_SEC=1800 bash scripts/ops/purge_stale_pool_workers.sh
#   DRY_RUN=1 bash scripts/ops/purge_stale_pool_workers.sh
#
# Env:
#   COORD_URL              default https://hackme.tech/pool/coordinator
#   ADMIN_TOKEN            coordinator admin (or .secrets/hackme_coordinator_admin_token)
#   PREFIX                 optional worker_id prefix (empty = all offline stale)
#   MAX_PAYOUT_HMC         keep offline rows above this accrual (default 2)
#   STALE_SEC              offline threshold (default 1800 = 30m)
#   IGNORE_PAYOUT          1 = remove all matching offline stale regardless of payout
#   DRY_RUN                1 = preview only

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_COORDINATOR_ADMIN_TOKEN:-}}"
PREFIX="${PREFIX:-}"
MAX_PAYOUT="${MAX_PAYOUT_HMC:-2}"
STALE_SEC="${STALE_SEC:-1800}"
IGNORE_PAYOUT="${IGNORE_PAYOUT:-0}"
DRY_RUN="${DRY_RUN:-0}"

if [[ -z "$ADMIN_TOKEN" && -f "$ROOT/.secrets/hackme_coordinator_admin_token" ]]; then
  ADMIN_TOKEN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_coordinator_admin_token")"
fi
if [[ -z "$ADMIN_TOKEN" ]]; then
  echo "[purge-workers] set ADMIN_TOKEN or HACKME_COORDINATOR_ADMIN_TOKEN" >&2
  exit 2
fi

body="$(python3 - "$PREFIX" "$MAX_PAYOUT" "$STALE_SEC" "$IGNORE_PAYOUT" "$DRY_RUN" <<'PY'
import json, sys
prefix, max_p, stale, ignore, dry = sys.argv[1:6]
print(json.dumps({
  "prefix": prefix,
  "max_payout_hmc": float(max_p),
  "stale_sec": int(stale),
  "ignore_payout": ignore == "1",
  "dry_run": dry == "1",
}))
PY
)"

echo "[purge-workers] coord=${COORD_URL} prefix=${PREFIX:-<all>} stale=${STALE_SEC}s max_payout=${MAX_PAYOUT} ignore_payout=${IGNORE_PAYOUT} dry=${DRY_RUN}"
resp="$(curl -fsS -X POST "${COORD_URL%/}/api/work/admin/prune-workers" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
  -d "$body")"
echo "$resp" | python3 -m json.tool 2>/dev/null || echo "$resp"
