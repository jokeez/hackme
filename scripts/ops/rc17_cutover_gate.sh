#!/usr/bin/env bash
# rc17_cutover_gate.sh — local pre-cut checks. NO deploy, NO restart.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
DEMO="${HACKME_EXCHANGE_DEMO:-$ROOT/../hackme-exchange-demo}"
API="${HACKME_EXCHANGE_API:-$ROOT/../hackme-exchange-api}"
FAIL=0
pass() { echo "[rc17-gate] PASS: $*"; }
fail() { echo "[rc17-gate] FAIL: $*" >&2; FAIL=1; }

echo "[rc17-gate] === hackme hub embed config ==="
if grep -q "EXCHANGE_SPA_ORIGIN_DEFAULT = 'https://exchange.hackme.tech'" "$ROOT/dashboard.html"; then
  pass "dashboard default origin exchange.hackme.tech"
else
  fail "dashboard missing EXCHANGE_SPA_ORIGIN_DEFAULT exchange.hackme.tech"
fi
if grep -q 'frame-src.*https://exchange.hackme.tech' "$ROOT/main.go"; then
  pass "hub CSP frame-src exchange.hackme.tech"
else
  fail "main.go missing frame-src for exchange.hackme.tech"
fi
if grep -q 'sup/activity' "$ROOT/scripts/ops/nginx/hackme-site-domain.tls.conf"; then
  pass "nginx template lists sup/activity"
else
  fail "nginx template missing sup/activity allowlist"
fi
if grep -q '/api/sup/tx/send' "$ROOT/scripts/ops/nginx/hackme-site-domain.tls.conf"; then
  pass "nginx template lists sup/tx/send"
else
  fail "nginx template missing sup/tx/send"
fi

echo "[rc17-gate] === hackme unit tests (SUP + chain) ==="
go test ./internal/chain/... -count=1 -run 'SUP|AuditSUP' || FAIL=1
go test ./cmd/coordinator/... -count=1 -run 'SUP|sup' || FAIL=1

echo "[rc17-gate] === exchange SPA embed (sibling) ==="
if [[ -d "$DEMO" ]]; then
  if (cd "$DEMO" && npm test -- src/embed.test.ts --run 2>/dev/null); then
    pass "embed.test.ts"
  else
    fail "embed.test.ts in $DEMO"
  fi
  if grep -q 'hackme\.tech' "$DEMO/vite.config.ts"; then
    pass "paper CSP frame-ancestors hackme.tech"
  else
    fail "vite.config.ts missing hackme.tech frame-ancestors"
  fi
else
  echo "[rc17-gate] SKIP: demo repo not at $DEMO"
fi

echo "[rc17-gate] === exchange API (sibling, optional) ==="
if [[ -d "$API" ]]; then
  if (cd "$API" && go test ./internal/ledger/... ./internal/nodewatch/... ./internal/httpapi/... -count=1 2>/dev/null); then
    pass "exchange-api ledger/nodewatch/httpapi"
  else
    fail "exchange-api tests"
  fi
else
  echo "[rc17-gate] SKIP: API repo not at $API"
fi

echo "[rc17-gate] === live SUP probes (read-only, optional) ==="
if [[ "${RC17_SKIP_LIVE:-0}" != "1" ]]; then
  bash "$ROOT/scripts/ops/sup_full_verdict_gate.sh" || FAIL=1
else
  echo "[rc17-gate] SKIP: RC17_SKIP_LIVE=1"
fi

if [[ "$FAIL" -ne 0 ]]; then
  echo "[rc17-gate] RED — fix before rc17 cut" >&2
  exit 1
fi
echo "[rc17-gate] GREEN — ready for rc17 bundle (deploy still manual)"
