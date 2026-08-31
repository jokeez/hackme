#!/usr/bin/env bash
# Gate: Hunt C/C++ inventory — multi-file C++ harness + CMake hints.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

echo "[hunt-inventory-cpp-gate] unit tests"
go test -count=1 ./internal/hunt/... -run 'Inventory|Harness|SourceLanguage|Companion|CMake|Cpp|Pack|Template' -timeout=3m

pass "hunt_inventory_cpp_gate PASS"
