#!/usr/bin/env bash
# Build all OSS CVE upstream ASAN drivers (clone + clang).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

command -v clang >/dev/null || { echo "[oss-cve-pack] need clang" >&2; exit 1; }
command -v git >/dev/null || { echo "[oss-cve-pack] need git" >&2; exit 1; }

echo "[oss-cve-pack] compile drivers for all targets"
HACKME_REPO_ROOT="$ROOT" go test ./internal/fuzzupstream/... -count=1 -run TestBuildAllTargets -timeout 15m

echo "[oss-cve-pack] PASS"
