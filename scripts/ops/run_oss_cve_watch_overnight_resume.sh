#!/usr/bin/env bash
# Resume overnight OSS CVE queue after interrupt (skips finished steps).
#
#   setsid bash scripts/ops/run_oss_cve_watch_overnight_resume.sh \
#     >>logs/oss-cve-overnight-resume.nohup.log 2>&1 &
set -u
export PATH="/home/kapa/.local/bin:$HOME/.cargo/bin:$PATH"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
LOG_DIR="$ROOT/logs"
REPORT_ROOT="$ROOT/reports/oss-cve-overnight-resume-${STAMP}"
mkdir -p "$LOG_DIR" "$REPORT_ROOT"

log() { echo "[resume $(date -u +%H:%M:%S)] $*" | tee -a "$REPORT_ROOT/resume.log"; }

wait_idle() {
  local pid
  while true; do
    pid="$(pgrep -f 'tools/oss_cve_hunt/|run_oss_cve_watch_day|run_oss_cve_hunt\.sh|run_oss_cve_wave' | head -1 || true)"
    [[ -n "$pid" ]] || break
    log "wait hunt pid=$pid"
    sleep 45
  done
}

wait_gpu_safe() { sleep "${HUNT_COOLDOWN_SEC:-20}"; }

has_rollup() { [[ -f "$1/ROLLUP.json" ]]; }

latest_watch_day_dir() {
  local day="$1"
  ls -td "$ROOT/reports/oss-cve-watch/day$(printf '%02d' "$day")-"* 2>/dev/null | head -1 || true
}

run_watch_day() {
  local day="$1" budget="$2" tlim="$3"
  local prev
  prev="$(latest_watch_day_dir "$day")"
  if [[ -n "$prev" ]] && has_rollup "$prev"; then
    log "skip watch day $day — done $prev"
    if [[ "${SKIP_PUBLISH:-1}" != "1" ]]; then
      python3 "$ROOT/scripts/ops/export_oss_cve_watch_html.py" "$day" "$prev" >>"$REPORT_ROOT/watch-day${day}.log" 2>&1 || true
    fi
    return 0
  fi
  log "=== CVE Watch DAY=$day nghttp2 budget=$budget time=${tlim}s (SKIP_PUBLISH=1) ==="
  wait_gpu_safe
  set +e
  DAY="$day" TARGET=nghttp2 BUDGET="$budget" TIME_LIMIT="$tlim" SKIP_PUBLISH=1 \
    bash "$ROOT/scripts/ops/run_oss_cve_watch_day.sh" >>"$REPORT_ROOT/watch-day${day}.log" 2>&1
  local rc=$?
  set -e
  log "watch day $day rc=$rc"
}

run_hunt() {
  local targets="$1" budget="$2" tlim="$3" label="$4"
  local marker="$REPORT_ROOT/.done-hunt-${label}"
  [[ -f "$marker" ]] && { log "skip hunt $label (marker)"; return 0; }
  log "=== hunt $label targets=$targets budget=$budget ==="
  wait_gpu_safe
  local out="$ROOT/reports/oss-cve/overnight-resume-${STAMP}-${label}"
  set +e
  TARGETS="$targets" BUDGET="$budget" TIME_LIMIT="$tlim" OUT="$out" SKIP_PACK_BUILD=1 \
    bash "$ROOT/scripts/ops/run_oss_cve_hunt.sh" >>"$REPORT_ROOT/hunt-${label}.log" 2>&1
  local rc=$?
  set -e
  log "hunt $label rc=$rc"
  if [[ -f "$out/ROLLUP.json" ]]; then
    touch "$marker"
    python3 "$ROOT/scripts/ops/export_oss_cve_html.py" "$out" >>"$REPORT_ROOT/hunt-${label}.log" 2>&1 || true
  fi
}

run_wave() {
  local wave="$1" top="$2"
  local marker="$REPORT_ROOT/.done-wave-${wave}"
  if [[ -f "$marker" ]]; then
    log "skip wave $wave (marker)"
    return 0
  fi
  local existing
  existing="$(ls -td "$ROOT/reports/oss-cve/wave${wave}-"*/ROLLUP.json 2>/dev/null | head -1 || true)"
  if [[ -n "$existing" ]] && [[ "$(stat -c %Y "$existing" 2>/dev/null || echo 0)" -gt "$(($(date +%s)-43200))" ]]; then
    log "skip wave $wave — recent $existing"
    touch "$marker"
    return 0
  fi
  log "=== wave $wave top=$top ==="
  wait_gpu_safe
  set +e
  WAVE="$wave" TOP="$top" STAMP="${STAMP}-w${wave}" \
    bash "$ROOT/scripts/ops/run_oss_cve_wave.sh" >>"$REPORT_ROOT/wave${wave}.log" 2>&1
  local rc=$?
  set -e
  log "wave $wave rc=$rc"
  [[ -f "$(ls -td "$ROOT/reports/oss-cve/wave${wave}-${STAMP}"*/ROLLUP.json 2>/dev/null | head -1)" ]] && touch "$marker" || true
}

log "=== resume start stamp=$STAMP (watch days local-only; publish manually) ==="
wait_idle

# Only finish interrupted day 2 locally — day 3+ waits for next calendar publish slot
run_watch_day 2 100000 10800

# Background matrix hunts (not watch ledger) — prep material for future days / oss-cve hub
run_hunt "md4c,cjson" 60000 3600 "bg-md4c-cjson"
run_hunt "jsmn,mjson" 50000 3600 "bg-json"

log "=== resume slice done — stop here until Day 2 is published on schedule ==="
log "publish: python3 scripts/ops/export_oss_cve_watch_html.py 2 <report_dir> && deploy_hackme_site.sh"
