#!/usr/bin/env bash
# Copy hard E2E VERDICT.json artifacts into the path stable_readiness_gate expects.
#
# Usage:
#   POOL_OUT=/tmp/hackme-rc20-e2e-… HUNT_OUT=/tmp/hackme-rc20-hunt-… DEMO_OUT=/tmp/hackme-rc20-demo-… \
#     bash scripts/tests/record_money_path_proof.sh
#
# Or pass dirs as args: pool_out hunt_out demo_out
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PROOF="${HACKME_MONEY_PATH_PROOF:-$ROOT/reports/gates/money_path_e2e}"
mkdir -p "$PROOF/pool" "$PROOF/hunt" "$PROOF/demo"

POOL_OUT="${1:-${POOL_OUT:-}}"
HUNT_OUT="${2:-${HUNT_OUT:-}}"
DEMO_OUT="${3:-${DEMO_OUT:-}}"

copy_one() {
  local suite="$1" src="$2"
  [[ -n "$src" && -f "$src/VERDICT.json" ]] || {
    echo "[record-money-path] skip $suite (missing $src/VERDICT.json)" >&2
    return 1
  }
  python3 - <<PY
import json,sys
v=json.load(open("$src/VERDICT.json"))
assert v.get("ok") and v.get("pickup"), v
assert not v.get("soft_pickup"), "soft_pickup not allowed"
print(json.dumps({**v, "suite":"$suite", "source":"$src"}, indent=2))
PY
  cp -f "$src/VERDICT.json" "$PROOF/$suite/VERDICT.json"
  echo "[record-money-path] recorded $suite <- $src/VERDICT.json"
}

ok=0
copy_one pool "$POOL_OUT" && ok=$((ok + 1)) || true
copy_one hunt "$HUNT_OUT" && ok=$((ok + 1)) || true
copy_one demo "$DEMO_OUT" && ok=$((ok + 1)) || true

cat >"$PROOF/README.txt" <<EOF
Money-path hard E2E proof (no ALLOW_SOFT_PICKUP).
Recorded: $(date -u +%Y-%m-%dT%H:%M:%SZ)
Suites present: $(ls -1 "$PROOF"/*/VERDICT.json 2>/dev/null | wc -l)
EOF

HACKME_MONEY_PATH_PROOF="$PROOF" python3 "$ROOT/scripts/tests/lib/money_path_proof.py" || {
  echo "[record-money-path] incomplete ($ok/3) — gate will still fail until all suites recorded" >&2
  exit 1
}
echo "[record-money-path] proof ready at $PROOF"
