#!/usr/bin/env bash
# Fail if main.go Version, app.js RELEASE_VER, and CURRENT_VERSION diverge.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
GO_VER="$(grep -E '^\s*Version\s*=' "$ROOT/main.go" | sed -n 's/.*"\([^"]*\)".*/\1/p' | head -1)"
JS_VER="$(grep -oE 'RELEASE_VER = "[^"]+"' "$ROOT/web/site/assets/app.js" | sed 's/.*"\([^"]*\)".*/\1/')"
CUR_VER="$(tr -d ' \n\r' <"$ROOT/scripts/release/CURRENT_VERSION")"
fail=0
echo "[version-gate] main.go=$GO_VER app.js=$JS_VER CURRENT_VERSION=$CUR_VER"
if [[ "$GO_VER" != "$JS_VER" ]]; then
  echo "[version-gate] FAIL main.go != app.js" >&2
  fail=$((fail + 1))
fi
if [[ "$GO_VER" != "$CUR_VER" ]]; then
  echo "[version-gate] FAIL main.go != CURRENT_VERSION" >&2
  fail=$((fail + 1))
fi
if [[ "$fail" -gt 0 ]]; then exit 1; fi
echo "[version-gate] OK — single channel $GO_VER (hold at rc11l until hardware verified)"
