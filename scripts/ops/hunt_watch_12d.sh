#!/usr/bin/env bash
# Hunt 12-day watch — sequential 24/7 batches (Hunt Standard, not libFuzzer).
#
#   DAY=1 bash scripts/ops/hunt_watch_12d.sh          # foreground
#   DAY=1 bash scripts/ops/hunt_watch_12d.sh launch   # setsid background
#   bash scripts/ops/hunt_watch_12d.sh status
#   bash scripts/ops/hunt_watch_12d.sh preflight      # driver + build gate
#
# Schedule: reports/hunt-watch/SCHEDULE.md
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export HACKME_REPO_ROOT="$ROOT"

DAY="${DAY:-1}"
CMD="${1:-run}"
SERIES="${SERIES:-2026sep}"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="${OUT:-$ROOT/reports/hunt-watch/${SERIES}/day$(printf '%02d' "$DAY")-${STAMP}}"
PIDFILE="$OUT/watch.pid"
LOG="$OUT/launcher.log"

# 3 targets × 8h wall = 24h per day (sequential ASAN).
WALL_SEC="${WALL_SEC:-86400}"
HUNT_ITER="${HUNT_ITER:-1000000}"
PKG="${PKG:-hunt_standard}"
RUN_LIBFUZZER="${RUN_LIBFUZZER:-0}"

day_targets() {
  case "$DAY" in
    1)  echo "cmp,cfgpack,cj5" ;;
    2)  echo "heatshrink,uriparser,inih" ;;
    3)  echo "jsmn,tomlc99,parson" ;;
    4)  echo "mjson,yyjson,sheredom" ;;
    5)  echo "miniz,zlib,expat" ;;
    6)  echo "md4c,cjson,libucl" ;;
    7)  echo "cmp,cfgpack,cj5" ;;       # repeat — depth / regression
    8)  echo "heatshrink,uriparser,inih" ;;
    9)  echo "jsmn,tomlc99,cj5" ;;
    10) echo "parson,mjson,yyjson" ;;
    11) echo "miniz,expat,libucl" ;;
    12) echo "cjson,cmp,cfgpack" ;;     # rollup eve — strong parsers
    *)  echo "[hunt-watch] invalid DAY=$DAY (1-12)" >&2; return 1 ;;
  esac
}

preflight() {
  local targets tid driver
  targets="$(day_targets)"
  IFS=',' read -r -a arr <<< "$targets"
  echo "[hunt-watch] preflight DAY=$DAY targets=$targets"
  for tid in "${arr[@]}"; do
    driver="$ROOT/tasks/sources/fuzz/oss/${tid}_stdin.c"
    if [[ ! -f "$driver" ]]; then
      echo "[hunt-watch] FAIL missing driver $driver" >&2
      return 1
    fi
  done
  TARGETS="$targets" bash "$ROOT/scripts/ops/build_oss_cve_pack.sh"
  echo "[hunt-watch] preflight PASS"
}

run_batch() {
  local targets
  targets="$(day_targets)"
  mkdir -p "$OUT"
  echo $$ >"$PIDFILE"
  {
    echo "[hunt-watch $(date -u +%H:%M:%S)] DAY=$DAY stamp=$STAMP"
    echo "[hunt-watch] targets=$targets wall_total=${WALL_SEC}s iter=$HUNT_ITER pkg=$PKG"
    TARGETS="$targets" \
      PKG="$PKG" \
      HUNT_ITER="$HUNT_ITER" \
      WALL_SEC="$WALL_SEC" \
      RUN_LIBFUZZER="$RUN_LIBFUZZER" \
      OUT="$OUT" \
      STAMP="$STAMP" \
      bash "$ROOT/scripts/ops/hunt_overnight_soak.sh"
    echo "[hunt-watch $(date -u +%H:%M:%S)] DAY=$DAY DONE → $OUT/REPORT.md"
  } >>"$LOG" 2>&1
}

status() {
  local base="$ROOT/reports/hunt-watch/${SERIES}"
  if [[ ! -d "$base" ]]; then
    echo "[hunt-watch] no series dir $base"
    return 0
  fi
  for d in "$base"/day*; do
    [[ -d "$d" ]] || continue
    local phase="?"
    [[ -f "$d/status.json" ]] && phase="$(python3 -c "import json; print(json.load(open('$d/status.json')).get('phase','?'))" 2>/dev/null || echo "?")"
    local pid=""
    [[ -f "$d/watch.pid" ]] && pid="$(cat "$d/watch.pid" 2>/dev/null || true)"
    local alive=""
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then alive="RUNNING"; else alive="stopped"; fi
    echo "$(basename "$d") phase=$phase pid=${pid:-—} $alive"
  done
}

case "$CMD" in
  preflight) preflight ;;
  run)       preflight && run_batch ;;
  launch)
    preflight
    mkdir -p "$OUT"
    setsid env \
      DAY="$DAY" STAMP="$STAMP" OUT="$OUT" SERIES="$SERIES" \
      WALL_SEC="$WALL_SEC" HUNT_ITER="$HUNT_ITER" PKG="$PKG" RUN_LIBFUZZER="$RUN_LIBFUZZER" \
      HACKME_REPO_ROOT="$ROOT" \
      bash "$0" run >>"$LOG" 2>&1 < /dev/null &
    echo "[hunt-watch] launched DAY=$DAY pid=$! out=$OUT"
    echo "[hunt-watch] tail -f $LOG"
    ;;
  status) status ;;
  *)
    echo "usage: DAY=N bash $0 {preflight|run|launch|status}" >&2
    exit 1
    ;;
esac
