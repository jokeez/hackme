#!/usr/bin/env bash
# 10-hour fleet soak: sample pool difficulty, payouts, fuzz, WAL every 10 min.
#
#   bash scripts/ops/fleet_soak_10h.sh          # foreground loop
#   bash scripts/ops/fleet_soak_10h.sh --bg     # systemd --user background
#   bash scripts/ops/fleet_soak_briefing.sh     # summary after samples exist
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

DURATION_SEC="${DURATION_SEC:-36000}"   # 10h
INTERVAL_SEC="${INTERVAL_SEC:-600}"     # 10 min
SOAK_DIR="${SOAK_DIR:-$ROOT/reports/fleet-soak-10h}"
UNIT_NAME="${UNIT_NAME:-hackme-fleet-soak-10h}"
LOG_FILE="${LOG_FILE:-$SOAK_DIR/soak.log}"

mkdir -p "$SOAK_DIR"

run_loop() {
  local end=$(( $(date +%s) + DURATION_SEC ))
  local n=0
  echo "[soak-10h] start $(date -u +%Y-%m-%dT%H:%M:%SZ) duration=${DURATION_SEC}s interval=${INTERVAL_SEC}s dir=${SOAK_DIR}"
  while (( $(date +%s) < end )); do
    n=$((n + 1))
    echo "[soak-10h] sample #$n"
    DURATION_SEC="$DURATION_SEC" INTERVAL_SEC="$INTERVAL_SEC" SOAK_DIR="$SOAK_DIR" \
      bash "$ROOT/scripts/ops/fleet_soak_sample.sh" 2>&1 | tee -a "$LOG_FILE" || true
    remaining=$(( end - $(date +%s) ))
    if (( remaining <= 0 )); then
      break
    fi
    sleep_sec="$INTERVAL_SEC"
    if (( remaining < sleep_sec )); then
      sleep_sec="$remaining"
    fi
    sleep "$sleep_sec"
  done
  echo "[soak-10h] done $(date -u +%Y-%m-%dT%H:%M:%SZ) samples=$n"
  bash "$ROOT/scripts/ops/fleet_soak_briefing.sh" 2>&1 | tee -a "$LOG_FILE" || true
}

if [[ "${1:-}" == "--bg" ]]; then
  systemctl --user stop "${UNIT_NAME}.service" 2>/dev/null || true
  systemd-run --user \
    --unit="$UNIT_NAME" \
    --property=Restart=no \
    --working-directory="$ROOT" \
    --setenv=DURATION_SEC="$DURATION_SEC" \
    --setenv=INTERVAL_SEC="$INTERVAL_SEC" \
    --setenv=SOAK_DIR="$SOAK_DIR" \
    /bin/bash -c "exec >>\"$LOG_FILE\" 2>&1; exec bash \"$ROOT/scripts/ops/fleet_soak_10h.sh\""
  echo "[soak-10h] background unit=${UNIT_NAME}.service log=$LOG_FILE"
  echo "[soak-10h] status: systemctl --user status ${UNIT_NAME}.service"
  echo "[soak-10h] briefing: bash scripts/ops/fleet_soak_briefing.sh"
  exit 0
fi

run_loop
