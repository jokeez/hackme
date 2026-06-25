#!/usr/bin/env bash
# Nightly OSS CVE hunt — rotate 2 targets per day, skip centijson until disclosed.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT="${OUT:-$ROOT/reports/oss-cve/nightly-${STAMP}}"
BUDGET="${BUDGET:-30000}"
TIME_LIMIT="${TIME_LIMIT:-300}"
SKIP_IDS="${SKIP_IDS:-centijson,libucl,cfgpack}"

DAY_OFFSET="$(date -u +%j)"
TARGETS="$(python3 - "$ROOT" "$DAY_OFFSET" <<'PY'
import json, sys
root, day = sys.argv[1], int(sys.argv[2])
m = json.loads((__import__("pathlib").Path(root) / "upstream/oss_cve_targets.json").read_text())
q = (m.get("rotation") or {}).get("queue") or []
if not q:
    raise SystemExit(0)
n = len(q)
start = day % n
ids = [q[(start + i) % n] for i in range(2)]
print(",".join(ids))
PY
)"

[[ -n "$TARGETS" ]] || { echo "[oss-cve-nightly] no rotation queue"; exit 0; }

if [[ -n "$SKIP_IDS" ]]; then
  FILTERED=""
  IFS=',' read -r -a _IDS <<< "$TARGETS"
  IFS=',' read -r -a _SKIP <<< "$SKIP_IDS"
  for id in "${_IDS[@]}"; do
    skip=0
    for s in "${_SKIP[@]}"; do [[ "$id" == "$s" ]] && skip=1; done
    [[ "$skip" -eq 1 ]] && continue
    FILTERED="${FILTERED:+$FILTERED,}$id"
  done
  TARGETS="$FILTERED"
fi
[[ -n "$TARGETS" ]] || { echo "[oss-cve-nightly] all targets skipped (SKIP_IDS)"; exit 0; }

log() { echo "[oss-cve-nightly $(date -u +%H:%M:%S)] $*"; }
log "targets=$TARGETS day_offset=$DAY_OFFSET budget=$BUDGET"

export SKIP_IDS
TARGETS="$TARGETS" BUDGET="$BUDGET" TIME_LIMIT="$TIME_LIMIT" OUT="$OUT" \
  bash "$ROOT/scripts/ops/run_oss_cve_hunt.sh" || RC=$?
RC=${RC:-0}

if [[ -f "$OUT/ROLLUP.json" ]] && jq -e '.verdict == "CVE_CANDIDATE"' "$OUT/ROLLUP.json" >/dev/null 2>&1; then
  log "CVE_CANDIDATE — disclosure HOLD (no public detail bump)"
  # Refresh site index with HOLD banner only
  python3 "$ROOT/scripts/ops/export_oss_cve_html.py" "$OUT" || true
fi

exit "$RC"
