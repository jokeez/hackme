#!/usr/bin/env bash
# Snapshot local worker + prod pool for morning verification.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT="${OUT:-$ROOT/reports/mining-night-$STAMP}"
mkdir -p "$OUT"

DESKTOP_ENV="${DESKTOP_ENV_FILE:-$ROOT/.env.desktop}"
ADMIN=""
WORKER_ID="${WORKER_ID:-worker-kapa-pc}"
POOL="${POOL_BASE:-https://hackme.tech/pool/coordinator}"

if [[ -f "$DESKTOP_ENV" ]]; then
  ADMIN="$(grep '^HACKME_ADMIN_TOKEN=' "$DESKTOP_ENV" | cut -d= -f2- || true)"
  WORKER_ID="$(grep '^WORKER_ID=' "$DESKTOP_ENV" | cut -d= -f2- || echo "$WORKER_ID")"
fi

{
  echo "# Mining night snapshot — $STAMP (UTC)"
  echo ""
  echo "## Processes"
  pgrep -af 'hackme-node-desktop|workerpoh-' || echo "(none)"
  echo ""
  echo "## Local worker status"
  if [[ -n "$ADMIN" ]]; then
    curl -fsS --max-time 15 -H "X-Hackme-Admin-Token: $ADMIN" \
      "http://127.0.0.1:8080/api/worker/status" | jq . 2>/dev/null || echo "node unreachable"
  else
    echo "no HACKME_ADMIN_TOKEN"
  fi
  echo ""
  echo "## Pool coordinator ($WORKER_ID)"
  curl -fsS --max-time 20 "${POOL}/api/work/stats" | jq "{
    target_mod, reward_per_m, pool_hashrate_gh_s,
    worker: .workers[\"$WORKER_ID\"]
  }" 2>/dev/null || echo "pool unreachable"
} | tee "$OUT/SNAPSHOT.md"

echo "[mining-night] wrote $OUT/SNAPSHOT.md"
