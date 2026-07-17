#!/usr/bin/env bash
# Chain two OSS CVE Watch libFuzzer days with autopublish after each.
#
#   bash scripts/ops/run_oss_cve_watch_day_chain_autopublish.sh
#   DAY_A=10 MAX_A=18000 DAY_B=11 MAX_B=72000 bash scripts/ops/run_oss_cve_watch_day_chain_autopublish.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
mkdir -p logs

DAY_A="${DAY_A:-10}"
MAX_A="${MAX_A:-18000}"
DAY_B="${DAY_B:-11}"
MAX_B="${MAX_B:-72000}"
SKIP_REBUILD="${SKIP_REBUILD:-1}"
NODE_SSH="${NODE_SSH:-hackme-vps}"
GIT_PUSH="${GIT_PUSH:-1}"
CHAIN_STAMP="${CHAIN_STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
LOG="logs/oss-cve-watch-chain-d${DAY_A}-d${DAY_B}-${CHAIN_STAMP}.nohup.log"

run_one() {
  local day="$1" max_time="$2"
  local stamp
  stamp="$(date -u +%Y%m%dT%H%M%SZ)-d$(printf '%02d' "$day")"
  echo "[chain] === DAY $day start max_time=${max_time}s stamp=$stamp $(date -Is) ==="
  set +e
  DAY="$day" TARGET=nghttp2 MAX_TIME="$max_time" SKIP_REBUILD="$SKIP_REBUILD" \
  SKIP_PUBLISH="${SKIP_PUBLISH:-1}" STAMP="$stamp" NODE_SSH="$NODE_SSH" GIT_PUSH="$GIT_PUSH" \
    bash "$ROOT/scripts/ops/run_oss_cve_watch_day_autopublish.sh"
  local rc=$?
  set -e
  echo "[chain] === DAY $day finished rc=$rc $(date -Is) ==="
  return "$rc"
}

{
  echo "[chain] start $(date -Is) A=$DAY_A/${MAX_A}s B=$DAY_B/${MAX_B}s"
  echo "[chain] log=$LOG"
  if ! run_one "$DAY_A" "$MAX_A"; then
    echo "[chain] FATAL day $DAY_A failed — not starting day $DAY_B" >&2
    exit 3
  fi
  if ! run_one "$DAY_B" "$MAX_B"; then
    echo "[chain] FATAL day $DAY_B failed" >&2
    exit 4
  fi
  echo "[chain] ALL DONE $(date -Is)"
} >>"$LOG" 2>&1
