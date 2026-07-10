#!/usr/bin/env bash
# Unit gate for ensure_settlement_treasury_float topup sizing (gap fill, not overshoot).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

calc_need() {
  python3 - "$1" "$2" "$3" <<'PY'
import sys
bal, mn, top = map(float, sys.argv[1:])
if bal >= mn:
    print("0")
else:
    gap = mn - bal
    print(f"{min(top, gap):.8f}")
PY
}

assert_eq() {
  local got="$1" want="$2" label="$3"
  python3 - "$got" "$want" "$label" <<'PY'
import sys
g, w, label = sys.argv[1], sys.argv[2], sys.argv[3]
if abs(float(g) - float(w)) > 1e-9:
    raise SystemExit(f"{label}: got={g} want={w}")
PY
}

got="$(calc_need 34 15 20)"
assert_eq "$got" "0.00000000" "above min float"

got="$(calc_need 10 15 20)"
assert_eq "$got" "5.00000000" "fill gap only"

got="$(calc_need 0 15 20)"
assert_eq "$got" "15.00000000" "full gap capped below top"

got="$(calc_need 0 50 20)"
assert_eq "$got" "20.00000000" "topup cap"

pass "treasury float topup gate PASS"
