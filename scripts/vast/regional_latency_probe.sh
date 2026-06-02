#!/usr/bin/env bash
# Measure RTT + coordinator API latency from current host (run on Vast instance AND on home PC).
# Usage on Vast: bash scripts/regional_latency_probe.sh [region_label]
set -euo pipefail
PACK_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LABEL="${1:-$(hostname -s)}"
COORD="${COORD_URL:-https://hackme.tech/pool/coordinator}"
COORD="${COORD%/}"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT="${OUT:-$PACK_ROOT/reports/vast-session}"
mkdir -p "$OUT"
REPORT="$OUT/latency-${LABEL}-${STAMP}.txt"

{
  echo "=== regional latency probe ==="
  echo "label=$LABEL stamp=$STAMP"
  echo "host=$(hostname -f 2>/dev/null || hostname)"
  echo "coord=$COORD"
  echo ""

  echo "--- ping hackme.tech (3 samples) ---"
  ping -c 3 hackme.tech 2>&1 || echo "(ping unavailable)"

  echo ""
  echo "--- curl timing coordinator ---"
  for path in /health /api/pool/stats /api/work/stats; do
    curl -fsS -o /dev/null -w "path=${path} dns=%{time_namelookup}s connect=%{time_connect}s tls=%{time_appconnect}s ttfb=%{time_starttransfer}s total=%{time_total}s http=%{http_code}\n" \
      --max-time 30 "${COORD}${path}" 2>&1 || echo "FAIL ${path}"
  done

  echo ""
  echo "--- geo hint (ipinfo if available) ---"
  curl -fsS --max-time 10 https://ipinfo.io/json 2>/dev/null | head -5 || echo "(no ipinfo)"

} | tee "$REPORT"

echo "[latency] -> $REPORT"
