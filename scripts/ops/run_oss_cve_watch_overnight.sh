#!/usr/bin/env bash
# Overnight OSS CVE hammer: Watch day 2+ (nghttp2) then multi-repo waves/hunts.
# Safe alongside GPU mining (CPU ASAN only).
#
#   setsid bash scripts/ops/run_oss_cve_watch_overnight.sh \
#     >>logs/oss-cve-overnight.nohup.log 2>&1 &
#   tail -f logs/oss-cve-overnight.nohup.log
set -u
export PATH="/home/kapa/.local/bin:$HOME/.cargo/bin:$PATH"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
LOG_DIR="$ROOT/logs"
REPORT_ROOT="$ROOT/reports/oss-cve-overnight-${STAMP}"
mkdir -p "$LOG_DIR" "$REPORT_ROOT"

log() { echo "[overnight $(date -u +%H:%M:%S)] $*" | tee -a "$REPORT_ROOT/overnight.log"; }

wait_gpu_safe() {
  # Brief pause so workerpoh CUDA batches aren't starved on same host.
  sleep "${HUNT_COOLDOWN_SEC:-30}"
}

run_watch_day() {
  local day="$1" budget="$2" tlim="$3"
  log "=== CVE Watch DAY=$day target=nghttp2 budget=$budget time=${tlim}s ==="
  wait_gpu_safe
  set +e
  DAY="$day" TARGET=nghttp2 BUDGET="$budget" TIME_LIMIT="$tlim" \
    bash "$ROOT/scripts/ops/run_oss_cve_watch_day.sh" >>"$REPORT_ROOT/watch-day${day}.log" 2>&1
  local rc=$?
  set -e
  log "watch day $day rc=$rc"
  return 0
}

run_hunt() {
  local targets="$1" budget="$2" tlim="$3" label="$4"
  log "=== hunt $label targets=$targets budget=$budget time=${tlim}s ==="
  wait_gpu_safe
  local out="$ROOT/reports/oss-cve/overnight-${STAMP}-${label}"
  set +e
  TARGETS="$targets" BUDGET="$budget" TIME_LIMIT="$tlim" OUT="$out" SKIP_PACK_BUILD=1 \
    bash "$ROOT/scripts/ops/run_oss_cve_hunt.sh" >>"$REPORT_ROOT/hunt-${label}.log" 2>&1
  local rc=$?
  set -e
  log "hunt $label rc=$rc out=$out"
  if [[ -f "$out/ROLLUP.json" ]]; then
    python3 "$ROOT/scripts/ops/export_oss_cve_html.py" "$out" >>"$REPORT_ROOT/hunt-${label}.log" 2>&1 || true
  fi
  return 0
}

run_wave() {
  local wave="$1" top="$2"
  log "=== wave $wave top=$top ==="
  wait_gpu_safe
  set +e
  WAVE="$wave" TOP="$top" STAMP="${STAMP}-w${wave}" SKIP_DISCOVERY=1 \
    bash "$ROOT/scripts/ops/run_oss_cve_wave.sh" >>"$REPORT_ROOT/wave${wave}.log" 2>&1
  local rc=$?
  set -e
  log "wave $wave rc=$rc"
  return 0
}

log "=== OSS CVE overnight start stamp=$STAMP ==="
log "host=$(hostname) disk=$(df -h . | tail -1)"

# --- OSS CVE Watch (14-day nghttp2 series) ---
run_watch_day 2 100000 10800   # ~3h deep day 2
run_watch_day 3 150000 14400   # ~4h day 3

# --- Priority parsers (skip centijson disclosure hold) ---
run_hunt "md4c,cjson" 80000 5400 "p1-md4c-cjson"
run_hunt "jsmn,mjson,yyjson" 70000 5400 "p2-json"
run_hunt "tomlc99,expat,inih" 70000 5400 "p3-config-xml"

# --- Ranked high-yield waves (fresh queue slices) ---
run_wave 42 10
run_wave 43 10

# --- Nightly rotation pair (day-of-year) ---
DAY_OFFSET="$(date -u +%j)"
NIGHTLY_TARGETS="$(python3 - "$ROOT" "$DAY_OFFSET" <<'PY'
import json, sys
root, day = sys.argv[1], int(sys.argv[2])
m = json.loads(open(f"{root}/upstream/oss_cve_targets.json").read())
q = (m.get("rotation") or {}).get("queue") or []
skip = {"centijson", "libucl", "cfgpack"}
if not q:
    raise SystemExit(0)
n = len(q)
ids = [q[(day + i) % n] for i in range(3) if q[(day + i) % n] not in skip]
print(",".join(ids[:2]))
PY
)"
if [[ -n "$NIGHTLY_TARGETS" ]]; then
  run_hunt "$NIGHTLY_TARGETS" 60000 3600 "nightly-rot"
fi

# --- nghttp2 extra ASAN chase (informational follow-up) ---
run_hunt "nghttp2" 200000 10800 "nghttp2-chase"

log "=== overnight complete stamp=$STAMP ==="
log "deploy site when ready: NODE_SSH=hackme-vps SKIP_DIST=1 bash scripts/ops/deploy_hackme_site.sh"
