#!/usr/bin/env bash
# Build (or reuse) a libFuzzer ASAN binary for a catalog OSS CVE target.
#
#   TARGET=mpack bash scripts/ops/build_oss_libfuzzer.sh
#   TARGETS=mpack,tinycbor,cwalk bash scripts/ops/build_oss_libfuzzer.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
export HACKME_REPO_ROOT="$ROOT"

TARGETS="${TARGETS:-${TARGET:-mpack}}"
log() { echo "[oss-lf-build $(date -u +%H:%M:%S)] $*"; }

command -v clang >/dev/null || { echo "clang required" >&2; exit 1; }
command -v go >/dev/null || { echo "go required" >&2; exit 1; }

IFS=',' read -r -a IDS <<< "$TARGETS"
for TID in "${IDS[@]}"; do
  TID="$(echo "$TID" | tr -d '[:space:]')"
  [[ -z "$TID" ]] && continue
  log "build TARGET=$TID"
  TARGETS="$TID" bash "$ROOT/scripts/ops/build_oss_cve_pack.sh"
  go run ./cmd/hunt-lf-import -target "$TID" -repo "$ROOT" -build-only
  BIN="$ROOT/.cache/hunt-lf-import/${TID}-libfuzzer-asan"
  if [[ ! -x "$BIN" ]]; then
    echo "missing fuzzer bin $BIN" >&2
    exit 1
  fi
  log "ok $BIN ($(stat -c%s "$BIN" 2>/dev/null || stat -f%z "$BIN") bytes)"
done
