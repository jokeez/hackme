#!/usr/bin/env bash
# Resume OSS CVE marathon after an in-flight hunt (or stale partial wave).
#
#   setsid bash scripts/ops/run_oss_cve_marathon_resume.sh \
#     >>logs/oss-cve-marathon-resume.nohup.log 2>&1 &
#
# Env: WAVE_FIRST WAVE_LAST MARATHON_TOP STAMP SKIP_DISCOVERY (same as day marathon)
set -euo pipefail
export PATH="/home/kapa/.nvm/versions/node/v24.14.1/bin:/home/kapa/.local/bin:$HOME/.foundry/bin:$HOME/.cargo/bin:$PATH"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

WAVE_FIRST="${WAVE_FIRST:-36}"
WAVE_LAST="${WAVE_LAST:-41}"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
LOG="$ROOT/logs/oss-cve-marathon-w${WAVE_FIRST}-${WAVE_LAST}-${STAMP}.nohup.log"

log() { echo "[resume $(date -u +%H:%M:%S)] $*" | tee -a "$LOG"; }

wait_hunt() {
  local pid
  while true; do
    pid="$(pgrep -f 'tools/oss_cve_hunt/|oss_cve_hunt -repo' | head -1 || true)"
    [[ -n "$pid" ]] || break
    log "wait oss_cve_hunt pid=$pid"
    sleep 60
  done
  pid="$(pgrep -f 'run_oss_cve_wave\.sh' | head -1 || true)"
  if [[ -n "$pid" ]]; then
    log "wait run_oss_cve_wave pid=$pid"
    while kill -0 "$pid" 2>/dev/null; do sleep 60; done
  fi
  log "hunt idle — starting marathon $WAVE_FIRST..$WAVE_LAST"
}

log "=== resume stamp=$STAMP waves=$WAVE_FIRST..$WAVE_LAST ==="
wait_hunt

export WAVE_FIRST WAVE_LAST STAMP SKIP_DISCOVERY="${SKIP_DISCOVERY:-1}" MARATHON_TOP="${MARATHON_TOP:-10}"
exec bash "$ROOT/scripts/ops/run_oss_cve_day_marathon.sh" >>"$LOG" 2>&1
