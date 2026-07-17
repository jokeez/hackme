#!/usr/bin/env bash
# Day N libFuzzer run + auto publish/deploy/news/telegram when finished.
# Must be started with setsid outside agent sandbox so the fuzzer is not killed.
#
#   DAY=9 MAX_TIME=14400 bash scripts/ops/run_oss_cve_watch_day_autopublish.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
mkdir -p logs

DAY="${DAY:-9}"
MAX_TIME="${MAX_TIME:-14400}"
SKIP_REBUILD="${SKIP_REBUILD:-1}"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)-d$(printf '%02d' "$DAY")}"
LOG="logs/oss-cve-watch-day$(printf '%02d' "$DAY")-autopublish-${STAMP}.nohup.log"
NODE_SSH="${NODE_SSH:-hackme-vps}"
GIT_PUSH="${GIT_PUSH:-1}"

{
  echo "[autopublish] start $(date -Is) DAY=$DAY MAX_TIME=$MAX_TIME STAMP=$STAMP"
  echo "[autopublish] log=$LOG"

  set +e
  DAY="$DAY" TARGET=nghttp2 MAX_TIME="$MAX_TIME" SKIP_REBUILD="$SKIP_REBUILD" \
    SKIP_PUBLISH=1 STAMP="$STAMP" \
    bash "$ROOT/scripts/ops/run_oss_cve_watch_libfuzzer_day.sh"
  RC=$?
  set -e
  echo "[autopublish] fuzzer day exit rc=$RC at $(date -Is)"

  OUT="$ROOT/reports/oss-cve-watch/day$(printf '%02d' "$DAY")-libfuzzer-$STAMP"
  if [[ ! -f "$OUT/ROLLUP.json" ]]; then
    echo "[autopublish] ERROR missing ROLLUP at $OUT — abort publish" >&2
    exit 3
  fi

  DAY="$DAY" OUT="$OUT" NODE_SSH="$NODE_SSH" GIT_PUSH="$GIT_PUSH" \
    bash "$ROOT/scripts/ops/publish_oss_cve_watch_day_finish.sh"
  echo "[autopublish] ALL DONE $(date -Is)"
} >>"$LOG" 2>&1
