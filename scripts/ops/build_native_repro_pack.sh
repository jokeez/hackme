#!/usr/bin/env bash
# Pre-build ASAN repro binaries for all upstream harness guards (Tier C cache warm).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

command -v clang >/dev/null || { echo "[native-repro-pack] need clang" >&2; exit 1; }

echo "[native-repro-pack] build upstream WASM (harness sources)"
bash "$ROOT/scripts/build_upstream_l1_pack.sh" >/dev/null

echo "[native-repro-pack] warm ASAN binary cache via go test"
HACKME_REPO_ROOT="$ROOT" go test ./internal/fuzznative/... -count=1 -run 'TestEvalReproAsanBinary'

echo "[native-repro-pack] cache dir:"
ls -la "$ROOT/.cache/native-repro/" 2>/dev/null | head -20 || echo "  (empty — tests may have skipped)"
echo "[native-repro-pack] PASS"
