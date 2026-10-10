#!/usr/bin/env bash
# Import libFuzzer corpus files into Hunt L2 seed cache (.cache/hunt-lf-seeds/{target}).
#
# Always runs coverage-preserving dedupe (-merge=1) before import when a durable
# corpus exists — prevents overnight bloat from parallel sessions.
#
#   TARGET=cjson WALL_SEC=120 bash scripts/ops/hunt_import_libfuzzer_corpus.sh
#   TARGET=mpack IMPORT_ONLY=1 bash scripts/ops/hunt_import_libfuzzer_corpus.sh
#   TARGET=mpack MERGE_ONLY=1 bash scripts/ops/hunt_import_libfuzzer_corpus.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export HACKME_REPO_ROOT="$ROOT"
export HACKME_POOL_SEED_FROM_RESEARCH="${HACKME_POOL_SEED_FROM_RESEARCH:-0}"

TARGET="${TARGET:-cjson}"
WALL_SEC="${WALL_SEC:-120}"

log() { echo "[hunt-lf-import $(date -u +%H:%M:%S)] $*" >&2; }

if [[ ! "$TARGET" =~ ^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$ ]]; then
  echo "[hunt-lf-import] invalid TARGET=$TARGET" >&2
  exit 2
fi

if [[ "${IMPORT_ONLY:-0}" != "1" && "${MERGE_ONLY:-0}" != "1" ]] && ! command -v clang >/dev/null 2>&1; then
  echo "[hunt-lf-import] need clang (or IMPORT_ONLY=1 / MERGE_ONLY=1)" >&2
  exit 1
fi

log "ensure upstream clone TARGET=$TARGET"
TARGETS="$TARGET" bash "$ROOT/scripts/ops/build_oss_cve_pack.sh" >/dev/null

# Prefer dedicated in-process harnesses (mpack/tinycbor/cwalk/…). Stdin subprocess
# fork+exec is ~0–tens exec/s and is last-resort only.
if [[ "${MERGE_ONLY:-0}" == "1" ]]; then
  log "merge-minimize durable corpus (libFuzzer -merge=1)"
  count="$(go run ./cmd/hunt-lf-import -target "$TARGET" -repo "$ROOT" -merge-only)"
  log "merged+imported $count seeds → .cache/hunt-lf-seeds/$TARGET"
  echo "$count"
  exit 0
fi

args=(-target "$TARGET" -repo "$ROOT" -wall "$WALL_SEC" -persist)
if [[ "${IMPORT_ONLY:-0}" == "1" ]]; then
  # Still merge durable corpus before import when present.
  if [[ -d "$ROOT/reports/oss-cve-libfuzzer/$TARGET/corpus" ]]; then
    log "pre-import merge-minimize"
    go run ./cmd/hunt-lf-import -target "$TARGET" -repo "$ROOT" -merge-only >/dev/null || true
  fi
  args=(-target "$TARGET" -repo "$ROOT" -import-only)
fi

count="$(go run ./cmd/hunt-lf-import "${args[@]}")"
log "imported $count seeds → .cache/hunt-lf-seeds/$TARGET"
echo "$count"
