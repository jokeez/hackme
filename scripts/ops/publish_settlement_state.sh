#!/usr/bin/env bash
# Publish read-only settlement state for desktop UI / canonical merge.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DEPLOY="${NODE_DEPLOY_DIR:-/opt/hackme}"
STATE_FILE="${STATE_FILE:-${DEPLOY}/data/worker_settlement_state.json}"
OUT="${SETTLEMENT_CANONICAL_JSON:-$(dirname "$STATE_FILE")/settlement_canonical_public.json}"

[[ -f "$STATE_FILE" ]] || { echo "[publish-settle] missing $STATE_FILE" >&2; exit 1; }
now="$(date +%s)"
jq --argjson ts "$now" \
  '{workers: (.workers // {}), meta: (.meta // {}), updated_unix: $ts, source: "canonical"}' \
  "$STATE_FILE" >"${OUT}.tmp"
mv "${OUT}.tmp" "$OUT"
chmod 644 "$OUT" 2>/dev/null || true
chmod 755 "$(dirname "$OUT")" 2>/dev/null || true
echo "[publish-settle] wrote $OUT"
