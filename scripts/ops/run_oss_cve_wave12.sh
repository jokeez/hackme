#!/usr/bin/env bash
# Wave 12 — binary/regex/HTTP/URI parsers not covered in wave11.
#
#   bash scripts/ops/run_oss_cve_wave12.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="${OUT:-$ROOT/reports/oss-cve/wave12-${STAMP}}"
TARGETS="${TARGETS:-pcre2,picohttpparser,yajl,json-c,cmp,uriparser}"
BUDGET="${BUDGET:-50000}"
TIME_LIMIT="${TIME_LIMIT:-3600}"
SKIP_PACK_BUILD="${SKIP_PACK_BUILD:-0}"

mkdir -p "$OUT"
log() { echo "[wave12 $(date -u +%H:%M:%S)] $*" | tee -a "$OUT/wave12.log"; }

log "preflight build (selected targets)"
HACKME_REPO_ROOT="$ROOT" go test ./internal/fuzzupstream/... -count=1 \
  -run 'TestBuildAllTargets' -timeout 45m >>"$OUT/build.log" 2>&1 || true

log "hunt targets=$TARGETS budget=$BUDGET time=$TIME_LIMIT"
export TARGETS BUDGET TIME_LIMIT OUT STAMP SKIP_PACK_BUILD
set +e
bash "$ROOT/scripts/ops/run_oss_cve_hunt.sh" >>"$OUT/hunt.log" 2>&1
RC=$?
set -e

if [[ -f "$OUT/ROLLUP.json" ]]; then
  ln -sfn "$(basename "$OUT")" "$ROOT/reports/oss-cve/CURRENT-wave12"
  python3 "$ROOT/scripts/ops/export_oss_cve_html.py" "$OUT" >>"$OUT/hunt.log" 2>&1 || true
fi
log "verdict rc=$RC — $OUT/ROLLUP.json"
exit "$RC"
