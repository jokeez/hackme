#!/usr/bin/env bash
# Bitcoin30 — run days 10–14 (5-day week-2 block). One module per invocation.
#
#   bash scripts/ops/start_bitcoin30_5days.sh              # days 10→14, wait 24h between
#   WAIT_SEC=0 bash scripts/ops/start_bitcoin30_5days.sh   # back-to-back (CI / catch-up)
#   START_DAY=11 END_DAY=11 bash scripts/ops/start_bitcoin30_5days.sh  # single day
#
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export PATH="/home/kapa/.local/bin:$HOME/.foundry/bin:$PATH"

START_DAY="${START_DAY:-10}"
END_DAY="${END_DAY:-14}"
WAIT_SEC="${WAIT_SEC:-86400}"
LOG="${LOG:-$ROOT/reports/bitcoin30/5day-campaign.log}"
mkdir -p "$(dirname "$LOG")"

log() { echo "[btc30-5d $(date -u +%Y-%m-%dT%H:%M:%SZ)] $*" | tee -a "$LOG"; }

log "campaign start days=$START_DAY..$END_DAY wait=${WAIT_SEC}s"
for d in $(seq "$START_DAY" "$END_DAY"); do
  log "=== DAY=$d ==="
  if DAY="$d" bash "$ROOT/scripts/ops/run_bitcoin30_day.sh" >>"$LOG" 2>&1; then
    log "DAY=$d ok"
  else
    log "DAY=$d failed (continuing)"
  fi
  if [[ "$d" -lt "$END_DAY" && "$WAIT_SEC" -gt 0 ]]; then
    log "sleep ${WAIT_SEC}s until next day"
    sleep "$WAIT_SEC"
  fi
done
log "campaign done days=$START_DAY..$END_DAY"
