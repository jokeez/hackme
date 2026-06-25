#!/usr/bin/env bash
# Bitcoin30 days 15–30 (Week 3 wasm_native + Week 4 bytes_corpus + milestone).
#   bash scripts/ops/run_bitcoin30_remaining.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
FROM_DAY="${FROM_DAY:-15}"
TO_DAY="${TO_DAY:-30}"
LOG="${LOG:-$ROOT/reports/bitcoin30/days${FROM_DAY}-${TO_DAY}-batch.log}"
mkdir -p "$(dirname "$LOG")"
for d in $(seq "$FROM_DAY" "$TO_DAY"); do
  echo "======== DAY $d ========" | tee -a "$LOG"
  DAY="$d" bash "$ROOT/scripts/ops/run_bitcoin30_day.sh" 2>&1 | tee -a "$LOG"
  python3 "$ROOT/scripts/ops/export_bitcoin30_day_html.py" "$d"
  sleep "${BETWEEN_DAYS_SEC:-5}"
done
for w in 3 4; do
  python3 "$ROOT/scripts/ops/export_bitcoin30_week_html.py" "$w" 2>/dev/null || true
done
echo "[btc30] days $FROM_DAY–$TO_DAY complete"
