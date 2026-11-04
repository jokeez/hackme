#!/usr/bin/env bash
# Live equal-wall benchmark: Hunt coverage engines (libFuzzer + AFL++) vs standalone libFuzzer.
# Replaces the old “113 vs 700” framing — that was byte-Hunt vs LF, not current product.
#
#   bash scripts/tests/hunt_coverage_engine_benchmark.sh
#   WALL_SEC=90 TARGET=cjson bash scripts/tests/hunt_coverage_engine_benchmark.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export HACKME_REPO_ROOT="$ROOT"
# Prefer desktop Hunt engines PATH when present.
if [[ -f "$ROOT/logs/desktop/toolchains/.env.hunt-engines" ]]; then
  _hackme_prev_path="$PATH"
  set -a
  # shellcheck disable=SC1090
  . "$ROOT/logs/desktop/toolchains/.env.hunt-engines"
  set +a
  PATH="$PATH:$_hackme_prev_path"; export PATH
fi

TARGET="${TARGET:-cjson}"
ALT_TARGET="${ALT_TARGET:-libucl}"
WALL_SEC="${WALL_SEC:-90}"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT="$ROOT/reports/hunt-benchmark/coverage-$STAMP"
mkdir -p "$OUT" "$ROOT/.cache/hunt-benchmark-bin"

log() { echo "[cov-bench $(date -u +%H:%M:%S)] $*" | tee -a "$OUT/run.log"; }

if ! command -v clang >/dev/null 2>&1; then
  echo "[cov-bench] SKIP: clang required" >&2
  exit 0
fi

log "=== Coverage Hunt vs standalone libFuzzer stamp=$STAMP wall=${WALL_SEC}s ==="
log "targets=$TARGET,$ALT_TARGET"

# Ensure catalog ASAN drivers exist for Hunt engines.
TARGETS="$TARGET,$ALT_TARGET" bash "$ROOT/scripts/ops/build_oss_cve_pack.sh" >>"$OUT/build-hunt.log" 2>&1 || \
  log "WARN: oss pack build partial — see $OUT/build-hunt.log"

