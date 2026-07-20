#!/usr/bin/env bash
# Build libFuzzer ASAN binary for an OSS libFuzzer target.
#
#   TARGET=nghttp2 bash scripts/ops/build_oss_libfuzzer.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

TARGET="${TARGET:-nghttp2}"
OUT_DIR="$ROOT/.cache/oss-libfuzzer-bin"
OUT="$OUT_DIR/${TARGET}-asan-fuzzer"
mkdir -p "$OUT_DIR" "$ROOT/logs"

log() { echo "[libfuzzer-build $(date -u +%H:%M:%S)] $*" >&2; }

if [[ "$TARGET" == "libheif" ]]; then
  FUZZER="${LIBHEIF_FUZZER:-file_fuzzer}" \
    bash "$ROOT/scripts/ops/build_oss_libfuzzer_libheif.sh"
  exit 0
fi

# Reuse cached ASAN binary without requiring clang on PATH (systemd/cron autopublish).
if [[ "${SKIP_REBUILD:-0}" == "1" && -x "$OUT" ]]; then
  log "reuse existing $OUT ($(du -h "$OUT" | awk '{print $1}'))"
  echo "$OUT"
  exit 0
fi

command -v clang >/dev/null || { echo "[libfuzzer-build] need clang" >&2; exit 1; }
command -v python3 >/dev/null || { echo "[libfuzzer-build] need python3" >&2; exit 1; }

# Fuzzed binaries need libFuzzer runtime from clang.
if ! clang -fsanitize=fuzzer -x c - -o /dev/null 2>/dev/null <<'EOF'
#include <stddef.h>
#include <stdint.h>
int LLVMFuzzerTestOneInput(const uint8_t *d, size_t n) { (void)d; (void)n; return 0; }
EOF
then
  echo "[libfuzzer-build] clang lacks -fsanitize=fuzzer (install clang with compiler-rt)" >&2
  exit 1
fi

read -r FUZZER OSS_ID <<< "$(python3 - "$ROOT" "$TARGET" <<'PY'
import json, sys
root, tid = sys.argv[1], sys.argv[2]
m = json.load(open(f"{root}/upstream/oss_libfuzzer_targets.json"))
t = next(x for x in m["targets"] if x["id"] == tid)
print(t["fuzzer"], t.get("oss_cve_target_id", tid))
PY
)"

log "ensure upstream clone via oss-cve pack ($OSS_ID)"
TARGETS="$OSS_ID" bash "$ROOT/scripts/ops/build_oss_cve_pack.sh" >>"$ROOT/logs/libfuzzer-build.log" 2>&1

CLONE="$ROOT/.cache/oss-cve-clones/$OSS_ID"
CFG="$ROOT/tasks/sources/fuzz/oss/nghttp2-config"
FUZZER_SRC="$ROOT/tasks/sources/fuzz/oss/${FUZZER}.c"

[[ -f "$FUZZER_SRC" ]] || { echo "missing harness $FUZZER_SRC" >&2; exit 1; }
[[ -d "$CLONE" ]] || { echo "missing clone $CLONE" >&2; exit 1; }

# nghttp2 autotools stubs (same as fuzzupstream)
cp "$CFG/config.h" "$CLONE/config.h"
mkdir -p "$CLONE/lib/includes/nghttp2"
cp "$CFG/nghttp2/nghttp2ver.h" "$CLONE/lib/includes/nghttp2/nghttp2ver.h"

mapfile -t SRCS < <(python3 - "$ROOT" "$OSS_ID" <<'PY'
import json, pathlib, sys
root, tid = sys.argv[1], sys.argv[2]
m = json.load(open(f"{root}/upstream/oss_cve_targets.json"))
t = next(x for x in m["targets"] if x["id"] == tid)
clone = pathlib.Path(f"{root}/.cache/oss-cve-clones/{tid}")
for pat in t["upstream_src"]:
    for p in sorted(clone.glob(pat) if "*" in pat else [clone / pat]):
        if p.is_file():
            print(p)
PY
)

if [[ ${#SRCS[@]} -lt 5 ]]; then
  echo "[libfuzzer-build] too few sources (${#SRCS[@]})" >&2
  exit 1
fi

log "compile ${#SRCS[@]} upstream objects + $FUZZER → $OUT"
clang -fsanitize=address,fuzzer \
  -fno-omit-frame-pointer -g -O1 -w \
  -D_GNU_SOURCE -DHAVE_CONFIG_H -DNGHTTP2_STATICLIB \
  -I "$CLONE/lib/includes" -I "$CLONE/lib" -I "$CFG" \
  "$FUZZER_SRC" "${SRCS[@]}" \
  -o "$OUT"

log "PASS $OUT ($(du -h "$OUT" | awk '{print $1}'))"
echo "$OUT"
