#!/usr/bin/env bash
# Wave 11 — parsers/binary not covered in wave10 + high-priority markdown/XML.
#
#   bash scripts/ops/run_oss_cve_wave11.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="${OUT:-$ROOT/reports/oss-cve/wave11-${STAMP}}"
TARGETS="${TARGETS:-md4c,mjson,yyjson,parson,jansson,sheredom,expat,cyaml,tomlc17,libyaml,cmark,mxml,miniz,oniguruma,zlib,nghttp2,libxml2,duktape}"
BUDGET="${BUDGET:-120000}"
TIME_LIMIT="${TIME_LIMIT:-7200}"
SKIP_PACK_BUILD="${SKIP_PACK_BUILD:-0}"

mkdir -p "$OUT"
log() { echo "[wave11 $(date -u +%H:%M:%S)] $*" | tee -a "$OUT/wave11.log"; }

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
  ln -sfn "$(basename "$OUT")" "$ROOT/reports/oss-cve/CURRENT"
  python3 "$ROOT/scripts/ops/export_oss_cve_html.py" "$OUT" >>"$OUT/hunt.log" 2>&1 || true
fi
log "verdict rc=$RC — $OUT/ROLLUP.json"
exit "$RC"
