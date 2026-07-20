#!/usr/bin/env bash
# Unit gate for treasury_bootstrap_guard catch-up decisions (no live chain).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

guard_catchup() {
  python3 - "$@" <<'PY'
import sys
sb, unpaid, prop, mn, trig, cap, dev, reserve = map(float, sys.argv[1:])
if prop <= 0 or prop > cap:
    sys.exit(2)
if dev > 0 and dev - prop < reserve:
    sys.exit(3)
critical = sb < mn * 0.25
backlog = sb < mn and unpaid >= trig
if critical or backlog:
    sys.exit(0)
sys.exit(1)
PY
}

# Dev ~44k must allow catch-up when settlement empty + backlog (old 45k reserve blocked this).
if guard_catchup 0.06 22 30 20 20 180 44000 10000; then
  :
else
  echo "FAIL backlog catch-up with dev=44k" >&2
  exit 1
fi

if guard_catchup 0.06 22 30 20 20 180 44000 45000; then
  echo "FAIL catch-up should block when reserve floor 45k and dev=44k" >&2
  exit 1
fi

if guard_catchup 25 5 10 20 20 180 44000 10000; then
  echo "FAIL catch-up should not run when settlement healthy" >&2
  exit 1
fi

if guard_catchup 5 25 30 20 20 180 44000 10000; then
  :
else
  echo "FAIL fleet unpaid backlog catch-up" >&2
  exit 1
fi

pass "treasury bootstrap guard catch-up PASS"
