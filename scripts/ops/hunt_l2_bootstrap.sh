#!/usr/bin/env bash
# Batch import libFuzzer corpora into Hunt L2 seed cache for catalog targets.
#
#   bash scripts/ops/hunt_l2_bootstrap.sh
#   TARGETS="spl,parsello,jsmn" WALL_SEC=60 bash scripts/ops/hunt_l2_bootstrap.sh
#   IMPORT_ONLY=1 TARGETS=libucl bash scripts/ops/hunt_l2_bootstrap.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export HACKME_REPO_ROOT="$ROOT"

TARGETS="${TARGETS:-spl,parsello,jsmn,centijson,jsonparser,microtar}"
WALL_SEC="${WALL_SEC:-90}"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="${OUT:-$ROOT/reports/hunt-l2-bootstrap/$STAMP}"

log() { echo "[hunt-l2-bootstrap $(date -u +%H:%M:%S)] $*" | tee -a "$OUT/run.log"; }

mkdir -p "$OUT"
log "targets=$TARGETS wall=${WALL_SEC}s import_only=${IMPORT_ONLY:-0}"

IFS=',' read -r -a arr <<<"$TARGETS"
ok=0
fail=0
for tid in "${arr[@]}"; do
  tid="$(echo "$tid" | tr -d ' ')"
  [[ -n "$tid" ]] || continue
  log "import $tid"
  set +e
  n="$(TARGET="$tid" WALL_SEC="$WALL_SEC" IMPORT_ONLY="${IMPORT_ONLY:-0}" \
    bash "$ROOT/scripts/ops/hunt_import_libfuzzer_corpus.sh" 2>"$OUT/${tid}.log")"
  rc=$?
  set -e
  if [[ "$rc" -eq 0 && -n "$n" ]]; then
    log "  PASS $tid seeds=$n"
    ok=$((ok + 1))
  else
    log "  FAIL $tid rc=$rc (see $OUT/${tid}.log)"
    fail=$((fail + 1))
  fi
done

python3 - "$OUT" "$TARGETS" "$ok" "$fail" <<'PY'
import json, os, sys
out, targets, ok, fail = sys.argv[1:5]
rows = []
for tid in targets.split(","):
    tid = tid.strip()
    if not tid:
        continue
    seed_dir = os.path.join(os.environ.get("HACKME_REPO_ROOT", "."), ".cache", "hunt-lf-seeds", tid)
    n = len([f for f in os.listdir(seed_dir) if os.path.isfile(os.path.join(seed_dir, f))]) if os.path.isdir(seed_dir) else 0
    rows.append({"target": tid, "seed_files": n})
md = [
    "# Hunt L2 bootstrap — libFuzzer seeds",
    "",
    f"- ok: **{ok}** · fail: **{fail}**",
    "",
    "| Target | Seed files |",
    "|--------|------------|",
]
for r in rows:
    md.append(f"| {r['target']} | {r['seed_files']} |")
md += [
    "",
    "Seeds merge into Hunt pool corpus at campaign create (`hunt:{target_id}` namespace).",
    "Does **not** guarantee faster first-hit — better starting corpus for obscure targets.",
    "",
]
open(os.path.join(out, "REPORT.md"), "w").write("\n".join(md))
json.dump({"ok": int(ok), "fail": int(fail), "targets": rows}, open(os.path.join(out, "summary.json"), "w"), indent=2)
print("\n".join(md))
PY

log "DONE ok=$ok fail=$fail → $OUT/REPORT.md"
[[ "$fail" -eq 0 ]]
