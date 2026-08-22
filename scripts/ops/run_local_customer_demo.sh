#!/usr/bin/env bash
# Local customer demo: 2048 runs, report.html + gate + proof (no deploy).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
PACK="${1:-secrets}"
OUT="${ROOT}/reports/local-demo-2048"
export HACKME_LOCAL_DEMO=1
export HACKME_DEMO_PACK="$PACK"
export HACKME_DEMO_OUT="$OUT"
echo "[demo] pack=$PACK runs=2048 out=$OUT"
HACKME_LOCAL_DEMO=1 HACKME_DEMO_PACK="$PACK" HACKME_DEMO_OUT="$OUT" \
  go test -count=1 -timeout=15m -run TestLocalCustomerDemo2048 . 2>&1 | tee /tmp/hackme-local-demo.log
echo ""
echo "[demo] open: file://$OUT/index.html"
