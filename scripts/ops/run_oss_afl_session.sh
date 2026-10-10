#!/usr/bin/env bash
# AFL++ (preferred) or honggfuzz persistent session → research corpus + Hunt L2 seeds.
# Stdin ASAN drivers (Hunt) are fed via stdin (no @@). Missing tools → skip-with-reason (exit 0).
#
#   TARGET=mpack WALL_SEC=120 bash scripts/ops/run_oss_afl_session.sh
#   TARGETS=mpack,cwalk WALL_SEC=60 bash scripts/ops/run_oss_afl_session.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export HACKME_REPO_ROOT="$ROOT"
export HACKME_POOL_SEED_FROM_RESEARCH="${HACKME_POOL_SEED_FROM_RESEARCH:-0}"
export HACKME_OSS_HARNESS_VARIANT="${HACKME_OSS_HARNESS_VARIANT:-deep_v1}"

TARGETS="${TARGETS:-${TARGET:-mpack}}"
WALL_SEC="${WALL_SEC:-120}"

log() { echo "[oss-afl $(date -u +%H:%M:%S)] $*"; }

ENGINE=""
if command -v afl-fuzz >/dev/null 2>&1; then
  ENGINE=afl
elif command -v honggfuzz >/dev/null 2>&1; then
  ENGINE=honggfuzz
else
  REASON="afl-fuzz (AFL++) and honggfuzz not installed"
  log "SKIP: $REASON"
  mkdir -p "$ROOT/reports/oss-cve-afl"
  echo "$REASON" >"$ROOT/reports/oss-cve-afl/SKIP.txt"
  exit 0
fi
log "engine=$ENGINE"

import_crashes() {
  local TID="$1" SRC="$2"
  local SEED_CACHE="$ROOT/.cache/hunt-lf-seeds/$TID"
  local PERSIST="$ROOT/reports/oss-cve-libfuzzer/$TID/corpus"
  mkdir -p "$SEED_CACHE" "$PERSIST"
  local copied=0
  [[ -d "$SRC" ]] || { echo 0; return 0; }
  while IFS= read -r -d '' f; do
    base="$(basename "$f")"
    [[ "$base" == README.txt ]] && continue
    [[ -f "$f" ]] || continue
    # Skip empty / huge
    sz=$(stat -c%s "$f" 2>/dev/null || stat -f%z "$f" 2>/dev/null || echo 0)
    (( sz > 0 && sz < 1048576 )) || continue
    cp -n "$f" "$SEED_CACHE/afl-${base}" 2>/dev/null || cp "$f" "$SEED_CACHE/afl-${base}.$$" 2>/dev/null || true
    cp -n "$f" "$PERSIST/afl-${base}" 2>/dev/null || cp "$f" "$PERSIST/afl-${base}.$$" 2>/dev/null || true
    copied=$((copied + 1))
  done < <(find "$SRC" -type f -print0 2>/dev/null)
  echo "$copied"
}

IFS=',' read -r -a IDS <<< "$TARGETS"
for TID in "${IDS[@]}"; do
  TID="$(echo "$TID" | tr -d '[:space:]')"
  [[ -z "$TID" ]] && continue
  OUT="$ROOT/.cache/hunt-afl/$TID"
  mkdir -p "$OUT/in" "$OUT/out"
  if [[ -z "$(ls -A "$OUT/in" 2>/dev/null || true)" ]]; then
    # Seed from persistent LF corpus when present.
    if [[ -d "$ROOT/reports/oss-cve-libfuzzer/$TID/corpus" ]]; then
      find "$ROOT/reports/oss-cve-libfuzzer/$TID/corpus" -type f ! -name '.*' 2>/dev/null | head -32 | while read -r f; do
        cp -n "$f" "$OUT/in/" 2>/dev/null || true
      done
    fi
  fi
  if [[ -z "$(ls -A "$OUT/in" 2>/dev/null || true)" ]]; then
    printf '{"a":1}\n' >"$OUT/in/seed1.json"
    printf '[]\n' >"$OUT/in/seed2.json"
    printf '\x80' >"$OUT/in/seed3.bin"
  fi

  log "ensure ASAN stdin harness TARGET=$TID"
  TARGETS="$TID" bash "$ROOT/scripts/ops/build_oss_cve_pack.sh" >/dev/null || true
  HARNESS="$(ls -1t "$ROOT/.cache/oss-cve-bin/${TID}-"*.bin 2>/dev/null | grep -v msan | head -1 || true)"
  if [[ -z "$HARNESS" || ! -x "$HARNESS" ]]; then
    log "SKIP TARGET=$TID: no ASAN stdin harness in .cache/oss-cve-bin"
    continue
  fi

  log "run TARGET=$TID wall=${WALL_SEC}s harness=$HARNESS"
  if [[ "$ENGINE" == "afl" ]]; then
    export AFL_SKIP_CPUFREQ="${AFL_SKIP_CPUFREQ:-1}"
    export AFL_I_DONT_CARE_ABOUT_MISSING_CRASHES="${AFL_I_DONT_CARE_ABOUT_MISSING_CRASHES:-1}"
    export ASAN_OPTIONS="${ASAN_OPTIONS:-detect_leaks=0:abort_on_error=1:allocator_may_return_null=1}"
    # Stdin mode (no @@) — Hunt drivers read stdin.
    timeout --signal=INT "${WALL_SEC}s" afl-fuzz -i "$OUT/in" -o "$OUT/out" -V "$WALL_SEC" -- "$HARNESS" \
      >"$OUT/afl.log" 2>&1 || true
    for d in "$OUT/out/default/queue" "$OUT/out/queue" "$OUT/out/default/crashes" "$OUT/out/crashes"; do
      n=$(import_crashes "$TID" "$d")
      [[ "${n:-0}" != "0" ]] && log "imported $n from $d"
    done
  else
    # honggfuzz stdin fuzzing
    timeout --signal=INT "${WALL_SEC}s" honggfuzz -f "$OUT/in" -w "$OUT/out" -t 3 -- "$HARNESS" \
      >"$OUT/hfuzz.log" 2>&1 || true
    n=$(import_crashes "$TID" "$OUT/out")
    log "imported $n honggfuzz artifacts"
  fi
done

log "done engine=$ENGINE"
exit 0
