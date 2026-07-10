#!/usr/bin/env bash
# Deep CVE hunts on non-watch targets — local reports only (no oss-cve-watch publish).
# Safe alongside GPU mining (CPU ASAN). Day 2 watch publishes manually next calendar day.
#
#   setsid bash scripts/ops/run_oss_cve_deep_daynight.sh \
#     >>logs/oss-cve-deep-daynight.nohup.log 2>&1 &
#   tail -f logs/oss-cve-deep-daynight.nohup.log
set -u
export PATH="/home/kapa/.local/bin:$HOME/.cargo/bin:$PATH"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
LOG_DIR="$ROOT/logs"
REPORT_ROOT="$ROOT/reports/oss-cve-deep-${STAMP}"
mkdir -p "$LOG_DIR" "$REPORT_ROOT"

log() { echo "[deep $(date -u +%H:%M:%S)] $*" | tee -a "$REPORT_ROOT/deep.log"; }

wait_idle() {
  local pid
  while true; do
    pid="$(pgrep -f 'tools/oss_cve_hunt/|run_oss_cve_hunt\.sh|run_oss_cve_wave' | head -1 || true)"
    [[ -n "$pid" ]] || break
    log "wait hunt pid=$pid"
    sleep 45
  done
}

wait_gpu_safe() { sleep "${HUNT_COOLDOWN_SEC:-25}"; }

run_hunt() {
  local targets="$1" budget="$2" tlim="$3" label="$4"
  local marker="$REPORT_ROOT/.done-${label}"
  [[ -f "$marker" ]] && { log "skip $label (marker)"; return 0; }
  log "=== deep $label targets=$targets budget=$budget time=${tlim}s ==="
  wait_gpu_safe
  local out="$ROOT/reports/oss-cve/deep-${STAMP}-${label}"
  set +e
  TARGETS="$targets" BUDGET="$budget" TIME_LIMIT="$tlim" OUT="$out" SKIP_PACK_BUILD=1 \
    bash "$ROOT/scripts/ops/run_oss_cve_hunt.sh" >>"$REPORT_ROOT/${label}.log" 2>&1
  local rc=$?
  set -e
  log "deep $label rc=$rc out=$out"
  if [[ -f "$out/ROLLUP.json" ]]; then
    touch "$marker"
    python3 "$ROOT/scripts/ops/export_oss_cve_html.py" "$out" >>"$REPORT_ROOT/${label}.log" 2>&1 || true
    python3 - "$out" >>"$REPORT_ROOT/summary.tsv" 2>/dev/null <<'PY' || true
import json, pathlib, sys
r = json.loads(pathlib.Path(sys.argv[1], "ROLLUP.json").read_text())
print(f"{pathlib.Path(sys.argv[1]).name}\t{r.get('verdict')}\t{r.get('summary','')[:80]}")
PY
  fi
}

run_wave() {
  local wave="$1" top="$2" label="$3"
  local marker="$REPORT_ROOT/.done-wave-${label}"
  [[ -f "$marker" ]] && { log "skip wave $label (marker)"; return 0; }
  log "=== wave $wave top=$top label=$label ==="
  wait_gpu_safe
  set +e
  WAVE="$wave" TOP="$top" STAMP="${STAMP}-${label}" SKIP_DISCOVERY=1 \
    bash "$ROOT/scripts/ops/run_oss_cve_wave.sh" >>"$REPORT_ROOT/wave-${label}.log" 2>&1
  local rc=$?
  set -e
  log "wave $label rc=$rc"
  local roll
  roll="$(ls -td "$ROOT/reports/oss-cve/wave${wave}-${STAMP}-${label}"*/ROLLUP.json 2>/dev/null | head -1 || true)"
  [[ -n "$roll" ]] && touch "$marker"
}

log "=== deep day+night start stamp=$STAMP (no watch publish) ==="
log "host=$(hostname) disk=$(df -h . | tail -1)"
echo -e "run\tverdict\tsummary" >"$REPORT_ROOT/summary.tsv"

wait_idle

# --- interpreters / JS engines (highest historical UBSan density → ASAN chase) ---
run_hunt "duktape" 200000 14400 "p1-duktape"

# --- compression (never deep-fuzzed in matrix) ---
run_hunt "miniz,lz4" 150000 10800 "p2-compress"

# --- regex engines ---
run_hunt "oniguruma,pcre2" 150000 10800 "p3-regex"

# --- XML / heavy parsers ---
run_hunt "libxml2" 180000 10800 "p4-libxml2"

# --- msgpack / json edge parsers ---
run_hunt "mpack,yajl-210,cj5" 120000 7200 "p5-binary-json"

# --- markdown (candidate for a future public watch day) ---
run_hunt "md4c" 150000 10800 "p6-md4c"

# --- ranked wave slice (fresh targets from ranker) ---
WAVE_NUM="${DEEP_WAVE:-48}"
run_wave "$WAVE_NUM" 10 "w${WAVE_NUM}"

# --- nightly rotation (skip disclosure-hold ids) ---
DAY_OFFSET="$(date -u +%j)"
NIGHTLY_TARGETS="$(python3 - "$ROOT" "$DAY_OFFSET" <<'PY'
import json, sys
root, day = sys.argv[1], int(sys.argv[2])
m = json.loads(open(f"{root}/upstream/oss_cve_targets.json").read())
q = (m.get("rotation") or {}).get("queue") or []
skip = {"centijson", "libucl", "cfgpack", "nghttp2"}
if not q:
    raise SystemExit(0)
n = len(q)
ids = [q[(day + i) % n] for i in range(5) if q[(day + i) % n] not in skip]
print(",".join(ids[:3]))
PY
)"
if [[ -n "$NIGHTLY_TARGETS" ]]; then
  run_hunt "$NIGHTLY_TARGETS" 100000 7200 "p7-rotation"
fi

# --- duktape ASAN deep chase if still informational-heavy ---
run_hunt "duktape" 250000 14400 "p8-duktape-chase"

log "=== deep day+night complete stamp=$STAMP ==="
log "summary: $REPORT_ROOT/summary.tsv"
log "Day 2 watch publish tomorrow: python3 scripts/ops/export_oss_cve_watch_html.py 2 reports/oss-cve-watch/day02-20260707T194029Z/"
