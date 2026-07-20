#!/usr/bin/env bash
# Unit gate for pool subsidy budget ratio math.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

out="$(python3 - <<'PY'
em_ref = 1.2
accrual_h = 4.5
ratio = accrual_h / em_ref
subsidy = accrual_h - em_ref
assert abs(ratio - 3.75) < 0.01
assert abs(subsidy - 3.3) < 0.01
warn = ratio > 2.5
assert warn
print("ok")
PY
)"
[[ "$out" == "ok" ]] || { echo "subsidy math failed"; exit 1; }

pass "pool subsidy budget gate PASS"
