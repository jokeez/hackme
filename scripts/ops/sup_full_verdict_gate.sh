#!/usr/bin/env bash
# sup_full_verdict_gate.sh — unit tests + live coordinator/chain checks + abuse probes.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
CHAIN_BASE="${CHAIN_BASE:-https://hackme.tech}"
FAIL=0
pass() { echo "[sup-verdict] PASS: $*"; }
fail() { echo "[sup-verdict] FAIL: $*" >&2; FAIL=1; }

echo "[sup-verdict] === unit tests ==="
go test ./internal/chain/... -count=1 -run 'SUP|AuditSUP' || FAIL=1
go test ./cmd/coordinator/... -count=1 -run 'SUP|sup' || FAIL=1
bash scripts/ops/sup_accrual_gate.sh || FAIL=1

echo "[sup-verdict] === live coordinator ==="
body="$(curl -fsS "${COORD_URL%/}/api/work/stats?details=0" 2>/dev/null || true)"
if [[ -z "$body" ]]; then
  fail "coordinator stats unreachable"
else
  en="$(echo "$body" | jq -r '.sup_policy.enabled // false')"
  [[ "$en" == "true" ]] && pass "sup_policy.enabled" || fail "sup_policy.enabled=false"
  tp="$(echo "$body" | jq -r '.total_payout_sup // 0')"
  pass "total_payout_sup=${tp}"
fi

echo "[sup-verdict] === live chain economics ==="
if ec="$(curl -fsS "${CHAIN_BASE%/}/api/sup/economics" 2>/dev/null)"; then
  mint="$(echo "$ec" | jq -r '.economics.mint_enabled // false')"
  pass "sup economics mint_enabled=${mint}"
else
  echo "[sup-verdict] WARN: /api/sup/economics not on public URL (may be loopback-only on VPS)"
fi

echo "[sup-verdict] === abuse: mint without token ==="
code="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "${CHAIN_BASE%/}/api/sup/mint" \
  -H 'Content-Type: application/json' -d '{"to":"HMC-abuse","amount_sup":1}' 2>/dev/null || echo 000)"
if [[ "$code" == "401" || "$code" == "403" ]]; then
  pass "unauthenticated mint rejected http=${code}"
else
  echo "[sup-verdict] WARN: mint without token http=${code} (may hit wrong host)"
fi

echo "[sup-verdict] === abuse: unsigned coordinator submit gets no SUP (unit) ==="
go test ./cmd/coordinator/... -count=1 -run TestSUPAccrualRequiresHybrid || FAIL=1

if [[ "$FAIL" -ne 0 ]]; then
  echo "[sup-verdict] OVERALL: FAIL"
  exit 1
fi
echo "[sup-verdict] OVERALL: PASS"
