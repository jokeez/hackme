#!/usr/bin/env bash
# A/B calibrate shallow vs deep_v1 harnesses for obscure-wave targets.
#
#   TARGETS=mpack,tinycbor,cwalk,libcbor BUDGET=8000 TIME_LIMIT=120 \
#     bash scripts/ops/calibrate_oss_deep_harness.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export HACKME_REPO_ROOT="$ROOT"
export HACKME_POOL_SEED_FROM_RESEARCH="${HACKME_POOL_SEED_FROM_RESEARCH:-0}"

TARGETS="${TARGETS:-mpack,tinycbor,cwalk,libcbor}"
BUDGET="${BUDGET:-8000}"
TIME_LIMIT="${TIME_LIMIT:-120}"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="${OUT:-$ROOT/reports/oss-cve/deep-calibrate-${STAMP}}"
mkdir -p "$OUT"
log() { echo "[deep-calibrate $(date -u +%H:%M:%S)] $*" | tee -a "$OUT/calibrate.log"; }

SUMMARY="$OUT/ab_summary.tsv"
echo -e "target\tvariant\trc\tverdict\titers\tcrashes\tasan\tdriver" >"$SUMMARY"

run_variant() {
  local TID="$1" VARIANT="$2"
  local TOUT="$OUT/${TID}-${VARIANT}"
  mkdir -p "$TOUT"
  log "=== $TID variant=$VARIANT budget=$BUDGET wall=${TIME_LIMIT}s ==="
  set +e
  HACKME_OSS_HARNESS_VARIANT="$VARIANT" \
    TARGETS="$TID" BUDGET="$BUDGET" TIME_LIMIT="$TIME_LIMIT" \
    STAMP="${STAMP}-${TID}-${VARIANT}" OUT="$TOUT" \
    bash "$ROOT/scripts/ops/run_oss_cve_hunt.sh" >>"$OUT/calibrate.log" 2>&1
  local RC=$?
  set -e
  local VERDICT="—" ITERS=0 CRASHES=0 ASAN=0 DRIVER=""
  if [[ -f "$TOUT/ROLLUP.json" ]]; then
    read -r VERDICT ITERS CRASHES ASAN DRIVER < <(python3 -c "
import json
r=json.load(open('$TOUT/ROLLUP.json'))
t=(r.get('targets') or [{}])[0]
cr=t.get('crashes') or []
asan=sum(1 for c in cr if (c.get('sanitizer_class')=='asan') or 'AddressSanitizer' in (c.get('sanitizer') or ''))
print(r.get('verdict','?'), t.get('iterations',0), len(cr), asan, t.get('driver') or '')
") || true
  fi
  echo -e "${TID}\t${VARIANT}\t${RC}\t${VERDICT}\t${ITERS}\t${CRASHES}\t${ASAN}\t${DRIVER}" >>"$SUMMARY"
  log "$TID $VARIANT rc=$RC verdict=$VERDICT iters=$ITERS crashes=$CRASHES driver=$DRIVER"
}

IFS=',' read -r -a IDS <<< "$TARGETS"
for TID in "${IDS[@]}"; do
  TID="$(echo "$TID" | tr -d '[:space:]')"
  [[ -z "$TID" ]] && continue
  run_variant "$TID" "shallow"
  run_variant "$TID" "deep_v1"
done

log "summary $SUMMARY"
column -t -s $'\t' "$SUMMARY" 2>/dev/null || cat "$SUMMARY"
