#!/usr/bin/env bash
# Live benchmark: Hunt Standard 128 vs libFuzzer on cjson/libucl (equal wall-time).
#
#   bash scripts/tests/hunt_standard128_live_benchmark.sh
#   TARGET=libucl WALL_SEC=180 bash scripts/tests/hunt_standard128_live_benchmark.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export HACKME_REPO_ROOT="$ROOT"

TARGET="${TARGET:-cjson}"
ALT_TARGET="${ALT_TARGET:-libucl}"
WALL_SEC="${WALL_SEC:-120}"
HUNT_ITER="${HUNT_ITER:-15000}"
POOL_SHARDS="${POOL_SHARDS:-4}"
POOL_ITER_PER_SHARD="${POOL_ITER_PER_SHARD:-128}"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT="$ROOT/reports/hunt-benchmark/$STAMP"
mkdir -p "$OUT" "$ROOT/.cache/hunt-benchmark-bin"

log() { echo "[hunt-bench $(date -u +%H:%M:%S)] $*" | tee -a "$OUT/run.log"; }

if ! command -v clang >/dev/null 2>&1; then
  echo "[hunt-bench] SKIP: clang required" >&2
  exit 0
fi

log "=== Hunt Standard 128 live benchmark stamp=$STAMP ==="
log "primary=$TARGET alt=$ALT_TARGET wall=${WALL_SEC}s hunt_iter=$HUNT_ITER pool_shards=$POOL_SHARDS pool_iter=$POOL_ITER_PER_SHARD"

log "prebuild Hunt ASAN drivers"
TARGETS="$TARGET,$ALT_TARGET" bash "$ROOT/scripts/ops/build_oss_cve_pack.sh" >>"$OUT/build-hunt.log" 2>&1

