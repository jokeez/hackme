#!/usr/bin/env bash
# Remove stale test workers from coordinator memory (e.g. worker-crypto-matrix-*).
#
# Usage:
#   COORD_URL=https://hackme.tech/pool/coordinator \
#   ADMIN_TOKEN=... bash scripts/ops/purge_stale_pool_workers.sh
#
# Optional: PREFIX=l1v4- DRY_RUN=1

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_COORDINATOR_ADMIN_TOKEN:-}}"
PREFIX="${PREFIX:-worker-kapa-fair-}"
MAX_PAYOUT="${MAX_PAYOUT_HMC:-0.001}"
STALE_SEC="${STALE_SEC:-300}"
IGNORE_PAYOUT="${IGNORE_PAYOUT:-1}"
DRY_RUN="${DRY_RUN:-0}"

if [[ -z "$ADMIN_TOKEN" && -f "$ROOT/.secrets/hackme_coordinator_admin_token" ]]; then
  ADMIN_TOKEN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_coordinator_admin_token")"
fi
if [[ -z "$ADMIN_TOKEN" ]]; then
  echo "[purge-workers] set ADMIN_TOKEN or HACKME_COORDINATOR_ADMIN_TOKEN" >&2
  exit 2
fi

dry=false
[[ "$DRY_RUN" == "1" ]] && dry=true

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

resp="$(curl -fsS -X POST "${COORD_URL%/}/api/work/admin/prune-workers" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
  -d "$body")"
echo "$resp" | python3 -m json.tool 2>/dev/null || echo "$resp"
