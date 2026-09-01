#!/usr/bin/env bash
# Overnight Hunt local soak + optional libFuzzer baseline comparison.
#
# Default: 8h total wall — Hunt Standard on cjson + libucl (sequential 4h each).
# Low RAM machines: sequential is safer than parallel ASAN.
#
#   bash scripts/ops/hunt_overnight_soak.sh
#   WALL_SEC=28800 TARGETS=cjson,libucl PARALLEL=1 bash scripts/ops/hunt_overnight_soak.sh
#   RUN_LIBFUZZER=1 LIBFUZZER_WALL_SEC=3600 bash scripts/ops/hunt_overnight_soak.sh
#
# Monitor: tail -f reports/hunt-overnight/<stamp>/run.log
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export HACKME_REPO_ROOT="$ROOT"

TARGETS="${TARGETS:-cjson,libucl}"
PKG="${PKG:-hunt_standard}"
# Total overnight budget (split across targets when SEQUENTIAL=1).
WALL_SEC="${WALL_SEC:-28800}"
HUNT_ITER="${HUNT_ITER:-200000}"
PARALLEL="${PARALLEL:-0}"
RUN_LIBFUZZER="${RUN_LIBFUZZER:-1}"
LIBFUZZER_WALL_SEC="${LIBFUZZER_WALL_SEC:-3600}"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="${OUT:-$ROOT/reports/hunt-overnight/$STAMP}"
PIDFILE="$OUT/soak.pid"
STATUS="$OUT/status.json"

log() { echo "[hunt-overnight $(date -u +%H:%M:%S)] $*" | tee -a "$OUT/run.log"; }

if ! command -v clang >/dev/null 2>&1; then
  echo "[hunt-overnight] need clang" >&2
  exit 1
fi

mkdir -p "$OUT" "$ROOT/.cache/hunt-overnight-bin"
echo $$ >"$PIDFILE"

IFS=',' read -r -a TARGET_ARR <<< "$TARGETS"
N_TARGETS="${#TARGET_ARR[@]}"
if [[ "$N_TARGETS" -lt 1 ]]; then
  echo "[hunt-overnight] TARGETS empty" >&2
  exit 1
fi

if [[ "$PARALLEL" == "1" ]]; then
  PER_TARGET_WALL="$WALL_SEC"
else
  PER_TARGET_WALL=$((WALL_SEC / N_TARGETS))
  if [[ "$PER_TARGET_WALL" -lt 300 ]]; then
    PER_TARGET_WALL=300
  fi
fi

mem_avail_kb="$(awk '/MemAvailable:/ {print $2}' /proc/meminfo 2>/dev/null || echo 0)"
if [[ "${mem_avail_kb:-0}" -lt 4000000 && "$PARALLEL" == "1" && "$N_TARGETS" -gt 1 ]]; then
  log "warn: MemAvailable < 4Gi — forcing sequential (was PARALLEL=1)"
  PARALLEL=0
  PER_TARGET_WALL=$((WALL_SEC / N_TARGETS))
fi

write_status() {
  local phase="$1"
  python3 - "$STATUS" "$phase" "$STAMP" "$TARGETS" "$PARALLEL" "$PER_TARGET_WALL" <<'PY'
import json, sys, time
path, phase, stamp, targets, parallel, per_wall = sys.argv[1:7]
doc = {
    "phase": phase,
    "stamp": stamp,
    "targets": targets.split(","),
    "parallel": parallel == "1",
    "per_target_wall_sec": int(per_wall),
    "total_wall_sec": int(per_wall) * (1 if parallel == "1" else len(targets.split(","))),
    "updated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
}
with open(path, "w") as f:
    json.dump(doc, f, indent=2)
PY
}

log "=== Hunt overnight soak stamp=$STAMP ==="
log "targets=$TARGETS package=$PKG hunt_iter=$HUNT_ITER wall_total=${WALL_SEC}s per_target=${PER_TARGET_WALL}s parallel=$PARALLEL libfuzzer=$RUN_LIBFUZZER"
write_status "prebuild"

log "prebuild Hunt ASAN drivers"
TARGETS="$TARGETS" bash "$ROOT/scripts/ops/build_oss_cve_pack.sh" >>"$OUT/build-hunt.log" 2>&1

