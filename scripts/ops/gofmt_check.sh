#!/usr/bin/env bash
# Fail if any Go file is not gofmt-formatted. Run before push / matches CI build-test-static-lang.
#
#   bash scripts/ops/gofmt_check.sh
#   bash scripts/ops/gofmt_check.sh --fix   # apply gofmt -w to listed files
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

FIX=0
if [[ "${1:-}" == "--fix" ]]; then
  FIX=1
fi

mapfile -t UNFMT < <(gofmt -l . 2>/dev/null || true)

if [[ "${#UNFMT[@]}" -eq 0 ]]; then
  echo "[gofmt-check] OK — all Go files formatted"
  exit 0
fi

if [[ "$FIX" == "1" ]]; then
  echo "[gofmt-check] applying gofmt -w to ${#UNFMT[@]} file(s)"
  gofmt -w "${UNFMT[@]}"
  echo "[gofmt-check] OK after fix"
  exit 0
fi

echo "[gofmt-check] FAIL — ${#UNFMT[@]} file(s) need gofmt -w:" >&2
printf '  %s\n' "${UNFMT[@]}" >&2
echo "[gofmt-check] fix: bash scripts/ops/gofmt_check.sh --fix" >&2
exit 1
