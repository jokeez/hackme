#!/usr/bin/env bash
# Detached overnight bounty marathon (safe alongside desktop node).
#
#   bash scripts/ops/start_bounty_overnight.sh
#   DURATION_HOURS=10 BUDGET_RUNS=512 bash scripts/ops/start_bounty_overnight.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
RUN_ID="bounty-overnight-${STAMP}"
OUT_DIR="$ROOT/reports/bounty/overnight/$RUN_ID"

mkdir -p "$OUT_DIR"
ln -sfn "$OUT_DIR" "$ROOT/reports/bounty/overnight/CURRENT"

# Avoid duplicate marathon (do not kill self — match only older PIDs)
if [[ -f "$ROOT/reports/bounty/overnight/CURRENT/marathon.pid" ]]; then
  old_pid="$(tr -d '\r\n' <"$ROOT/reports/bounty/overnight/CURRENT/marathon.pid" 2>/dev/null || true)"
  if [[ -n "$old_pid" ]] && kill -0 "$old_pid" 2>/dev/null; then
    echo "[bounty-overnight] already running pid=$old_pid — exit"
    echo "  tail -f $ROOT/reports/bounty/overnight/CURRENT/marathon.log"
    exit 0
  fi
fi

export DURATION_HOURS="${DURATION_HOURS:-8}"
export BUDGET_RUNS="${BUDGET_RUNS:-512}"
export FOUNDRY_FUZZ_RUNS="${FOUNDRY_FUZZ_RUNS:-2048}"
unset FOUNDRY_FUZZ 2>/dev/null || true
export SKIP_RUST="${SKIP_RUST:-1}"
export STAMP RUN_ID HUNT_OUT="$OUT_DIR"

nohup bash "$ROOT/scripts/ops/run_bounty_overnight.sh" \
  >>"$OUT_DIR/marathon.log" 2>&1 &
echo $! >"$OUT_DIR/marathon.pid"
ln -sfn "$OUT_DIR" "$ROOT/reports/bounty/overnight/CURRENT"

cat <<EOF
[bounty-overnight] started RUN_ID=$RUN_ID pid=$(cat "$OUT_DIR/marathon.pid")
  duration=${DURATION_HOURS}h  wasm_runs=$BUDGET_RUNS  foundry_fuzz=$FOUNDRY_FUZZ_RUNS
  log:  tail -f $OUT_DIR/marathon.log
  утро: bash scripts/ops/bounty_morning_report.sh
EOF
