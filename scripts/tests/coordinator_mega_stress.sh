#!/usr/bin/env bash
# Mega stress: local coordinator + 100 virtual workers + memory/RSS + chaos + report.
#
# Quick CI run (~90s load):
#   STRESS_QUICK=1 bash scripts/tests/coordinator_mega_stress.sh
#
# Full 10-minute run (as spec):
#   bash scripts/tests/coordinator_mega_stress.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

require_cmd python3
require_cmd go
require_cmd curl
require_cmd jq

# shellcheck disable=SC1091
source "$ROOT/scripts/tests/coordinator_stress.env"

STRESS_QUICK="${STRESS_QUICK:-0}"
if [[ "$STRESS_QUICK" == "1" ]]; then
  DURATION_SEC="${DURATION_SEC:-90}"
  WORKERS="${WORKERS:-50}"
  TARGET_RPS="${TARGET_RPS:-10}"
else
  DURATION_SEC="${DURATION_SEC:-600}"
  WORKERS="${WORKERS:-100}"
  TARGET_RPS="${TARGET_RPS:-25}"
fi

RID="${RUN_ID:-$(run_id)}"
REPORT_DIR="${REPORT_DIR:-$ROOT/reports/tests/$RID/coordinator_mega_stress}"
COORD_BIN="${COORD_BIN:-$ROOT/bin/coordinator-stress}"
COORD_PID_FILE="$REPORT_DIR/coordinator.pid"
COORD_LOG="$REPORT_DIR/coordinator.log"

mkdir -p "$REPORT_DIR" "$(dirname "$HACKME_COORDINATOR_DB")"
rm -f "$HACKME_COORDINATOR_DB" "${HACKME_COORDINATOR_DB}-wal" "${HACKME_COORDINATOR_DB}-shm" 2>/dev/null || true

echo "[mega-stress] build coordinator"
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

echo "[mega-stress] start coordinator on $HACKME_COORDINATOR_ADDR"
nohup "$COORD_BIN" >>"$COORD_LOG" 2>&1 &
echo $! >"$COORD_PID_FILE"
COORD_PID="$(cat "$COORD_PID_FILE")"

for _ in $(seq 1 40); do
  if curl -fsS --max-time 2 "http://${HACKME_COORDINATOR_ADDR#*://}/health" >/dev/null 2>&1 \
    || curl -fsS --max-time 2 "http://127.0.0.1:${HACKME_COORDINATOR_ADDR##*:}/health" >/dev/null 2>&1; then
    break
  fi
  sleep 0.25
done

COORD_URL="http://${HACKME_COORDINATOR_ADDR}"
if [[ "$HACKME_COORDINATOR_ADDR" == *:* ]]; then
  COORD_URL="http://127.0.0.1:${HACKME_COORDINATOR_ADDR##*:}"
fi

curl -fsS --max-time 5 "$COORD_URL/health" | jq -e '.ok == "coordinator"' >/dev/null \
  || { echo "[mega-stress] coordinator failed to start — see $COORD_LOG" >&2; exit 1; }

curl -fsS --max-time 8 -X POST "$COORD_URL/api/work/admin/clear-abuse" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: $HACKME_COORDINATOR_ADMIN_TOKEN" \
  -d '{"all":true}' >/dev/null || true

echo "[mega-stress] workers=$WORKERS duration=${DURATION_SEC}s target_rps=$TARGET_RPS pid=$COORD_PID"
export COORD="$COORD_URL"
export COORD_ADMIN_TOKEN="$HACKME_COORDINATOR_ADMIN_TOKEN"
export COORD_PID REPORT_DIR ROOT

python3 "$ROOT/scripts/tests/tools/coordinator_mega_stress.py" \
  --coord "$COORD" \
  --token "$COORD_ADMIN_TOKEN" \
  --workers "$WORKERS" \
  --duration-sec "$DURATION_SEC" \
  --target-rps "$TARGET_RPS" \
  --coord-pid "$COORD_PID" \
  --report-dir "$REPORT_DIR" \
  --root "$ROOT"

if grep -Eqi 'database is locked|SQLITE_BUSY|panic:' "$COORD_LOG"; then
  echo "[mega-stress] FAIL: coordinator log contains sqlite lock or panic" >&2
  grep -Ei 'database is locked|SQLITE_BUSY|panic:' "$COORD_LOG" | tail -20 >&2 || true
  exit 1
fi

ln -sfn "$REPORT_DIR" "$ROOT/reports/coordinator-mega-stress-LATEST"
cp -f "$REPORT_DIR/MEGA_STRESS_REPORT.md" "$ROOT/reports/COORDINATOR_MEGA_STRESS_REPORT.md" 2>/dev/null || true

pass "coordinator mega stress finished — report: $REPORT_DIR/MEGA_STRESS_REPORT.md"
