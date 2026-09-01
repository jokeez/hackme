#!/usr/bin/env bash
# Hunt pilot: 1h local Standard run on a small external GitHub target (default: spl).
#
#   TARGET=spl bash scripts/ops/hunt_pilot_external_1h.sh
#   TARGET=spl WALL_SEC=3600 HUNT_ITER=400000 bash scripts/ops/hunt_pilot_external_1h.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export HACKME_REPO_ROOT="$ROOT"

TARGET="${TARGET:-spl}"
HUNT_ITER="${HUNT_ITER:-400000}"
WALL_SEC="${WALL_SEC:-3700}"
PKG="${HUNT_PACKAGE:-hunt_standard}"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="${OUT:-$ROOT/reports/hunt-pilot-$TARGET/$STAMP}"
BENCH="${HUNT_BENCH_BIN:-$ROOT/bin/hunt-bench-local}"

log() { echo "[hunt-pilot $(date -u +%H:%M:%S)] $*" | tee -a "$OUT/run.log"; }

mkdir -p "$OUT"
echo $$ >"$OUT/pilot.pid"

if [[ ! -x "$BENCH" ]]; then
  go build -trimpath -o "$BENCH" ./scripts/tests/tools/hunt_bench_local.go
fi

log "target=$TARGET package=$PKG iter=$HUNT_ITER wall=${WALL_SEC}s out=$OUT"
log "prebuild ASAN harness (spl catalog)"
go test -count=1 ./internal/fuzzupstream/ -run "TestBuildAllTargets/${TARGET}$" -timeout 10m >/dev/null

"$BENCH" \
  -target "$TARGET" \
  -package "$PKG" \
  -iter "$HUNT_ITER" \
  -wall "$WALL_SEC" \
  -out "$OUT/hunt-$TARGET.json" \
  -crashes-dir "$OUT/crashes-$TARGET" \
  -report "$OUT/hunt-report-$TARGET.json" \
  2>&1 | tee -a "$OUT/hunt-$TARGET.log"

python3 - "$OUT" "$TARGET" "$OUT/hunt-$TARGET.json" "$OUT/hunt-report-$TARGET.json" <<'PY'
import json, os, sys
out, target, summary_path, report_path = sys.argv[1:5]
def load(p):
    return json.load(open(p)) if os.path.isfile(p) else {}
s = load(summary_path)
r = load(report_path)
first = None
for c in r.get("crashes", []):
    it = c.get("iteration")
    if it:
        first = it if first is None else min(first, it)
lines = [
    f"# Hunt pilot — {target} (1h external)",
    "",
    f"- stamp: `{os.path.basename(out)}`",
    f"- repo: catalog target `{target}` in upstream/oss_cve_targets.json",
    "",
    "## Results",
    "",
    f"| Target | Iterations | exec/s | Crashes | Unique sigs | First hit | Verdict |",
    f"|--------|------------|--------|---------|-------------|-----------|---------|",
    f"| {target} | {s.get('iterations','?')} | {s.get('exec_per_sec',0):.1f} | {s.get('crashes','?')} | {s.get('unique_signatures','?')} | {first or '—'} | {s.get('verdict','?')} |",
    "",
]
if s.get("sanitizer_signatures"):
    lines.append("### Sanitizer classes")
    lines.append("")
    for k, v in sorted(s.get("sanitizer_signatures", {}).items()):
        lines.append(f"- `{k}`: {v}")
    lines.append("")
md = "\n".join(lines)
open(os.path.join(out, "REPORT.md"), "w").write(md)
print(md)
PY

log "DONE → $OUT/REPORT.md"
