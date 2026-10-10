#!/usr/bin/env bash
# Persistent libFuzzer research session for catalog targets.
# Corpus lives under reports/oss-cve-libfuzzer/<target>/corpus/ across nights.
#
#   TARGET=mpack WALL_SEC=120 bash scripts/ops/run_oss_libfuzzer_session.sh
#   TARGETS=mpack,tinycbor,cwalk WALL_SEC=300 bash scripts/ops/run_oss_libfuzzer_session.sh
#
# Customer pool dig seed feed stays off (HACKME_POOL_SEED_FROM_RESEARCH=0).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export HACKME_REPO_ROOT="$ROOT"
export HACKME_POOL_SEED_FROM_RESEARCH="${HACKME_POOL_SEED_FROM_RESEARCH:-0}"

TARGETS="${TARGETS:-${TARGET:-mpack,tinycbor,cwalk}}"
WALL_SEC="${WALL_SEC:-120}"
MIN_WALL_DETECT="${MIN_WALL_DETECT:-90}"

log() { echo "[oss-lf-session $(date -u +%H:%M:%S)] $*"; }

command -v clang >/dev/null || { echo "clang required" >&2; exit 1; }
command -v go >/dev/null || { echo "go required" >&2; exit 1; }

IFS=',' read -r -a IDS <<< "$TARGETS"
FAIL=0
for TID in "${IDS[@]}"; do
  TID="$(echo "$TID" | tr -d '[:space:]')"
  [[ -z "$TID" ]] && continue
  CORPUS="$ROOT/reports/oss-cve-libfuzzer/$TID/corpus"
  mkdir -p "$CORPUS"
  BEFORE=$(find "$CORPUS" -type f ! -name '.*' 2>/dev/null | wc -l | tr -d ' ')
  log "=== TARGET=$TID wall=${WALL_SEC}s corpus_before=$BEFORE ==="
  set +e
  OUT=$(go run ./cmd/hunt-lf-import -target "$TID" -wall "$WALL_SEC" -persist -repo "$ROOT" 2>"$ROOT/reports/oss-cve-libfuzzer/$TID/session.stderr")
  RC=$?
  set -e
  AFTER=$(find "$CORPUS" -type f ! -name '.*' 2>/dev/null | wc -l | tr -d ' ')
  log "TARGET=$TID rc=$RC imported_seeds=${OUT:-0} corpus_after=$AFTER"
  if [[ -f "$ROOT/reports/oss-cve-libfuzzer/$TID/session_stats.json" ]]; then
    log "stats $(tr '\n' ' ' <"$ROOT/reports/oss-cve-libfuzzer/$TID/session_stats.json")"
  fi
  if [[ $RC -ne 0 ]]; then
    FAIL=1
    log "FAIL TARGET=$TID — see reports/oss-cve-libfuzzer/$TID/session.stderr"
    continue
  fi
  if (( WALL_SEC >= MIN_WALL_DETECT )) && (( AFTER == 0 )); then
    FAIL=1
    log "FAIL TARGET=$TID empty corpus after ${WALL_SEC}s (broken harness)"
  fi
done

exit "$FAIL"
