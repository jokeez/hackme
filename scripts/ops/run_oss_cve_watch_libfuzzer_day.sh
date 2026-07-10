#!/usr/bin/env bash
# OSS CVE Watch day N — libFuzzer session (coverage-guided depth).
# Day 1 on site = mutation stdin; Day 2+ = libFuzzer (honest label in export).
#
#   DAY=2 bash scripts/ops/run_oss_cve_watch_libfuzzer_day.sh
#   DAY=2 MAX_TIME=7200 SKIP_PUBLISH=1 bash scripts/ops/run_oss_cve_watch_libfuzzer_day.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

DAY="${DAY:-2}"
TARGET="${TARGET:-nghttp2}"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"

if [[ "$DAY" -lt 2 ]]; then
  echo "[watch-libfuzzer] Day 1 is mutation — use run_oss_cve_watch_day.sh" >&2
  exit 2
fi

# Budget ramp for libFuzzer (wall time, not iteration count)
if [[ -z "${MAX_TIME:-}" ]]; then
  case "$DAY" in
    2|3) MAX_TIME=7200 ;;
    4|5|6|7) MAX_TIME=14400 ;;
    8|9|10|11) MAX_TIME=21600 ;;
    *) MAX_TIME=28800 ;;
  esac
fi

OUT="$ROOT/reports/oss-cve-watch/day$(printf '%02d' "$DAY")-libfuzzer-$STAMP"
mkdir -p "$OUT" logs
log() { echo "[watch-lf-d$(printf '%02d' "$DAY")] $*" | tee -a "$OUT/run.log"; }

log "libFuzzer watch day=$DAY target=$TARGET max_time=${MAX_TIME}s"

export TARGET MAX_TIME STAMP
CORPUS_ROOT="$ROOT/reports/oss-cve-libfuzzer/$TARGET"
SESSION="$CORPUS_ROOT/sessions/$STAMP"
mkdir -p "$SESSION"

set +e
bash "$ROOT/scripts/ops/run_oss_libfuzzer_session.sh" 2>&1 | tee -a "$OUT/run.log"
RC=$?
set -e

[[ -f "$SESSION/ROLLUP.json" ]] || fail "missing session rollup — see $OUT/run.log"
cp "$SESSION/ROLLUP.json" "$OUT/ROLLUP.json"
cp "$SESSION/SESSION.json" "$OUT/SESSION.json" 2>/dev/null || true
cp "$SESSION/fuzzer.log" "$OUT/fuzzer.log" 2>/dev/null || true

if [[ "${SKIP_PUBLISH:-0}" == "1" ]]; then
  log "SKIP_PUBLISH=1 — local only ($OUT)"
else
  python3 "$ROOT/scripts/ops/export_oss_cve_watch_libfuzzer.py" "$DAY" "$OUT"
  log "published web/site/reports/oss-cve-watch/day$(printf '%02d' "$DAY").html"
fi

if [[ $RC -eq 1 ]]; then
  log "CVE_CANDIDATE — disclosure hold"
  exit 1
fi
log "session complete verdict=$(python3 -c "import json;print(json.load(open('$OUT/ROLLUP.json'))['verdict'])")"
exit 0
