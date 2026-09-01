#!/usr/bin/env bash
# Soak #2 A/B: libucl Hunt Standard 200k — compare with libFuzzer L2 seeds vs overnight baseline.
#
#   bash scripts/ops/hunt_soak2_libucl_ab.sh
#   SKIP_IMPORT=1 bash scripts/ops/hunt_soak2_libucl_ab.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export HACKME_REPO_ROOT="$ROOT"

TARGET="${TARGET:-libucl}"
HUNT_ITER="${HUNT_ITER:-200000}"
WALL_SEC="${WALL_SEC:-14400}"
IMPORT_WALL="${IMPORT_WALL:-120}"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="${OUT:-$ROOT/reports/hunt-soak2-libucl/$STAMP}"
BASELINE="${BASELINE:-$ROOT/reports/hunt-overnight/20260831T191543Z/hunt-libucl.json}"
BENCH="${HUNT_BENCH_BIN:-$ROOT/bin/hunt-bench-local}"

log() { echo "[hunt-soak2 $(date -u +%H:%M:%S)] $*" | tee -a "$OUT/run.log"; }

mkdir -p "$OUT"
echo $$ >"$OUT/soak.pid"

if [[ ! -x "$BENCH" ]]; then
  go build -trimpath -o "$BENCH" ./scripts/tests/tools/hunt_bench_local.go
fi

if [[ "${SKIP_IMPORT:-0}" != "1" ]]; then
  log "1/3 libFuzzer import TARGET=$TARGET wall=${IMPORT_WALL}s"
  TARGET="$TARGET" WALL_SEC="$IMPORT_WALL" bash "$ROOT/scripts/ops/hunt_import_libfuzzer_corpus.sh" \
    >>"$OUT/import.log" 2>&1
  seed_n="$(find "$ROOT/.cache/hunt-lf-seeds/$TARGET" -type f ! -name '.*' 2>/dev/null | wc -l)"
  log "imported seeds=$seed_n dir=.cache/hunt-lf-seeds/$TARGET"
else
  log "1/3 skip import (SKIP_IMPORT=1)"
fi

log "2/3 Hunt Standard WITH seeds iter=$HUNT_ITER"
"$BENCH" \
  -target "$TARGET" \
  -package hunt_standard \
  -iter "$HUNT_ITER" \
  -wall "$WALL_SEC" \
  -out "$OUT/hunt-with-seeds.json" \
  -crashes-dir "$OUT/crashes-with-seeds" \
  -report "$OUT/hunt-report-with-seeds.json" \
  >>"$OUT/hunt-with-seeds.log" 2>&1

log "3/3 compare vs baseline"
python3 - "$OUT" "$BASELINE" "$OUT/hunt-with-seeds.json" <<'PY'
import json, os, sys
out, baseline_path, with_path = sys.argv[1:4]
def load(p):
    if os.path.isfile(p):
        return json.load(open(p))
    return {}
base = load(baseline_path)
withs = load(with_path)
crashes_base = base.get("crashes", 0)
crashes_with = withs.get("crashes", 0)
def first_iter(report_path):
    if not os.path.isfile(report_path):
        return None
    r = json.load(open(report_path))
    iters = [c.get("iteration", 0) for c in r.get("crashes", []) if c.get("iteration")]
    return min(iters) if iters else None
first_base = first_iter(baseline_path.replace("hunt-libucl.json", "hunt-report-libucl.json"))
first_with = first_iter(with_path.replace("hunt-with-seeds.json", "hunt-report-with-seeds.json"))
lines = [
    "# Hunt soak #2 — libucl with libFuzzer L2 seeds",
    "",
    f"- stamp: `{os.path.basename(out)}`",
    f"- seeds dir: `.cache/hunt-lf-seeds/libucl`",
    f"- baseline: `{baseline_path}` (overnight #1, no seeds)",
    "",
    "## Results",
    "",
    "| Run | Iterations | exec/s | Crashes | Unique sigs | First hit iter | Verdict |",
    "|-----|------------|--------|---------|-------------|----------------|---------|",
]
def row(label, d, first):
    return (f"| {label} | {d.get('iterations','?')} | {d.get('exec_per_sec',0):.1f} "
            f"| {d.get('crashes','?')} | {d.get('unique_signatures','?')} "
            f"| {first if first is not None else '—'} | {d.get('verdict','?')} |")
lines.append(row("overnight #1 (no seeds)", base, first_base))
lines.append(row("soak #2 (with seeds)", withs, first_with))
lines += [
    "",
    "## Interpretation",
    "",
]
if first_with and first_base:
    if first_with < first_base:
        lines.append(f"- **Seeds helped:** first hit {first_base} → **{first_with}** iter ({100*(first_base-first_with)/first_base:.0f}% faster).")
    elif first_with > first_base:
        lines.append(f"- First hit slower with seeds ({first_base} → {first_with}) — same UBSan class expected; variance normal.")
    else:
        lines.append(f"- Same first-hit iter ({first_with}) — seeds may help stability more than first-hit on this target.")
else:
    lines.append("- Compare `unique_signatures` and first-hit iter in crash reports.")
lines += [
    "- `unique_signatures` should stay ~1 for libucl (same UBSan root cause).",
    "- New ASAN heap class would be a jackpot.",
    "",
]
md = "\n".join(lines)
open(os.path.join(out, "REPORT.md"), "w").write(md)
print(md)
summary = {
    "baseline_crashes": crashes_base,
    "with_seeds_crashes": crashes_with,
    "baseline_first_iter": first_base,
    "with_seeds_first_iter": first_with,
    "baseline_unique_sig": base.get("unique_signatures"),
    "with_seeds_unique_sig": withs.get("unique_signatures"),
}
json.dump(summary, open(os.path.join(out, "summary.json"), "w"), indent=2)
PY

log "DONE → $OUT/REPORT.md"
