#!/usr/bin/env bash
# Private CVE candidate hunt — large budgets, no site publish.
# Targets: high-yield repos from ranker (interpreters, HTTP, binary parsers, obscure).
#
#   setsid bash scripts/ops/run_oss_cve_private_marathon.sh \
#     >>logs/oss-cve-private-marathon.nohup.log 2>&1 &
#   tail -f logs/oss-cve-private-marathon.nohup.log
#
# Env: STAMP HUNT_COOLDOWN_SEC SKIP_WAVES (set 1 to skip final ranked wave)
set -u
export PATH="/home/kapa/.local/bin:$HOME/.cargo/bin:$PATH"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
LOG_DIR="$ROOT/logs"
REPORT_ROOT="$ROOT/reports/oss-cve/private-marathon-${STAMP}"
mkdir -p "$LOG_DIR" "$REPORT_ROOT"

log() { echo "[private $(date -u +%H:%M:%S)] $*" | tee -a "$REPORT_ROOT/marathon.log"; }

wait_idle() {
  local pid
  while true; do
    pid="$(pgrep -f 'tools/oss_cve_hunt/|run_oss_cve_hunt\.sh|run_oss_cve_wave\.sh' | head -1 || true)"
    [[ -n "$pid" ]] || break
    log "wait hunt pid=$pid"
    sleep 45
  done
}

wait_gpu_safe() { sleep "${HUNT_COOLDOWN_SEC:-30}"; }

run_phase() {
  local targets="$1" budget="$2" tlim="$3" label="$4"
  local marker="$REPORT_ROOT/.done-${label}"
  [[ -f "$marker" ]] && { log "skip $label (done)"; return 0; }
  log "=== phase $label targets=$targets budget=$budget time=${tlim}s ==="
  wait_gpu_safe
  wait_idle
  local out="$ROOT/reports/oss-cve/private-${STAMP}-${label}"
  set +e
  TARGETS="$targets" BUDGET="$budget" TIME_LIMIT="$tlim" OUT="$out" SKIP_PACK_BUILD=1 \
    bash "$ROOT/scripts/ops/run_oss_cve_hunt.sh" >>"$REPORT_ROOT/${label}.log" 2>&1
  local rc=$?
  set -e
  log "phase $label rc=$rc out=$out"
  if [[ -f "$out/ROLLUP.json" ]]; then
    touch "$marker"
    python3 - "$out" >>"$REPORT_ROOT/summary.tsv" 2>/dev/null <<'PY' || true
import json, pathlib, sys
r = json.loads(pathlib.Path(sys.argv[1], "ROLLUP.json").read_text())
cands = r.get("cve_candidates") or []
print(f"{pathlib.Path(sys.argv[1]).name}\t{r.get('verdict')}\t{len(cands)}\t{(r.get('summary') or '')[:100]}")
PY
    if python3 - "$out/ROLLUP.json" <<'PY' 2>/dev/null; then
import json, pathlib, sys
r = json.loads(pathlib.Path(sys.argv[1]).read_text())
if r.get("verdict") == "CVE_CANDIDATE" or r.get("cve_candidates"):
    raise SystemExit(0)
raise SystemExit(1)
PY
      log "*** CVE_CANDIDATE in $label — check $out ***"
    fi
  fi
}

log "=== private CVE marathon start stamp=$STAMP (no publish) ==="
log "host=$(hostname) disk=$(df -h . | tail -1)"
echo -e "run\tverdict\tcandidates\tsummary" >"$REPORT_ROOT/summary.tsv"

python3 "$ROOT/scripts/ops/rank_oss_cve_targets.py" --top 24 \
  --out "$REPORT_ROOT/cve-rank.md" >>"$REPORT_ROOT/marathon.log" 2>&1 || true

# Skip disclosure-hold + saturated CLEAN from today's deep matrix
SKIP_IDS="centijson,md4c,libxml2,miniz,lz4,oniguruma,pcre2,zlib,mpack,yajl-210,cj5,inih,jsmn,tomlc99,libconfini,parsello"

# --- Phase 1: HTTP / protocol (highest real-world CVE surface) ---
run_phase "nghttp2" 500000 28800 "p1-nghttp2-deep"
run_phase "hiredis,picohttpparser" 300000 21600 "p2-http-redis"

# --- Phase 2: interpreters (UBSan-dense → ASAN chase) ---
run_phase "lua,quickjs,wren" 350000 25200 "p3-interpreters"
run_phase "duktape" 450000 28800 "p4-duktape-deep"

# --- Phase 3: binary / media parsers (often heap bugs) ---
run_phase "minimp3,stb_vorbis,stb_image" 300000 21600 "p5-binary-media"

# --- Phase 4: serialization / obscure low-coverage ---
run_phase "cmp,tinycbor,libcbor,kuba_zip" 250000 18000 "p6-serialization"
run_phase "jsonparser,frozen,cwalk,microtar" 200000 14400 "p7-obscure"

# --- Phase 5: fresh targets never in matrix ---
run_phase "expat,cmark,uriparser,cjson" 250000 18000 "p8-xml-uri-json"

# --- Phase 6: ranked wave slice (auto queue, skip saturated) ---
if [[ "${SKIP_WAVES:-0}" != "1" ]]; then
  wait_idle
  WAVE_NUM="${PRIVATE_WAVE:-49}"
  log "=== ranked wave $WAVE_NUM top=12 skip=$SKIP_IDS ==="
  wait_gpu_safe
  set +e
  WAVE="$WAVE_NUM" TOP=12 STAMP="${STAMP}-w${WAVE_NUM}" SKIP_DISCOVERY=1 \
    BUDGET=280000 TIME_LIMIT=21600 \
    bash "$ROOT/scripts/ops/run_oss_cve_wave.sh" >>"$REPORT_ROOT/wave${WAVE_NUM}.log" 2>&1
  set -e
fi

log "=== private marathon done — summary: $REPORT_ROOT/summary.tsv ==="
cat "$REPORT_ROOT/summary.tsv" | tee -a "$REPORT_ROOT/marathon.log"
