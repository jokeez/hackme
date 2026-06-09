#!/usr/bin/env bash
# Payments E2E pack: canonical-nonce transfer demo + transfers matrix + settlement visibility.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

BASE="${BASE:-http://127.0.0.1:8080}"
CANONICAL_BASE="${CANONICAL_BASE:-https://hackme.tech}"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="${OUT:-$ROOT/reports/payments-e2e-$STAMP}"
ensure_reports_dir "$OUT"

ADMIN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
if [[ -z "$ADMIN" && -f "$ROOT/.secrets/hackme_admin_token" ]]; then
  ADMIN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_admin_token")"
fi
if [[ -z "$ADMIN" && -f "$ROOT/.env.desktop" ]]; then
  ADMIN="$(grep -m1 '^HACKME_ADMIN_TOKEN=' "$ROOT/.env.desktop" | cut -d= -f2-)"
fi
export HACKME_ADMIN_TOKEN="${HACKME_ADMIN_TOKEN:-$ADMIN}"

log() { echo "[payments-e2e] $*" | tee -a "$OUT/run.log"; }

if ! curl -fsS --max-time 15 "${BASE}/api/status?lite=1" >/dev/null 2>&1; then
  log "local node down — try restart_linux_desktop_worker.sh"
  bash "$ROOT/scripts/ops/restart_linux_desktop_worker.sh" >>"$OUT/run.log" 2>&1 || true
  sleep 5
fi

FAIL=0
RUN_ID="payments_e2e_${STAMP}"

log "transfer_demo (canonical nonce, 0.01 HMC)"
if env RUN_ID="${RUN_ID}_transfer" BASE="$BASE" CANONICAL_BASE="$CANONICAL_BASE" \
  AMOUNT_HMC=0.01 ADMIN_TOKEN="$ADMIN" OUT_DIR="$OUT" \
  bash "$ROOT/scripts/tests/transfer_demo.sh" >>"$OUT/transfer_demo.log" 2>&1; then
  log "PASS transfer_demo"
else
  log "FAIL transfer_demo — see $OUT/transfer_demo.log"
  FAIL=$((FAIL + 1))
fi

log "transfers_matrix"
if env RUN_ID="${RUN_ID}_matrix" BASE="$BASE" ADMIN_TOKEN="$ADMIN" OUT_DIR="$OUT" \
  bash "$ROOT/scripts/tests/transfers_matrix.sh" >>"$OUT/transfers_matrix.log" 2>&1; then
  log "PASS transfers_matrix"
else
  log "FAIL transfers_matrix"
  FAIL=$((FAIL + 1))
fi

log "settlement API"
settle="$(curl -fsS --max-time 20 "${BASE}/api/worker/settlement" 2>/dev/null || echo '{}')"
printf '%s\n' "$settle" | jq . >"$OUT/settlement.json"
if jq -e '.ok == true' "$OUT/settlement.json" >/dev/null 2>&1; then
  log "PASS settlement API"
else
  log "WARN settlement API not ok"
fi

wallet="$(curl -fsS --max-time 60 "${BASE}/api/wallet" 2>/dev/null || echo '{}')"
printf '%s\n' "$wallet" | jq . >"$OUT/wallet.json"
jq '{address,balance_hmc,sup_balance,wallet_source,canonical_unreachable}' "$OUT/wallet.json" | tee "$OUT/wallet_summary.json"

{
  echo "# Payments E2E — $STAMP"
  echo ""
  echo "- transfer_demo: $([[ $FAIL -eq 0 ]] && echo PASS || echo FAIL)"
  echo "- wallet: \`$OUT/wallet_summary.json\`"
  echo "- settlement: \`$OUT/settlement.json\`"
} >"$OUT/VERDICT.md"

log "→ $OUT/VERDICT.md (failures=$FAIL)"
exit "$FAIL"
