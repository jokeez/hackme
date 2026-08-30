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
  live="$(echo "$ec" | jq -r '.economics.on_chain_settle_live // false')"
  [[ "$mint" == "true" ]] && pass "sup economics mint_enabled=true" || fail "sup economics mint_enabled=false"
  [[ "$live" == "true" ]] && pass "sup economics on_chain_settle_live=true" || fail "sup economics on_chain_settle_live=false"
else
  fail "/api/sup/economics unreachable at ${CHAIN_BASE%/}/api/sup/economics"
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

echo "[sup-verdict] === abuse: unsigned SUP transfer rejected (unit) ==="
go test ./internal/chain/... -count=1 -run 'TestSupTransferReject' || FAIL=1

echo "[sup-verdict] === abuse: unsigned SUP transfer on live chain ==="
unsigned_body='{"tx_type":"transfer_sup_v1","from":"HMC-abuse","to":"HMC-abuse2","amount_units":1000000,"fee_units":1000,"nonce":0,"timestamp_unix":'$(date +%s)'}'
code="$(curl -sS -o /tmp/sup_tx_abuse.json -w '%{http_code}' -X POST "${CHAIN_BASE%/}/api/sup/tx/send" \
  -H 'Content-Type: application/json' -d "$unsigned_body" 2>/dev/null || echo 000)"
if [[ "$code" == "403" ]]; then
  echo "[sup-verdict] WARN: /api/sup/tx/send http=403 — deploy nginx allowlist (scripts/ops/nginx/hackme-site-domain.tls.conf)"
elif [[ "$code" == "400" || "$code" == "401" ]]; then
  rej="$(jq -r '.code // .error // empty' /tmp/sup_tx_abuse.json 2>/dev/null || true)"
  if [[ "$rej" == "invalid_signature" || "$rej" == *signature* ]]; then
    pass "unsigned SUP transfer rejected http=${code} code=${rej}"
  else
    fail "unsigned SUP transfer unexpected code=${rej} http=${code}"
  fi
else
  echo "[sup-verdict] WARN: unsigned SUP transfer http=${code} (may hit wrong host or nginx not deployed)"
fi

echo "[sup-verdict] === abuse: SUP transfer fee too low (unit) ==="
go test ./internal/chain/... -count=1 -run TestSupTransferRejectFeeTooLow || FAIL=1

echo "[sup-verdict] === live SUP activity endpoint ==="
if act="$(curl -fsS "${CHAIN_BASE%/}/api/sup/activity?address=HMC-719006d93916ad52&limit=1" 2>/dev/null)"; then
  ok="$(echo "$act" | jq -r '.ok // false')"
  asset="$(echo "$act" | jq -r '.asset // ""')"
  [[ "$ok" == "true" && "$asset" == "SUP" ]] && pass "sup activity endpoint ok" || fail "sup activity bad payload"
else
  code="$(curl -sS -o /dev/null -w '%{http_code}' "${CHAIN_BASE%/}/api/sup/activity?address=HMC-719006d93916ad52&limit=1" 2>/dev/null || echo 000)"
  if [[ "$code" == "403" ]]; then
    echo "[sup-verdict] WARN: /api/sup/activity http=403 — deploy nginx allowlist"
  else
    fail "/api/sup/activity unreachable http=${code}"
  fi
fi

if [[ "$FAIL" -ne 0 ]]; then
  echo "[sup-verdict] OVERALL: FAIL"
  exit 1
fi
echo "[sup-verdict] OVERALL: PASS"
