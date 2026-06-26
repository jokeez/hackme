#!/usr/bin/env bash
# Wave 8 — HTTP/XML/JS parsers: nghttp2, libxml2, duktape, libyaml (150k each).
#
#   bash scripts/ops/run_oss_cve_wave8.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="${OUT:-$ROOT/reports/oss-cve/wave8-${STAMP}}"
TARGETS="${TARGETS:-nghttp2,libxml2,duktape,libyaml}"
BUDGET="${BUDGET:-150000}"
TIME_LIMIT="${TIME_LIMIT:-10800}"
SKIP_PACK_BUILD="${SKIP_PACK_BUILD:-1}"

mkdir -p "$OUT"
log() { echo "[wave8 $(date -u +%H:%M:%S)] $*" | tee -a "$OUT/wave8.log"; }

log "preflight build"
HACKME_REPO_ROOT="$ROOT" go test ./internal/fuzzupstream/... -count=1 \
  -run 'TestBuildAllTargets/(nghttp2|libxml2|duktape|libyaml)$' -timeout 25m >>"$OUT/build.log" 2>&1

if [[ "${DRY_BUILD:-0}" == "1" ]]; then
  log "DRY_BUILD=1 — skip hunt"
  exit 0
fi

log "hunt targets=$TARGETS budget=$BUDGET time=$TIME_LIMIT"
export TARGETS BUDGET TIME_LIMIT OUT STAMP SKIP_PACK_BUILD
set +e
bash "$ROOT/scripts/ops/run_oss_cve_hunt.sh" >>"$OUT/hunt.log" 2>&1
RC=$?
set -e

if [[ -f "$OUT/ROLLUP.json" ]]; then
  ln -sfn "$(basename "$OUT")" "$ROOT/reports/oss-cve/CURRENT"
fi
log "verdict rc=$RC — $OUT/ROLLUP.json"
exit "$RC"