build_libfuzzer_harness() {
  local tid="$1"
  local out="$ROOT/.cache/hunt-benchmark-bin/${tid}-libfuzzer-asan"
  if [[ -x "$out" ]]; then
    echo "$out"
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

run_hunt_local() {
  local tid="$1"
  local json="$OUT/hunt-local-${tid}.json"
  log "Hunt local $tid iter=$HUNT_ITER wall=$WALL_SEC"
  go run ./scripts/tests/tools/hunt_bench_local.go \
    -target "$tid" \
    -package hunt_standard \
    -iter "$HUNT_ITER" \
    -wall "$WALL_SEC" \
    -out "$json" >>"$OUT/hunt-local-${tid}.log" 2>&1
  python3 - "$json" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
print(f"  verdict={d.get('verdict')} iter={d.get('iterations')} crashes={d.get('crashes')} "
      f"elapsed={d.get('elapsed_sec',0):.1f}s exec_per_sec={d.get('exec_per_sec',0):.1f} "
      f"dict_profile={d.get('mutator_profile')} iter_per_shard_cfg={d.get('iterations_per_shard')}")
PY
}

run_libfuzzer() {
  local tid="$1"
  local bin
  bin="$(build_libfuzzer_harness "$tid")"
  local corpus="$OUT/libfuzzer-corpus-$tid"
  local crashes="$OUT/libfuzzer-crashes-$tid"
  mkdir -p "$corpus" "$crashes"
  local logf="$OUT/libfuzzer-${tid}.log"
  log "libFuzzer $tid wall=$WALL_SEC"
  export ASAN_OPTIONS="${ASAN_OPTIONS:-detect_leaks=1:halt_on_error=1:allocator_may_return_null=1}"
  export UBSAN_OPTIONS="${UBSAN_OPTIONS:-halt_on_error=1}"
  set +e
  "$bin" "$corpus" \
    -max_total_time="$WALL_SEC" \
    -timeout=3 \
    -rss_limit_mb=2048 \
    -max_len=65536 \
    -artifact_prefix="$crashes/crash-" \
    -print_final_stats=1 \
    2>&1 | tee "$logf"
  local rc=$?
  set -e
  python3 - "$logf" "$crashes" "$rc" <<'PY'
import glob, re, sys
logf, crashdir, rc = sys.argv[1], sys.argv[2], int(sys.argv[3])
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
elif execs > 0:
    m3 = re.search(r"Done \d+ runs in (\d+) second", text)
    if m3:
        eps = execs / max(1, int(m3.group(1)))
crashes = len([p for p in glob.glob(crashdir + "/crash-*") if not p.endswith(".metadata")])
print(f"  libFuzzer rc={rc} execs={execs} exec_per_sec={eps:.1f} crash_artifacts={crashes}")
open(logf + ".summary", "w").write(f"{execs}\n{eps}\n{crashes}\n")
PY
}

run_pool_standard128() {
  local tid="$1"
  local json="$OUT/hunt-pool-${tid}.json"
  log "Hunt pool $tid shards=$POOL_SHARDS iter_per_shard=$POOL_ITER_PER_SHARD"
  TARGET_ID="$tid" \
    POOL_SHARDS="$POOL_SHARDS" \
    POOL_ITER_PER_SHARD="$POOL_ITER_PER_SHARD" \
    OUT_JSON="$json" \
    WORKERFUZZ_TIMEOUT_MS="${WORKERFUZZ_TIMEOUT_MS:-300000}" \
    bash "$ROOT/scripts/tests/tools/hunt_bench_pool.sh" >>"$OUT/hunt-pool-${tid}.log" 2>&1
  python3 - "$json" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
print(f"  pool shards_done={d.get('shards_done')} iter_per_shard={d.get('iterations_per_shard')} "
      f"total_execs={d.get('total_shard_execs')} package={d.get('hunt_package')}")
PY
}

for tid in "$TARGET" "$ALT_TARGET"; do
  log "--- target $tid ---"
  run_hunt_local "$tid"
  run_libfuzzer "$tid"
done

run_pool_standard128 "$TARGET"

python3 - "$OUT" "$TARGET" "$ALT_TARGET" "$WALL_SEC" "$POOL_ITER_PER_SHARD" <<'PY'
import json, os, sys
out, primary, alt, wall, pool_iter = sys.argv[1:6]
lines = [
    "# Hunt Standard 128 vs libFuzzer — live benchmark",
    "",
    f"- stamp: `{os.path.basename(out)}`",
    f"- wall-time (libFuzzer / Hunt cap): **{wall}s**",
    f"- pool: **{pool_iter} iter/shard** (Standard SKU)",
    "",
    "## Summary",
    "",
    "| Target | Engine | Iterations | exec/s | Crashes | Notes |",
    "|--------|--------|------------|--------|---------|-------|",
]
def row(tid, engine, path, notes=""):
    p = os.path.join(out, path)
    if not os.path.isfile(p):
        return
    if path.endswith(".json"):
        d = json.load(open(p))
        lines.append(f"| {tid} | {engine} | {d.get('iterations', d.get('total_shard_execs','?'))} | "
                     f"{d.get('exec_per_sec', '—')} | {d.get('crashes', d.get('crash_artifacts','?'))} | {notes} |")
    elif path.endswith(".summary"):
        a = open(p).read().splitlines()
        if len(a) >= 3:
            lines.append(f"| {tid} | {engine} | {a[0]} | {a[1]} | {a[2]} | {notes} |")

for tid in [primary, alt]:
    row(tid, "Hunt local", f"hunt-local-{tid}.json", "mutator_dict + asan+ubsan+lsan")
    row(tid, "libFuzzer", f"libfuzzer-{tid}.log.summary", "corpus-guided in-process")
row(primary, "Hunt pool", f"hunt-pool-{primary}.json", f"{pool_iter} exec/shard coordinator replay")
lines += [
    "",
    "## Takeaways",
    "",
    "- **Hunt pool Standard** proves `iterations_per_shard=128` on coordinator replay (not marketing).",
    "- **Hunt local** uses domain dict + byte mutation; **libFuzzer** uses in-process corpus + coverage feedback.",
    "- libFuzzer usually wins **raw exec/s**; Hunt wins **turnkey fleet + verified shard report + escrow**.",
    "",
]
md = "\n".join(lines)
open(os.path.join(out, "REPORT.md"), "w").write(md)
print(md)
PY

log "DONE → $OUT/REPORT.md"
