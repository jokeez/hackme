#!/usr/bin/env bash
# Build OSS CVE upstream ASAN drivers (clone + clang).
#   TARGETS=cmark,miniz bash scripts/ops/build_oss_cve_pack.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

command -v clang >/dev/null || { echo "[oss-cve-pack] need clang" >&2; exit 1; }
command -v git >/dev/null || { echo "[oss-cve-pack] need git" >&2; exit 1; }

TARGETS="${TARGETS:-}"
if [[ -n "$TARGETS" && "$TARGETS" != "all" ]]; then
  IFS=',' read -r -a IDS <<< "$TARGETS"
  RUN="TestBuildAllTargets/($(IFS='|'; echo "${IDS[*]}"))"
  echo "[oss-cve-pack] compile drivers for targets=$TARGETS"
else
  RUN="TestBuildAllTargets"
  echo "[oss-cve-pack] compile drivers for all targets"
fi

HACKME_REPO_ROOT="$ROOT" go test ./internal/fuzzupstream/... -count=1 -run "$RUN" -timeout 25m
echo "[oss-cve-pack] PASS"
