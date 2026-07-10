#!/usr/bin/env bash
# Background CVE work — local reports only (no site publish). Use between public watch days.
#
#   SKIP_PUBLISH=1 DAY=2 bash scripts/ops/run_oss_cve_watch_day.sh
#   SKIP_PUBLISH=1 bash scripts/ops/run_oss_cve_watch_background.sh
#
# Publish when the calendar day is ready:
#   python3 scripts/ops/export_oss_cve_watch_html.py 2 reports/oss-cve-watch/day02-.../
#   NODE_SSH=hackme-vps SKIP_DIST=1 bash scripts/ops/deploy_hackme_site.sh
set -u
export PATH="/home/kapa/.local/bin:$HOME/.cargo/bin:$PATH"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export SKIP_PUBLISH=1
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
LOG="$ROOT/logs/oss-cve-background-${STAMP}.nohup.log"

log() { echo "[cve-bg $(date -u +%H:%M:%S)] $*" | tee -a "$LOG"; }

log "=== background CVE (no site publish) stamp=$STAMP ==="

# Finish in-flight watch day if env set, else default: deep nghttp2 only
DAY="${DAY:-2}"
TARGET="${TARGET:-nghttp2}"
BUDGET="${BUDGET:-100000}"
TIME_LIMIT="${TIME_LIMIT:-10800}"

log "watch day=$DAY target=$TARGET budget=$BUDGET (local only)"
set +e
DAY="$DAY" TARGET="$TARGET" BUDGET="$BUDGET" TIME_LIMIT="$TIME_LIMIT" SKIP_PUBLISH=1 \
  bash "$ROOT/scripts/ops/run_oss_cve_watch_day.sh" >>"$LOG" 2>&1
RC=$?
set -e
log "watch day $DAY rc=$RC"

# Optional: queue next repo for a future public day (reports/oss-cve only — not watch ledger)
if [[ "${RUN_EXTRA_HUNT:-1}" == "1" ]]; then
  EXTRA="${EXTRA_TARGETS:-md4c,cjson}"
  OUT="$ROOT/reports/oss-cve/background-${STAMP}-${EXTRA//,/-}"
  log "extra hunt targets=$EXTRA (oss-cve matrix, not watch day)"
  sleep 30
  set +e
  TARGETS="$EXTRA" BUDGET="${EXTRA_BUDGET:-60000}" TIME_LIMIT="${EXTRA_TIME:-3600}" OUT="$OUT" SKIP_PACK_BUILD=1 \
    bash "$ROOT/scripts/ops/run_oss_cve_hunt.sh" >>"$LOG" 2>&1
  set -e
fi

log "=== background done — publish watch days manually when ready ==="
