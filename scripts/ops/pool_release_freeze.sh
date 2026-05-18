#!/usr/bin/env bash
set -euo pipefail
# Snapshot build + tests for pool release freeze (see docs/POOL_FINAL_FREEZE.md).

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT="${ROOT_DIR}/reports/pool-freeze-${STAMP}"
mkdir -p "$OUT"

{
  echo "# pool freeze ${STAMP}"
  echo
  date -u
  echo
  (command -v go >/dev/null && go version) || echo "go: not in PATH"
  echo
  if command -v git >/dev/null && git -C "$ROOT_DIR" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    echo "## git"
    git -C "$ROOT_DIR" rev-parse HEAD 2>/dev/null || true
    git -C "$ROOT_DIR" status -sb 2>/dev/null || true
  else
    echo "## git: not a repo"
  fi
} >"$OUT/meta.txt"

echo "Running go test (full)…"
if go test ./... -count=1 >"$OUT/go-test.log" 2>&1; then
  echo "go test: PASS" >>"$OUT/meta.txt"
else
  echo "go test: FAIL (see go-test.log)" >>"$OUT/meta.txt"
  exit 1
fi

echo "Building hackme…"
go build -o "$OUT/hackme" -trimpath -ldflags="-s -w" .

{
  echo "## binary"
  ls -la "$OUT/hackme" 2>/dev/null || true
  sha256sum "$OUT/hackme" 2>/dev/null || shasum -a 256 "$OUT/hackme" 2>/dev/null || true
} >>"$OUT/meta.txt"

echo "Freeze written to: $OUT"
cat "$OUT/meta.txt"
