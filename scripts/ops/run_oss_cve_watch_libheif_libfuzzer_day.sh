#!/usr/bin/env bash
# OSS CVE Watch · libheif — one libFuzzer day (24h cadence uses MAX_TIME=remaining).
#
#   DAY=1 MAX_TIME=86400 bash scripts/ops/run_oss_cve_watch_libheif_libfuzzer_day.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

DAY="${DAY:-1}"
TARGET="${TARGET:-libheif}"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
MAX_TIME="${MAX_TIME:-86400}"
WATCH_DIR="${WATCH_DIR:-$ROOT/reports/oss-cve-watch-libheif}"

OUT="$WATCH_DIR/day$(printf '%02d' "$DAY")-libfuzzer-$STAMP"
mkdir -p "$OUT" logs
log() { echo "[watch-libheif-d$(printf '%02d' "$DAY")] $*" | tee -a "$OUT/run.log"; }

log "libFuzzer libheif day=$DAY max_time=${MAX_TIME}s stamp=$STAMP"

export TARGET MAX_TIME STAMP SKIP_REBUILD
CORPUS_ROOT="$ROOT/reports/oss-cve-libfuzzer/$TARGET"
SESSION="$CORPUS_ROOT/sessions/$STAMP"
mkdir -p "$SESSION"

# Bootstrap corpus from upstream OSS-Fuzz seeds once
SEEDS_UP="$ROOT/.cache/oss-cve-clones/libheif/fuzzing/data/corpus"
WORK_CORPUS="$CORPUS_ROOT/corpus"
if [[ -z "$(find "$WORK_CORPUS" -type f 2>/dev/null | head -1)" && -d "$SEEDS_UP" ]]; then
  log "bootstrap corpus from $SEEDS_UP"
  cp -n "$SEEDS_UP"/* "$WORK_CORPUS/" 2>/dev/null || cp "$SEEDS_UP"/* "$WORK_CORPUS/" || true
fi

set +e
bash "$ROOT/scripts/ops/run_oss_libfuzzer_session.sh" 2>&1 | tee -a "$OUT/run.log"
RC=$?
set -e

[[ -f "$SESSION/ROLLUP.json" ]] || fail "missing session rollup — see $OUT/run.log"
cp "$SESSION/ROLLUP.json" "$OUT/ROLLUP.json"
cp "$SESSION/SESSION.json" "$OUT/SESSION.json" 2>/dev/null || true
cp "$SESSION/fuzzer.log" "$OUT/fuzzer.log" 2>/dev/null || true
echo "$OUT" >"$WATCH_DIR/.last_out_day$(printf '%02d' "$DAY")"

if [[ "${SKIP_PUBLISH:-0}" == "1" ]]; then
  log "SKIP_PUBLISH=1 — local only ($OUT)"
else
  DAY="$DAY" OUT="$OUT" bash "$ROOT/scripts/ops/publish_oss_cve_watch_libheif_day_finish.sh"
fi

if [[ $RC -eq 1 ]]; then
  log "CVE_CANDIDATE — disclosure hold"
  exit 1
fi
log "complete verdict=$(python3 -c "import json;print(json.load(open('$OUT/ROLLUP.json'))['verdict'])")"
exit 0
