#!/usr/bin/env bash
# MemorySanitizer triage lane for CLEAN ASAN survivors.
# Hits are MSAN_TRIAGE only — never auto-claimed as CVE.
#
#   TARGETS=mpack,cwalk,tinycbor WALL_SEC=120 bash scripts/ops/run_oss_msan_session.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export HACKME_REPO_ROOT="$ROOT"
export HACKME_POOL_SEED_FROM_RESEARCH="${HACKME_POOL_SEED_FROM_RESEARCH:-0}"
export HACKME_OSS_HARNESS_VARIANT="${HACKME_OSS_HARNESS_VARIANT:-deep_v1}"

TARGETS="${TARGETS:-${TARGET:-mpack,cwalk,tinycbor}}"
WALL_SEC="${WALL_SEC:-120}"
MAX_SEEDS="${MAX_SEEDS:-256}"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
OUT_ROOT="${OUT:-$ROOT/reports/oss-cve-msan/${STAMP}}"
mkdir -p "$OUT_ROOT"

log() { echo "[oss-msan $(date -u +%H:%M:%S)] $*"; }

command -v clang >/dev/null || { echo "clang required" >&2; exit 1; }
# Soft check for MSAN runtime.
if ! clang -fsanitize=memory -x c - -o /dev/null >/dev/null 2>&1 <<<'int main(){return 0;}'; then
  echo "SKIP: clang MSAN runtime unavailable on this host" | tee "$OUT_ROOT/SKIP.txt"
  exit 0
fi

FAIL=0
IFS=',' read -r -a IDS <<< "$TARGETS"
for TID in "${IDS[@]}"; do
  TID="$(echo "$TID" | tr -d '[:space:]')"
  [[ -z "$TID" ]] && continue
  TOUT="$OUT_ROOT/$TID"
  mkdir -p "$TOUT"
  log "=== TARGET=$TID wall=${WALL_SEC}s ==="
  set +e
  go run ./cmd/oss-msan -target "$TID" -repo "$ROOT" -out "$TOUT" -wall "$WALL_SEC" -max-seeds "$MAX_SEEDS" \
    2>"$TOUT/session.stderr" | tee "$TOUT/session.stdout"
  RC=$?
  set -e
  if [[ $RC -ne 0 ]]; then
    FAIL=1
    log "FAIL TARGET=$TID rc=$RC"
  else
    log "ok TARGET=$TID $(tr '\n' ' ' <"$TOUT/session.stdout" 2>/dev/null || true)"
  fi
done

ln -sfn "$(basename "$OUT_ROOT")" "$ROOT/reports/oss-cve-msan/LATEST" 2>/dev/null || true
exit "$FAIL"
