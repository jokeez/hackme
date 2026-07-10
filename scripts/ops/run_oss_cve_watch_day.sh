#!/usr/bin/env bash
# OSS CVE Watch — 14-day single-repo hunt (nghttp2) + public site export.
#
#   DAY=1 bash scripts/ops/run_oss_cve_watch_day.sh
#   DAY=2 BUDGET=50000 bash scripts/ops/run_oss_cve_watch_day.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

DAY="${DAY:-1}"
TARGET="${TARGET:-nghttp2}"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="${OUT:-$ROOT/reports/oss-cve-watch/day$(printf '%02d' "$DAY")-$STAMP}"

# Budget ramp — override with BUDGET= / TIME_LIMIT= env.
if [[ -z "${BUDGET:-}" || -z "${TIME_LIMIT:-}" ]]; then
  case "$DAY" in
    1|2|3) BUDGET="${BUDGET:-20000}"; TIME_LIMIT="${TIME_LIMIT:-600}" ;;
    4|5|6|7) BUDGET="${BUDGET:-50000}"; TIME_LIMIT="${TIME_LIMIT:-1800}" ;;
    8|9|10) BUDGET="${BUDGET:-100000}"; TIME_LIMIT="${TIME_LIMIT:-3600}" ;;
    *) BUDGET="${BUDGET:-150000}"; TIME_LIMIT="${TIME_LIMIT:-5400}" ;;
  esac
fi

require_cmd clang git go python3

mkdir -p "$OUT" logs
log() { echo "[cve-watch-d$(printf '%02d' "$DAY")] $*" | tee -a "$OUT/run.log"; }

log "preflight build driver target=$TARGET"
HACKME_REPO_ROOT="$ROOT" go test ./internal/fuzzupstream/... -count=1 \
  -run "TestBuildAllTargets/${TARGET}$" -timeout 20m >>"$OUT/build.log" 2>&1

log "hunt target=$TARGET budget=$BUDGET time_limit=${TIME_LIMIT}s out=$OUT"
export TARGETS="$TARGET" BUDGET TIME_LIMIT OUT STAMP SKIP_PACK_BUILD=1
set +e
bash "$ROOT/scripts/ops/run_oss_cve_hunt.sh" 2>&1 | tee -a "$OUT/hunt.log"
RC=${PIPESTATUS[0]}
set -e

if [[ ! -f "$OUT/ROLLUP.json" ]]; then
  fail "missing ROLLUP.json — see $OUT/hunt.log"
fi

if [[ "${SKIP_PUBLISH:-0}" == "1" ]]; then
  log "SKIP_PUBLISH=1 — report kept local only ($OUT); publish later: python3 scripts/ops/export_oss_cve_watch_html.py $DAY $OUT"
else
  python3 "$ROOT/scripts/ops/export_oss_cve_watch_html.py" "$DAY" "$OUT"
  log "site export ok — web/site/reports/oss-cve-watch/day$(printf '%02d' "$DAY").html"
fi

if [[ $RC -eq 1 ]]; then
  log "CVE_CANDIDATE — disclosure hold; HTML exported with banner"
  exit 1
fi
if [[ $RC -ne 0 ]]; then
  fail "hunt failed rc=$RC"
fi
log "CLEAN/INFORMATIONAL complete"
exit 0
