#!/usr/bin/env bash
# OSS CVE hunt — real upstream ASAN mutation fuzz on cloned repos.
#
#   bash scripts/ops/run_oss_cve_hunt.sh
#   TARGETS=md4c,cjson BUDGET=20000 TIME_LIMIT=300 bash scripts/ops/run_oss_cve_hunt.sh
#   PRIORITY_MAX=1 bash scripts/ops/run_oss_cve_hunt.sh   # md4c, cjson, centijson only
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

TARGETS="${TARGETS:-all}"
BUDGET="${BUDGET:-0}"
TIME_LIMIT="${TIME_LIMIT:-0}"
PRIORITY_MAX="${PRIORITY_MAX:-0}"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="${OUT:-$ROOT/reports/oss-cve/${STAMP}}"

require_cmd clang git go

command -v clang >/dev/null || fail "clang required for OSS CVE hunt"

mkdir -p "$OUT"
log() { echo "[oss-cve $(date -u +%H:%M:%S)] $*"; }

if [[ "${SKIP_PACK_BUILD:-0}" == "1" ]]; then
  if [[ -z "$TARGETS" || "$TARGETS" == "all" ]]; then
    fail "SKIP_PACK_BUILD=1 requires explicit TARGETS=..."
  fi
  log "preflight: build selected targets ($TARGETS)"
  TARGETS="$TARGETS" bash "$ROOT/scripts/ops/build_oss_cve_pack.sh" >>"$OUT/build.log" 2>&1
else
  log "preflight: build OSS CVE pack (clone + compile drivers)"
  bash "$ROOT/scripts/ops/build_oss_cve_pack.sh" >>"$OUT/build.log" 2>&1
fi

ARGS=(-repo "$ROOT" -out "$OUT" -targets "$TARGETS")
[[ "$BUDGET" -gt 0 ]] && ARGS+=(-budget "$BUDGET")
[[ "$TIME_LIMIT" -gt 0 ]] && ARGS+=(-time-limit "$TIME_LIMIT")
[[ "$PRIORITY_MAX" -gt 0 ]] && ARGS+=(-priority-max "$PRIORITY_MAX")

log "hunt targets=$TARGETS budget=${BUDGET:-default} time=${TIME_LIMIT:-default}"
set +e
HACKME_REPO_ROOT="$ROOT" go run ./tools/oss_cve_hunt/ "${ARGS[@]}" 2>&1 | tee "$OUT/hunt.log"
RC=${PIPESTATUS[0]}
set -e

ln -sfn "$(basename "$OUT")" "$ROOT/reports/oss-cve/CURRENT"

if [[ -f "$OUT/ROLLUP.json" ]]; then
  python3 - "$OUT" <<'PY'
import json, pathlib, sys
out = pathlib.Path(sys.argv[1])
r = json.loads((out / "ROLLUP.json").read_text())
html = f"""<!DOCTYPE html><html><head><meta charset=utf-8><title>OSS CVE Hunt</title>
<style>body{{font-family:system-ui;max-width:900px;margin:2rem auto;padding:0 1rem}}
table{{border-collapse:collapse;width:100%}}td,th{{border:1px solid #ccc;padding:6px}}
.cve{{color:#c00;font-weight:bold}}.clean{{color:#080}}</style></head><body>
<h1>OSS CVE Hunt</h1><p><strong>Verdict:</strong> <span class="{'cve' if r.get('verdict')=='CVE_CANDIDATE' else 'clean'}">{r.get('verdict')}</span></p>
<p>{r.get('summary','')}</p>
<h2>Targets</h2><table><tr><th>ID</th><th>Iterations</th><th>Crashes</th><th>Verdict</th></tr>"""
for t in r.get("targets", []):
    html += f"<tr><td>{t.get('target_id')}</td><td>{t.get('iterations')}</td><td>{len(t.get('crashes',[]))}</td><td>{t.get('verdict')}</td></tr>"
html += "</table></body></html>"
(out / "index.html").write_text(html)
print("wrote", out / "index.html")
PY
fi

if [[ -f "$OUT/ROLLUP.json" ]]; then
  python3 "$ROOT/scripts/ops/export_oss_cve_html.py" "$OUT" >>"$OUT/hunt.log" 2>&1 || true
fi

if [[ $RC -eq 1 ]]; then
  log "CVE_CANDIDATE — see $OUT/ROLLUP.json (responsible disclosure HOLD)"
  exit 1
fi
if [[ $RC -ne 0 ]]; then
  fail "hunt failed rc=$RC — see $OUT/hunt.log"
fi
log "CLEAN — $OUT/ROLLUP.json"
exit 0
