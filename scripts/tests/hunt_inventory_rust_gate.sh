#!/usr/bin/env bash
# Gate: Hunt Rust Phase A — inventory detect + serde_json catalog ASAN build/smoke.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

echo "[hunt-inventory-rust-gate] unit tests (inventory + language)"
go test -count=1 ./internal/hunt/... -run 'SourceLanguage|ScanInventoryFindsRust|ListCatalogTargetsIncludesRust|Inventory' -timeout=3m

echo "[hunt-inventory-rust-gate] fuzzupstream rust targets + smoke"
if rustc +nightly --version >/dev/null 2>&1; then
  go test -count=1 ./internal/fuzzupstream/... -run 'TargetLanguage|HuntSerdeJSONSmoke|HuntMemchrSmoke|HuntQuickXMLSmoke|BuildAllTargets/serde_json|BuildAllTargets/memchr|BuildAllTargets/quick_xml' -timeout=20m
else
  echo "[hunt-inventory-rust-gate] WARN: rustc +nightly missing — skipping ASAN smoke (install: rustup toolchain install nightly)"
  go test -count=1 ./internal/fuzzupstream/... -run 'TargetLanguage' -timeout=1m
fi

pass "hunt_inventory_rust_gate PASS"
