#!/usr/bin/env bash
# Import libFuzzer corpus files into Hunt L2 seed cache (.cache/hunt-lf-seeds/{target}).
#
# Works for any catalog target in upstream/oss_cve_targets.json:
#   - dedicated harness (cjson, libucl, nghttp2_fuzzer, …)
#   - generic stdin subprocess harness for obscure targets (spl, parsello, jsmn, …)
#
#   TARGET=cjson WALL_SEC=120 bash scripts/ops/hunt_import_libfuzzer_corpus.sh
#   TARGET=spl WALL_SEC=60 bash scripts/ops/hunt_import_libfuzzer_corpus.sh
#   TARGET=libucl IMPORT_ONLY=1 bash scripts/ops/hunt_import_libfuzzer_corpus.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export HACKME_REPO_ROOT="$ROOT"

TARGET="${TARGET:-cjson}"
WALL_SEC="${WALL_SEC:-120}"

log() { echo "[hunt-lf-import $(date -u +%H:%M:%S)] $*" >&2; }

if [[ "${IMPORT_ONLY:-0}" != "1" ]] && ! command -v clang >/dev/null 2>&1; then
  echo "[hunt-lf-import] need clang (or IMPORT_ONLY=1)" >&2
  exit 1
fi

log "ensure upstream clone TARGET=$TARGET"
TARGETS="$TARGET" bash "$ROOT/scripts/ops/build_oss_cve_pack.sh" >/dev/null

args=(-target "$TARGET" -repo "$ROOT" -wall "$WALL_SEC")
if [[ "${IMPORT_ONLY:-0}" == "1" ]]; then
  args+=(-import-only)
fi

count="$(go run ./cmd/hunt-lf-import "${args[@]}")"
log "imported $count seeds → .cache/hunt-lf-seeds/$TARGET"
echo "$count"
