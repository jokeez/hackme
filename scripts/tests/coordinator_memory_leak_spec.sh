#!/usr/bin/env bash
# Memory & leak spec: 500 virtual workers churn for 2h (or quick CI mode).
#
#   LEAK_SPEC_QUICK=1 bash scripts/tests/coordinator_memory_leak_spec.sh
#   bash scripts/tests/coordinator_memory_leak_spec.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

require_cmd python3
require_cmd go
require_cmd curl

# shellcheck disable=SC1091
source "$ROOT/scripts/tests/coordinator_stress.env"

LEAK_SPEC_QUICK="${LEAK_SPEC_QUICK:-0}"
if [[ "$LEAK_SPEC_QUICK" == "1" ]]; then
  DURATION_SEC="${DURATION_SEC:-180}"
  WORKERS="${WORKERS:-80}"
  BASELINE_MB="${BASELINE_MB:-24}"
  MARGIN_MB="${MARGIN_MB:-18}"
else
  DURATION_SEC="${DURATION_SEC:-7200}"
  WORKERS="${WORKERS:-500}"
  BASELINE_MB="${BASELINE_MB:-20.8}"
  MARGIN_MB="${MARGIN_MB:-12}"
fi

RID="${RUN_ID:-$(run_id)}"
REPORT_DIR="${REPORT_DIR:-$ROOT/reports/tests/$RID/coordinator_memory_leak_spec}"
COORD_BIN="${COORD_BIN:-$ROOT/bin/coordinator-leak-spec}"
COORD_PID_FILE="$REPORT_DIR/coordinator.pid"
COORD_LOG="$REPORT_DIR/coordinator.log"

mkdir -p "$REPORT_DIR" "$(dirname "$HACKME_COORDINATOR_DB")"
rm -f "$HACKME_COORDINATOR_DB" "${HACKME_COORDINATOR_DB}-wal" "${HACKME_COORDINATOR_DB}-shm" 2>/dev/null || true

echo "[leak-spec] build coordinator"
go build -trimpath -o "$COORD_BIN" ./cmd/coordinator

cleanup() {
  if [[ -f "$COORD_PID_FILE" ]]; then
    pid="$(cat "$COORD_PID_FILE" 2>/dev/null || true)"
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
      sleep 1
      kill -9 "$pid" 2>/dev/null || true
    fi
  fi
}
trap cleanup EXIT

nohup "$COORD_BIN" >>"$COORD_LOG" 2>&1 &
echo $! >"$COORD_PID_FILE"

COORD_URL="http://127.0.0.1:${HACKME_COORDINATOR_ADDR##*:}"
for _ in $(seq 1 40); do
  if curl -fsS --max-time 2 "$COORD_URL/health" >/dev/null 2>&1; then
    break
  fi
  sleep 0.25
done
curl -fsS --max-time 5 "$COORD_URL/health" | jq -e '.ok == "coordinator"' >/dev/null

python3 "$ROOT/scripts/tests/tools/coordinator_memory_leak_spec.py" \
  --coord "$COORD_URL" \
  --token "$HACKME_COORDINATOR_ADMIN_TOKEN" \
  --workers "$WORKERS" \
  --duration-sec "$DURATION_SEC" \
  --report-dir "$REPORT_DIR" \
  --baseline-mb "$BASELINE_MB" \
  --margin-mb "$MARGIN_MB"

pass "memory leak spec finished — $REPORT_DIR/MEMORY_LEAK_SPEC_REPORT.md"