build_libfuzzer_harness() {
  local tid="$1"
  local outb="$ROOT/.cache/hunt-benchmark-bin/${tid}-libfuzzer-asan"
  if [[ -x "$outb" ]]; then
    echo "$outb"
    return 0
  fi
  local clone="$ROOT/.cache/oss-cve-clones/$tid"
  [[ -d "$clone" ]] || { log "missing clone $clone"; return 1; }
  local harness="$ROOT/tasks/sources/fuzz/benchmark/${tid}_libfuzzer.c"
  [[ -f "$harness" ]] || { log "missing harness $harness"; return 1; }
  local -a args=(
    -fsanitize=fuzzer,address,undefined
    -fno-omit-frame-pointer -g -O1
    -I"$clone"
    -o "$outb"
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
  clang "${args[@]}" >>"$OUT/build-libfuzzer.log" 2>&1
  echo "$outb"
}

run_standalone_lf() {
  local tid="$1"
  local bin
  bin="$(build_libfuzzer_harness "$tid")" || return 0
  local corpus="$OUT/standalone-corpus-$tid"
  local crashes="$OUT/standalone-crashes-$tid"
  mkdir -p "$corpus" "$crashes"
  local logf="$OUT/standalone-lf-${tid}.log"
  log "standalone libFuzzer $tid wall=$WALL_SEC"
  export ASAN_OPTIONS="detect_leaks=0:halt_on_error=0:allocator_may_return_null=1"
  export UBSAN_OPTIONS="halt_on_error=0"
  set +e
  "$bin" "$corpus" \
    -max_total_time="$WALL_SEC" \
    -timeout=3 \
    -rss_limit_mb=2048 \
    -max_len=65536 \
    -artifact_prefix="$crashes/crash-" \
    -print_final_stats=1 \
    >"$logf" 2>&1
  local rc=$?
  set -e
  python3 - "$logf" "$crashes" "$rc" "$OUT/standalone-lf-${tid}.json" "$tid" "$WALL_SEC" <<'PY'
import glob, json, re, sys
logf, crashdir, rc, outj, tid, wall = sys.argv[1:7]
text = open(logf, errors="replace").read()
execs = 0
m = re.search(r"stat::number_of_executed_units:\s*(\d+)", text)
if m: execs = int(m.group(1))
eps = 0.0
m2 = re.search(r"stat::average_exec_per_sec:\s*([\d.]+)", text)
if m2: eps = float(m2.group(1))
elif execs and int(wall) > 0: eps = execs / float(wall)
crashes = len([p for p in glob.glob(crashdir + "/crash-*") if not p.endswith(".metadata")])
cov = 0
m3 = re.search(r"stat::new_features:\s*(\d+)", text)
if m3: cov = int(m3.group(1))
d = {
  "lane": "standalone_libfuzzer",
  "target": tid,
  "wall_sec": int(wall),
  "executions": execs,
  "exec_per_sec": round(eps, 1),
  "crash_artifacts": crashes,
  "new_features": cov,
  "rc": int(rc),
}
json.dump(d, open(outj, "w"), indent=2)
print(f"  standalone LF {tid}: execs={execs} exec/s={eps:.1f} crashes={crashes} features+={cov}")
PY
}

run_hunt_engine() {
  local tid="$1" eng="$2"
  local json="$OUT/hunt-${eng}-${tid}.json"
  local logf="$OUT/hunt-${eng}-${tid}.log"
  log "Hunt engine=$eng target=$tid wall=$WALL_SEC"
  set +e
  go run ./scripts/tests/tools/hunt_coverage_bench.go \
    -target "$tid" -engine "$eng" -wall "$WALL_SEC" -out "$json" \
    >"$logf" 2>&1
  local rc=$?
  set -e
  python3 - "$json" "$eng" "$tid" "$rc" <<'PY'
import json, sys
path, eng, tid, rc = sys.argv[1:5]
try:
    d = json.load(open(path))
except Exception as e:
    print(f"  Hunt {eng} {tid}: FAIL parse ({e}) rc={rc}")
    raise SystemExit(0)
print(f"  Hunt {eng} {tid}: ran={d.get('ran')} corpus {d.get('corpus_before')}→{d.get('corpus_after')} "
      f"(+{d.get('new_corpus')}) crashes={d.get('crash_files')} elapsed={d.get('elapsed_sec',0):.1f}s err={d.get('err')!r}")
PY
}

# Optional short byte-path reference (historical framing).
run_byte_ref() {
  local tid="$1"
  local json="$OUT/byte-hunt-${tid}.json"
  log "byte-Hunt reference $tid wall=$WALL_SEC (historical lane)"
  set +e
  go run ./scripts/tests/tools/hunt_bench_local.go \
    -target "$tid" -package hunt_standard -iter 200000 -wall "$WALL_SEC" -out "$json" \
    >"$OUT/byte-hunt-${tid}.log" 2>&1
  set -e
  python3 - "$json" "$tid" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
print(f"  byte-Hunt {sys.argv[2]}: iter={d.get('iterations')} exec/s={d.get('exec_per_sec',0):.1f} "
      f"verdict={d.get('verdict')} crashes={d.get('crashes')}")
PY
}

for tid in "$TARGET" "$ALT_TARGET"; do
  log "--- $tid ---"
  run_standalone_lf "$tid"
  run_hunt_engine "$tid" "libfuzzer"
  if command -v afl-fuzz >/dev/null 2>&1 || [[ -x "${HACKME_AFL_FUZZ:-}" ]]; then
    run_hunt_engine "$tid" "aflpp"
  else
    log "SKIP aflpp — afl-fuzz not on PATH"
  fi
  run_byte_ref "$tid"
done

python3 - "$OUT" "$TARGET" "$ALT_TARGET" "$WALL_SEC" "$STAMP" <<'PY'
import json, os, sys
out, primary, alt, wall, stamp = sys.argv[1:6]
lines = [
    f"# Coverage Hunt vs libFuzzer — live benchmark `{stamp}`",
    "",
    f"- wall: **{wall}s** equal per lane",
    f"- product Hunt = **libFuzzer / AFL++ engine leases** (not byte-shard exec/s)",
    f"- artifacts: `reports/hunt-benchmark/coverage-{stamp}/`",
    "",
    "## Results",
    "",
    "| Target | Lane | Metric | Value | Notes |",
    "|--------|------|--------|-------|-------|",
]

def load(name):
    p = os.path.join(out, name)
    if os.path.isfile(p):
        return json.load(open(p))
    return None

for tid in (primary, alt):
    d = load(f"standalone-lf-{tid}.json")
    if d:
        lines.append(f"| {tid} | standalone libFuzzer | exec/s | **{d.get('exec_per_sec')}** | {d.get('executions')} execs, crashes={d.get('crash_artifacts')} |")
    d = load(f"hunt-libfuzzer-{tid}.json")
    if d:
        lines.append(f"| {tid} | Hunt libFuzzer engine | corpus Δ | **+{d.get('new_corpus',0)}** | {d.get('corpus_before')}→{d.get('corpus_after')}, crashes={d.get('crash_files')}, {d.get('elapsed_sec',0):.0f}s |")
    d = load(f"hunt-aflpp-{tid}.json")
    if d:
        lines.append(f"| {tid} | Hunt AFL++ engine | corpus Δ | **+{d.get('new_corpus',0)}** | {d.get('corpus_before')}→{d.get('corpus_after')}, crashes={d.get('crash_files')}, {d.get('elapsed_sec',0):.0f}s |")
    d = load(f"byte-hunt-{tid}.json")
    if d:
        lines.append(f"| {tid} | byte-Hunt (legacy) | exec/s | **{round(float(d.get('exec_per_sec') or 0),1)}** | {d.get('iterations')} iters — historical lane only |")

lines += [
    "",
    "## How to read this",
    "",
    "- **Standalone libFuzzer** wins raw local exec/s — expected (in-process, no pool prove).",
    "- **Hunt libFuzzer / AFL++** are the *same class of engines*, wrapped as paid pool leases with ASAN + afl-showmap prove.",
    "- **byte-Hunt exec/s** (old ~100–150 vs LF ~700) is **not** the product default anymore — kept as honesty reference.",
    "- Product win: **distributed coverage-guided compute + coordinator-proven edges/crashes + escrow report**.",
    "",
]
md = "\n".join(lines)
open(os.path.join(out, "REPORT.md"), "w").write(md)
print(md)
PY

log "DONE → $OUT/REPORT.md"
echo "$OUT"