build_libfuzzer_harness() {
  local tid="$1"
  local out="$ROOT/.cache/hunt-overnight-bin/${tid}-libfuzzer-asan"
  if [[ -x "$out" ]]; then
    echo "$out"
    return 0
  fi
  local clone="$ROOT/.cache/oss-cve-clones/$tid"
  [[ -d "$clone" ]] || { log "missing clone $clone"; return 1; }
  local harness="$ROOT/tasks/sources/fuzz/benchmark/${tid}_libfuzzer.c"
  [[ -f "$harness" ]] || { log "no libfuzzer harness for $tid — skip"; return 1; }
  local -a args=(
    -fsanitize=fuzzer,address,undefined
    -fno-omit-frame-pointer -g -O1
    -I"$clone"
    -o "$out"
    "$harness"
  )
  case "$tid" in
    cjson) args+=("$clone/cJSON.c") ;;
    libucl)
      for f in src/ucl_parser.c src/ucl_util.c src/ucl_hash.c src/ucl_schema.c \
               src/ucl_emitter.c src/ucl_emitter_utils.c src/ucl_msgpack.c src/ucl_sexp.c; do
        args+=("$clone/$f")
      done
      args+=(-I"$clone/include" -I"$clone/uthash" -I"$clone/klib" -I"$clone/src" -w)
      ;;
    *) return 1 ;;
  esac
  log "build libFuzzer $tid" >&2
  clang "${args[@]}" >>"$OUT/build-libfuzzer.log" 2>&1
  echo "$out"
}

run_hunt_target() {
  local tid="$1"
  local json="$OUT/hunt-${tid}.json"
  local crashes="$OUT/crashes-${tid}"
  local report="$OUT/hunt-report-${tid}.json"
  local bench="${HUNT_BENCH_BIN:-$ROOT/bin/hunt-bench-local}"
  if [[ ! -x "$bench" ]]; then
    log "build hunt-bench-local"
    go build -trimpath -o "$bench" ./scripts/tests/tools/hunt_bench_local.go
  fi
  log "Hunt local START $tid iter=$HUNT_ITER wall=${PER_TARGET_WALL}s"
  "$bench" \
    -target "$tid" \
    -package "$PKG" \
    -iter "$HUNT_ITER" \
    -wall "$PER_TARGET_WALL" \
    -out "$json" \
    -crashes-dir "$crashes" \
    -report "$report" >>"$OUT/hunt-${tid}.log" 2>&1
  python3 - "$json" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
print(f"  Hunt {d['target']}: verdict={d.get('verdict')} iter={d.get('iterations')} "
      f"crashes={d.get('crashes')} unique_sig={d.get('unique_signatures')} "
      f"exec/s={d.get('exec_per_sec',0):.1f} elapsed={d.get('elapsed_sec',0):.0f}s")
PY
  log "Hunt local DONE $tid"
}

run_libfuzzer_target() {
  local tid="$1"
  local bin
  bin="$(build_libfuzzer_harness "$tid")" || return 0
  local corpus="$OUT/libfuzzer-corpus-$tid"
  local crashes="$OUT/libfuzzer-crashes-$tid"
  mkdir -p "$corpus" "$crashes"
  local logf="$OUT/libfuzzer-${tid}.log"
  log "libFuzzer START $tid wall=${LIBFUZZER_WALL_SEC}s"
  export ASAN_OPTIONS="${ASAN_OPTIONS:-detect_leaks=1:halt_on_error=1:allocator_may_return_null=1}"
  export UBSAN_OPTIONS="${UBSAN_OPTIONS:-halt_on_error=1}"
  set +e
  "$bin" "$corpus" \
    -max_total_time="$LIBFUZZER_WALL_SEC" \
    -timeout=3 \
    -rss_limit_mb=2048 \
    -max_len=65536 \
    -artifact_prefix="$crashes/crash-" \
    -print_final_stats=1 \
    2>&1 | tee "$logf"
  local rc=$?
  set -e
  python3 - "$logf" "$crashes" "$rc" "$tid" "$OUT/libfuzzer-${tid}.json" <<'PY'
import glob, json, re, sys
logf, crashdir, rc, tid, outj = sys.argv[1:6]
text = open(logf, errors="replace").read()
execs = 0
m = re.search(r"stat::number_of_executed_units:\s*(\d+)", text)
if m:
    execs = int(m.group(1))
elif re.search(r"Done (\d+) runs", text):
    execs = int(re.search(r"Done (\d+) runs", text).group(1))
eps = 0.0
m2 = re.search(r"stat::average_exec_per_sec:\s*([\d.]+)", text)
if m2:
    eps = float(m2.group(1))
crashes = len([p for p in glob.glob(crashdir + "/crash-*") if not p.endswith(".metadata")])
doc = {
    "engine": "libfuzzer",
    "target": tid,
    "executions": execs,
    "exec_per_sec": eps,
    "crash_artifacts": crashes,
    "exit_code": rc,
}
json.dump(doc, open(outj, "w"), indent=2)
print(f"  libFuzzer {tid}: execs={execs} exec/s={eps:.1f} artifacts={crashes} rc={rc}")
PY
  log "libFuzzer DONE $tid"
}

