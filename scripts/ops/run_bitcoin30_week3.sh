#!/usr/bin/env bash
# Bitcoin30 Week 3 — days 15–21 (wasm_native + native bridge).
#   bash scripts/ops/run_bitcoin30_week3.sh
#   FROM_DAY=17 bash scripts/ops/run_bitcoin30_week3.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
FROM_DAY="${FROM_DAY:-15}"
TO_DAY="${TO_DAY:-21}"
LOG="${LOG:-$ROOT/reports/bitcoin30/week3-batch.log}"
mkdir -p "$(dirname "$LOG")"
echo "[btc30-w3] days $FROM_DAY–$TO_DAY → $LOG"
for d in $(seq "$FROM_DAY" "$TO_DAY"); do
  echo "======== DAY $d ========" | tee -a "$LOG"
  DAY="$d" bash "$ROOT/scripts/ops/run_bitcoin30_day.sh" 2>&1 | tee -a "$LOG"
  python3 "$ROOT/scripts/ops/export_bitcoin30_day_html.py" "$d"
done
echo "[btc30-w3] export week3 ledger"
python3 "$ROOT/scripts/ops/export_bitcoin30_week_html.py" 3 2>/dev/null || true
echo "[btc30-w3] done"
