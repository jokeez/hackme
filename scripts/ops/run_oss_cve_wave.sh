#!/usr/bin/env bash
# Generic OSS CVE wave runner — targets from rank_oss_cve_targets.py → upstream/oss_cve_high_yield.json
#
#   WAVE=15 bash scripts/ops/run_oss_cve_wave.sh
#   WAVE=16 TARGETS=pcre2,lua BUDGET=120000 bash scripts/ops/run_oss_cve_wave.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

WAVE="${WAVE:-15}"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="${OUT:-$ROOT/reports/oss-cve/wave${WAVE}-${STAMP}}"
RANK_JSON="$ROOT/upstream/oss_cve_high_yield.json"
TOP="${TOP:-14}"
WAVE_KEY="wave${WAVE}"

if [[ -z "${TARGETS:-}" ]]; then
  python3 "$ROOT/scripts/ops/rank_oss_cve_targets.py" --wave "$WAVE" --top "$TOP" \
    --out "$ROOT/reports/oss-cve/cve-rank-wave${WAVE}-${STAMP}.md"
  read -r _TARGETS _BUDGET _TIME_LIMIT < <(python3 - "$RANK_JSON" "$WAVE_KEY" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
w = d.get(sys.argv[2], {})
print(
    ",".join(w.get("targets") or []),
    int(w.get("budget_iterations") or 150000),
    int(w.get("time_limit_sec") or 5400),
)
PY
  )
  TARGETS="$_TARGETS"
  BUDGET="${BUDGET:-$_BUDGET}"
  TIME_LIMIT="${TIME_LIMIT:-$_TIME_LIMIT}"
fi

if [[ -z "${TARGETS:-}" ]]; then
  echo "[wave${WAVE}] no targets in $RANK_JSON ($WAVE_KEY)" >&2
  exit 2
fi

BUDGET="${BUDGET:-150000}"
TIME_LIMIT="${TIME_LIMIT:-5400}"

mkdir -p "$OUT"
log() { echo "[wave${WAVE} $(date -u +%H:%M:%S)] $*" | tee -a "$OUT/wave${WAVE}.log"; }

log "easy-target hunt targets=$TARGETS budget=$BUDGET time=$TIME_LIMIT"
log "rank file=$RANK_JSON key=$WAVE_KEY"

log "preflight build ($TARGETS)"
TARGETS="$TARGETS" bash "$ROOT/scripts/ops/build_oss_cve_pack.sh" >>"$OUT/build.log" 2>&1

export TARGETS BUDGET TIME_LIMIT OUT STAMP SKIP_PACK_BUILD=1
set +e
bash "$ROOT/scripts/ops/run_oss_cve_hunt.sh" >>"$OUT/hunt.log" 2>&1
RC=$?
set -e

if [[ -f "$OUT/ROLLUP.json" ]]; then
  ln -sfn "$(basename "$OUT")" "$ROOT/reports/oss-cve/CURRENT-wave${WAVE}"
  ln -sfn "$(basename "$OUT")" "$ROOT/reports/oss-cve/CURRENT"
  python3 "$ROOT/scripts/ops/export_oss_cve_html.py" "$OUT" >>"$OUT/hunt.log" 2>&1 || true
fi
log "verdict rc=$RC — $OUT/ROLLUP.json"
exit "$RC"