write_status "hunt"

if [[ "$PARALLEL" == "1" ]]; then
  pids=()
  for tid in "${TARGET_ARR[@]}"; do
    run_hunt_target "$tid" &
    pids+=($!)
  done
  for pid in "${pids[@]}"; do
    wait "$pid"
  done
else
  for tid in "${TARGET_ARR[@]}"; do
    run_hunt_target "$tid"
  done
fi

if [[ "$RUN_LIBFUZZER" == "1" ]]; then
  write_status "libfuzzer"
  for tid in "${TARGET_ARR[@]}"; do
    run_libfuzzer_target "$tid" || true
  done
fi

write_status "summary"

python3 - "$OUT" "$PKG" "$PER_TARGET_WALL" "$LIBFUZZER_WALL_SEC" <<'PY'
import glob, json, os, sys
out, pkg, hunt_wall, lf_wall = sys.argv[1:5]
lines = [
    "# Hunt overnight soak",
    "",
    f"- output: `{out}`",
    f"- package: **{pkg}**",
    f"- hunt wall per target: **{hunt_wall}s**",
    f"- libfuzzer wall per target: **{lf_wall}s** (if enabled)",
    "",
    "## Results",
    "",
    "| Target | Engine | Iterations | exec/s | Crashes | Unique sigs | Verdict |",
    "|--------|--------|------------|--------|---------|-------------|---------|",
]
for path in sorted(glob.glob(os.path.join(out, "hunt-*.json"))):
    base = os.path.basename(path)
    if base.startswith("hunt-report-"):
        continue
    d = json.load(open(path))
    tid = d.get("target", "?")
    eps = d.get("exec_per_sec") or 0
    lines.append(
        f"| {tid} | Hunt | {d.get('iterations','?')} | {eps:.1f} "
        f"| {d.get('crashes','?')} | {d.get('unique_signatures','?')} | {d.get('verdict','?')} |"
    )
for path in sorted(glob.glob(os.path.join(out, "libfuzzer-*.json"))):
    d = json.load(open(path))
    tid = d.get("target", "?")
    eps = d.get("exec_per_sec") or 0
    lines.append(
        f"| {tid} | libFuzzer | {d.get('executions','?')} | {eps:.1f} "
        f"| {d.get('crash_artifacts','?')} | — | — |"
    )

lines += [
    "",
    "## Interpretation",
    "",
    "- **unique_signatures** ≈ sanitizer class/subtype buckets (not CVE count).",
    "- libucl: expect 1 dominant UBSan root cause unless new ASAN class appears.",
    "- cjson: CLEAN on both engines is a normal outcome on mature OSS.",
    "- Hunt exec/s < libFuzzer is expected (no in-process coverage feedback).",
    "",
    "## Artifacts",
    "",
    "- `hunt-*.json` — summary metrics",
    "- `hunt-report-*.json` — full crash payloads",
    "- `crashes-*/` — repro `.bin` files + `index.json`",
    "- `libfuzzer-crashes-*/` — libFuzzer artifacts (if enabled)",
    "",
]
md = "\n".join(lines)
open(os.path.join(out, "REPORT.md"), "w").write(md)
print(md)
PY

write_status "done"
log "DONE → $OUT/REPORT.md"
