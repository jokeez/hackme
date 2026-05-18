#!/usr/bin/env bash
# Legacy thin harness (baseline/transfers/orders/security). Prefer run_daily.sh:
#   MODE=quick — minimal; MODE=full — full gate + language-static prefix + report_summary.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RID="${RUN_ID:-$(date -u +"%Y%m%dT%H%M%SZ")}"
export RUN_ID="$RID"

echo "== HackMe test pack =="
echo "RUN_ID=$RUN_ID"

"$ROOT_DIR/scripts/tests/baseline_snapshot.sh"
"$ROOT_DIR/scripts/tests/transfers_matrix.sh"
"$ROOT_DIR/scripts/tests/orders_matrix.sh"

if [[ "${RUN_COORDINATOR_MATRIX:-0}" == "1" ]]; then
  "$ROOT_DIR/scripts/tests/coordinator_matrix.sh"
fi

"$ROOT_DIR/scripts/tests/security_assertions.sh"
"$ROOT_DIR/scripts/tests/report_summary.sh"

echo "Done. Run summary:"
echo "  $ROOT_DIR/reports/tests/$RUN_ID/summary_all.json"
