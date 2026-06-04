#!/usr/bin/env bash
# Final HMC confidence battery: ISO, site, economics, phasing, customer gates → morning verdict.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="${OUT:-$ROOT/reports/final-confidence-$STAMP}"
LOG="$OUT/run.log"
FINAL="$ROOT/reports/FINAL_CONFIDENCE_VERDICT.md"
ADMIN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_admin_token" 2>/dev/null || true)"
export HACKME_ADMIN_TOKEN="${HACKME_ADMIN_TOKEN:-$ADMIN}"
export ADMIN_TOKEN="$HACKME_ADMIN_TOKEN"

mkdir -p "$OUT"
ln -sfn "$OUT" "$ROOT/reports/final-confidence-CURRENT"
exec > >(tee -a "$LOG") 2>&1

pass=0
fail=0
skip=0
declare -a ROWS=()

step() {
  local id="$1" rc
  shift
  echo ""
  echo "========== [$id] $(date -u +%H:%M:%S) =========="
  set +e
  "$@"
  rc=$?
  set -e
  if [[ "$rc" -eq 0 ]]; then
    echo "[$id] PASS"
    pass=$((pass + 1))
    ROWS+=("| $id | PASS |")
  else
    echo "[$id] FAIL (exit $rc) — see $LOG" >&2
    fail=$((fail + 1))
    ROWS+=("| $id | **FAIL** |")
  fi
}

step_optional() {
  local id="$1" rc
  shift
  echo ""
  echo "========== [$id] optional $(date -u +%H:%M:%S) =========="
  set +e
  "$@"
  rc=$?
  set -e
  if [[ "$rc" -eq 0 ]]; then
    echo "[$id] PASS"
    pass=$((pass + 1))
    ROWS+=("| $id | PASS (optional) |")
  else
    echo "[$id] SKIP/WARN"
    skip=$((skip + 1))
    ROWS+=("| $id | skip/warn |")
  fi
}

echo "[final-confidence] stamp=$STAMP out=$OUT"

step nightly_max bash "$ROOT/scripts/ops/nightly_max_gate.sh"
step hmc_verdict bash "$ROOT/scripts/tests/hmc_customer_verdict_gate.sh"
step customer_readiness bash "$ROOT/scripts/tests/customer_readiness_gate.sh"
step phasing_full env PHASE_FROM=1 POLL_SEC=300 RATE_SLEEP=22 BUDGET_RUNS=24 POOL_DISTRIBUTED=false \
  bash "$ROOT/scripts/tests/customer_real_network_phasing.sh"
step release_local env VERIFY_CDN=0 bash "$ROOT/scripts/tests/release_full_check.sh"

# After deploy (if deploy log reports PASS), verify CDN ISO SHA matches local CURRENT_VERSION (rc11l).
DEPLOY_LOG="$ROOT/reports/deploy-latest.log"
if [[ -f "$DEPLOY_LOG" ]] && grep -q 'PASS (HTTP 200)' "$DEPLOY_LOG" 2>/dev/null; then
  step cdn_iso_sha env VERIFY_CDN=1 bash "$ROOT/scripts/tests/release_full_check.sh"
else
  echo "[final-confidence] skip cdn_iso_sha (deploy not finished or missing $DEPLOY_LOG)"
  skip=$((skip + 1))
  ROWS+=("| cdn_iso_sha | skip (deploy pending) |")
fi

overnight_run=""
if [[ -L "$ROOT/reports/overnight/CURRENT" ]]; then
  overnight_run="$(basename "$(readlink -f "$ROOT/reports/overnight/CURRENT")")"
fi

overall="GO"
if [[ "$fail" -gt 0 ]]; then
  overall="NO-GO"
elif [[ "$skip" -gt 0 ]]; then
  overall="GO_WITH_GAPS"
fi

{
  echo "# Final confidence verdict — $STAMP"
  echo ""
  echo "Generated: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo ""
  echo "## Summary"
  echo ""
  echo "- **PASS:** $pass"
  echo "- **FAIL:** $fail"
  echo "- **SKIP:** $skip"
  echo "- **Overnight mining monitor:** \`${overnight_run:-none}\` → \`reports/overnight/CURRENT\`"
  echo ""
  echo "## Steps"
  echo ""
  echo "| Step | Result |"
  echo "|------|--------|"
  for row in "${ROWS[@]}"; do
    echo "$row"
  done
  echo ""
  echo "## Overall: **$overall**"
  echo ""
  if [[ "$overall" == "GO" ]]; then
    echo "HMC core (mining accrual, audits, economics, phasing, ISO local) — ready for customers."
    echo "Morning: \`bash scripts/ops/desktop_morning_report.sh\` for pool soak delta."
  elif [[ "$overall" == "GO_WITH_GAPS" ]]; then
    echo "Core gates passed; CDN/deploy or optional checks pending — see step table and \`$LOG\`."
  else
    echo "Fix failed steps in \`$LOG\` and nested reports under \`$OUT\` / \`reports/nightly-max-*\`."
  fi
  echo ""
  echo "Artifacts: \`$OUT/\`, \`reports/nightly-max-*\`, \`reports/customer-phasing-*\`, \`reports/customer-readiness-*\`"
} | tee "$OUT/VERDICT.md" >"$FINAL"

echo ""
echo "[final-confidence] done → $FINAL (pass=$pass fail=$fail skip=$skip overall=$overall)"
[[ "$fail" -eq 0 ]]
