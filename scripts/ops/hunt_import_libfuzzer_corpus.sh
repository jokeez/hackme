#!/usr/bin/env bash
# Import libFuzzer corpus files into Hunt L2 seed cache (.cache/hunt-lf-seeds/{target}).
#
#   TARGET=cjson WALL_SEC=120 bash scripts/ops/hunt_import_libfuzzer_corpus.sh
#   TARGET=libucl bash scripts/ops/hunt_import_libfuzzer_corpus.sh   # import existing corpus only
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export HACKME_REPO_ROOT="$ROOT"

TARGET="${TARGET:-cjson}"
WALL_SEC="${WALL_SEC:-120}"
OUT_DIR="${OUT_DIR:-$ROOT/.cache/hunt-lf-seeds/$TARGET}"
CORPUS="$ROOT/.cache/hunt-lf-import/$TARGET-corpus"
BIN="$ROOT/.cache/hunt-lf-import/${TARGET}-libfuzzer-asan"

log() { echo "[hunt-lf-import $(date -u +%H:%M:%S)] $*" >&2; }

if ! command -v clang >/dev/null 2>&1; then
  echo "[hunt-lf-import] need clang" >&2
  exit 1
fi

mkdir -p "$OUT_DIR" "$(dirname "$CORPUS")" "$(dirname "$BIN")"
TARGETS="$TARGET" bash "$ROOT/scripts/ops/build_oss_cve_pack.sh" >/dev/null

build_libfuzzer() {
  if [[ -x "$BIN" ]]; then
    return 0
  fi
  local clone="$ROOT/.cache/oss-cve-clones/$TARGET"
  local harness="$ROOT/tasks/sources/fuzz/benchmark/${TARGET}_libfuzzer.c"
  [[ -f "$harness" ]] || { log "no harness for $TARGET"; return 1; }
  local -a args=(
    -fsanitize=fuzzer,address,undefined
    -fno-omit-frame-pointer -g -O1
    -I"$clone"
    -o "$BIN"
    "$harness"
  )
  case "$TARGET" in
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
  log "build libFuzzer $TARGET"
  clang "${args[@]}"
}

if [[ "${IMPORT_ONLY:-0}" != "1" ]]; then
  build_libfuzzer
  rm -rf "$CORPUS"
  mkdir -p "$CORPUS"
  export ASAN_OPTIONS="${ASAN_OPTIONS:-detect_leaks=1:halt_on_error=1:allocator_may_return_null=1}"
  export UBSAN_OPTIONS="${UBSAN_OPTIONS:-halt_on_error=1}"
  log "run libFuzzer $TARGET wall=${WALL_SEC}s"
  set +e
  "$BIN" "$CORPUS" \
    -max_total_time="$WALL_SEC" \
    -timeout=3 \
    -rss_limit_mb=2048 \
    -max_len=65536 \
    -print_final_stats=1
  lf_rc=$?
  set -e
  shopt -s nullglob
  corpus_n=("$CORPUS"/*)
  if [[ "$lf_rc" -ne 0 ]]; then
    if [[ "${#corpus_n[@]}" -eq 0 ]]; then
      log "libFuzzer exit $lf_rc and no corpus files in $CORPUS"
      exit "$lf_rc"
    fi
    log "libFuzzer exit $lf_rc (sanitizer stop ok) — corpus files=${#corpus_n[@]}"
  fi
fi

shopt -s nullglob
files=("$CORPUS"/*)
if [[ "${#files[@]}" -eq 0 ]]; then
  log "no corpus files in $CORPUS"
  exit 1
fi

rm -f "$OUT_DIR"/*
count=0
for f in "${files[@]}"; do
  [[ -f "$f" ]] || continue
  base="$(basename "$f")"
  [[ "$base" == crash-* ]] && continue
  cp -f "$f" "$OUT_DIR/seed-$(printf '%04d' "$count")-$base"
  count=$((count + 1))
done
log "imported $count seeds → $OUT_DIR"
echo "$count"
