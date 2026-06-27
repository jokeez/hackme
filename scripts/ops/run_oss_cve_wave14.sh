#!/usr/bin/env bash
# Wave 14 — high-yield OSS CVE hunt (ASAN-class targets, deep budget).
#
# Targets auto-selected by rank_oss_cve_targets.py → upstream/oss_cve_high_yield.json
#
#   bash scripts/ops/run_oss_cve_wave14.sh
#   TARGETS=libucl,oniguruma,libxml2 bash scripts/ops/run_oss_cve_wave14.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="${OUT:-$ROOT/reports/oss-cve/wave14-${STAMP}}"
RANK_JSON="$ROOT/upstream/oss_cve_high_yield.json"

python3 "$ROOT/scripts/ops/rank_oss_cve_targets.py" --wave 14 --top 10 --out "$ROOT/reports/oss-cve/cve-rank-${STAMP}.md"

if [[ -z "${TARGETS:-}" ]]; then
  TARGETS="$(python3 -c "import json; d=json.load(open('$RANK_JSON')); print(','.join(d['wave14']['targets']))")"
fi
BUDGET="${BUDGET:-250000}"
TIME_LIMIT="${TIME_LIMIT:-7200}"

mkdir -p "$OUT"
log() { echo "[wave14 $(date -u +%H:%M:%S)] $*" | tee -a "$OUT/wave14.log"; }

log "high-yield hunt targets=$TARGETS budget=$BUDGET time=$TIME_LIMIT"
log "rank file=$RANK_JSON"

log "preflight build ($TARGETS)"
TARGETS="$TARGETS" bash "$ROOT/scripts/ops/build_oss_cve_pack.sh" >>"$OUT/build.log" 2>&1

export TARGETS BUDGET TIME_LIMIT OUT STAMP SKIP_PACK_BUILD=1
set +e
bash "$ROOT/scripts/ops/run_oss_cve_hunt.sh" >>"$OUT/hunt.log" 2>&1
RC=$?
set -e

if [[ -f "$OUT/ROLLUP.json" ]]; then
  ln -sfn "$(basename "$OUT")" "$ROOT/reports/oss-cve/CURRENT-wave14"
  python3 "$ROOT/scripts/ops/export_oss_cve_html.py" "$OUT" >>"$OUT/hunt.log" 2>&1 || true
fi
log "verdict rc=$RC — $OUT/ROLLUP.json"
exit "$RC"
