#!/usr/bin/env bash
# Overnight / max confidence: ISO + site + HMC gates (logs under reports/nightly-max-*).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT="${OUT:-$ROOT/reports/nightly-max-$STAMP}"
LOG="$OUT/run.log"
ISO="${ISO:-$ROOT/dist/release_0.1.0-rc11k/HackMe-OS-0.1.0-rc11k-amd64.iso}"
ADMIN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_admin_token" 2>/dev/null || true)"

mkdir -p "$OUT"
exec > >(tee -a "$LOG") 2>&1

pass=0
fail=0
skip=0

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
  else
    echo "[$id] FAIL (see $LOG)" >&2
    fail=$((fail + 1))
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
  else
    echo "[$id] SKIP/WARN"
    skip=$((skip + 1))
  fi
}

echo "[nightly-max] stamp=$STAMP out=$OUT iso=$ISO"

step iso_verify bash "$ROOT/scripts/tests/verify_hackme_iso.sh" "$ISO"
step iso_qemu env TIMEOUT_SEC=300 bash "$ROOT/scripts/tests/iso_qemu_boot_smoke.sh" "$ISO" "$OUT/iso-qemu.log"
step public_site bash "$ROOT/scripts/tests/public_site_smoke.sh"
step_optional site_release bash "$ROOT/scripts/tests/site_release_consistency_gate.sh"
step go_test_short bash -c 'cd "$1" && env -u HACKME_PUBLIC_AUTHORITY_BASE go test -short -count=1 ./... -timeout=600s' _ "$ROOT"
step economics bash "$ROOT/scripts/tests/economics_confidence_gate.sh"
if [[ -n "$ADMIN" ]]; then
  step orders_multilang env ADMIN_TOKEN="$ADMIN" HACKME_ADMIN_TOKEN="$ADMIN" \
    bash "$ROOT/scripts/tests/orders_multilang_audit.sh"
  step_optional hmc_verdict env ADMIN_TOKEN="$ADMIN" HACKME_ADMIN_TOKEN="$ADMIN" \
    bash "$ROOT/scripts/tests/hmc_customer_verdict_gate.sh"
else
  echo "[nightly-max] skip multilang/hmc_verdict (no admin token)"
  skip=$((skip + 2))
fi

{
  echo "# Nightly max gate — $STAMP"
  echo ""
  echo "- PASS: $pass"
  echo "- FAIL: $fail"
  echo "- SKIP: $skip"
  echo ""
  if [[ "$fail" -eq 0 ]]; then
    echo "## Verdict: **GO**"
  else
    echo "## Verdict: **NO-GO** — see $LOG"
  fi
} >"$OUT/VERDICT.md"

echo ""
echo "[nightly-max] done → $OUT/VERDICT.md (pass=$pass fail=$fail skip=$skip)"
[[ "$fail" -eq 0 ]]
