#!/usr/bin/env bash
# Phase 0 fuzz regression gate — offline positive/negative ASAN controls + bootstrap hygiene.
#
# Positive: intentional demo_stack_overflow → native_crash (Tier-C pipeline alive).
# Negative: clean inputs → rejected (CLEAN — no false CVE signal).
# Hygiene: bootstrap MAX_BUDGET_RUNS cap + empty revive IDS (no 24k walls).
#
# Usage:
#   bash scripts/tests/fuzz_regression_gate.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

echo "[fuzz-regression] [1/4] bootstrap pool hygiene"
for f in \
  "$ROOT/scripts/ops/bootstrap_customer/bootstrap_bot.sh" \
  "$ROOT/scripts/ops/bootstrap_customer/place_bootstrap_order.sh"; do
  [[ -f "$f" ]] || fail "missing $f"
  grep -q 'MAX_BUDGET_RUNS="${MAX_BUDGET_RUNS:-5000}"' "$f" || fail "$f missing MAX_BUDGET_RUNS cap"
done
BUDGET_RUNS=24000
MAX_BUDGET_RUNS=5000
if [[ "$BUDGET_RUNS" -gt "$MAX_BUDGET_RUNS" ]]; then
  BUDGET_RUNS="$MAX_BUDGET_RUNS"
fi
[[ "$BUDGET_RUNS" -eq 5000 ]] || fail "bootstrap clamp simulation failed"
python3 - "$ROOT/scripts/ops/revive_bootstrap_deep_pool_campaigns.sh" <<'PY'
import re, sys, pathlib
text = pathlib.Path(sys.argv[1]).read_text()
m = re.search(r'IDS=\(\s*(.*?)\s*\)', text, re.S)
if not m:
    raise SystemExit("revive script missing IDS=() block")
inner = m.group(1)
lines = [
    ln.strip()
    for ln in inner.splitlines()
    if ln.strip() and not ln.strip().startswith('#')
]
if lines:
    raise SystemExit(f"revive IDS must stay empty (legacy 24k walls); found: {lines}")
print("revive IDS empty ok")
PY

command -v clang >/dev/null || fail "clang required for ASAN positive/negative controls (apt install clang)"

echo "[fuzz-regression] [2/4] go fuzznative ASAN repro (bitcoin + demo)"
go test ./internal/fuzznative/... -count=1 -run 'TestEvalReproAsanBinary'

echo "[fuzz-regression] [3/4] positive: tier_c_demo end-to-end"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
HACKME_REPO_ROOT="$ROOT" go run ./tools/tier_c_demo/ -guard demo_stack_overflow -out "$tmp"

echo "[fuzz-regression] [4/4] depth engine smoke"
go test ./internal/fuzzengine/... -count=1 -run 'Depth|Tier|Repro'

pass "fuzz_regression_gate PASS"
