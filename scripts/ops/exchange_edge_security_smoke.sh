#!/usr/bin/env bash
# Public-edge security smoke: CORS, CSP leakage, auth gates, lab/admin disabled.
#   bash scripts/ops/exchange_edge_security_smoke.sh
set -euo pipefail

API="${EXCHANGE_API_BASE:-https://exchange-api.hackme.tech}"
DESK="${EXCHANGE_DESK_BASE:-https://exchange.hackme.tech}"
fail=0

pass() { echo "PASS  $*"; }
bad() { echo "FAIL  $*"; fail=$((fail + 1)); }

code_of() {
  local method="$1" url="$2" data="${3:-}"
  if [[ "$method" == "GET" ]]; then
    curl -sS -m 15 -o /tmp/edge-smoke.body -w '%{http_code}' "$url" || echo err
  else
    curl -sS -m 15 -o /tmp/edge-smoke.body -w '%{http_code}' -X "$method" \
      -H 'Content-Type: application/json' -d "${data:-{}}" "$url" || echo err
  fi
}

echo "=== CORS ==="
evil_opt=$(curl -sS -m 15 -o /dev/null -w '%{http_code}' -X OPTIONS "$API/tickers" \
  -H 'Origin: https://evil.example' -H 'Access-Control-Request-Method: GET' || echo err)
[[ "$evil_opt" == "403" ]] && pass "OPTIONS evil Origin → 403" || bad "OPTIONS evil → $evil_opt (want 403)"

good_acao=$(curl -sS -m 15 -D - -o /dev/null -H 'Origin: https://exchange.hackme.tech' "$API/tickers" \
  | tr -d '\r' | grep -i '^access-control-allow-origin:' | head -1 || true)
echo "$good_acao" | grep -qi 'https://exchange.hackme.tech' \
  && pass "GET tickers allowlists exchange.hackme.tech" \
  || bad "missing ACAO for desk origin ($good_acao)"

evil_acao=$(curl -sS -m 15 -D - -o /dev/null -H 'Origin: https://evil.example' "$API/tickers" \
  | tr -d '\r' | grep -i '^access-control-allow-origin:' || true)
[[ -z "$evil_acao" ]] && pass "GET tickers no ACAO for evil Origin" || bad "reflected ACAO: $evil_acao"

echo "=== CSP (HTTP header is the real gate) ==="
csp=$(curl -sSI -m 15 "$DESK/" | tr -d '\r' | grep -i '^content-security-policy:' | head -1 || true)
[[ -n "$csp" ]] && pass "HTTP CSP present" || bad "missing HTTP CSP"
echo "$csp" | grep -qi "connect-src 'self' https://hackme.tech" && pass "connect-src tight (self+hub)" || bad "connect-src unexpected: $csp"
echo "$csp" | grep -qi 'exchange-api\.hackme\.tech' && bad "CSP still lists exchange-api host" || pass "no exchange-api in CSP"
echo "$csp" | grep -qi 'api\.binance\.com' && bad "CSP still lists api.binance.com" || pass "no binance in CSP"
echo "$csp" | grep -qi 'cloudflareinsights' && bad "CSP still lists cloudflareinsights" || pass "no CF insights in CSP"
echo "$csp" | grep -qi "frame-ancestors.*https://hackme.tech" && pass "frame-ancestors allows hub" || bad "frame-ancestors missing hub"

xfo=$(curl -sSI -m 15 "$DESK/" | tr -d '\r' | grep -i '^x-frame-options:' || true)
echo "$xfo" | grep -qi 'SAMEORIGIN' && bad "XFO SAMEORIGIN blocks hub iframe" || pass "no XFO SAMEORIGIN"

echo "=== Auth / lab / admin gates ==="
for path in /balances /orders /fills '/deposit/address?asset=HMC' /withdrawals /vip; do
  c=$(code_of GET "$API$path")
  [[ "$c" == "401" ]] && pass "GET $path → 401" || bad "GET $path → $c (want 401)"
done
for path in /metrics /openapi.yaml /ws/market; do
  c=$(code_of GET "$API$path")
  [[ "$c" == "404" ]] && pass "GET $path → 404 (edge off)" || bad "GET $path → $c (want 404)"
done
for path in /lab/deposit /lab/mm/seed /lab/counterparty /lab/mark; do
  c=$(code_of POST "$API$path" '{}')
  [[ "$c" == "404" ]] && pass "POST $path → 404 (lab off)" || bad "POST $path → $c (want 404)"
done
for path in /admin/credit /admin/kyt/decide /admin/withdraw/complete; do
  c=$(code_of POST "$API$path" '{}')
  [[ "$c" == "404" ]] && pass "POST $path → 404 (admin off)" || bad "POST $path → $c (want 404)"
done
c=$(code_of POST "$API/orders" '{"pair":"HMC/USDT","side":"buy","type":"limit","qty":1,"price":1}')
[[ "$c" == "401" ]] && pass "POST /orders → 401" || bad "POST /orders → $c"

echo "=== Same-origin desk + mark proxy ==="
c=$(code_of GET "$DESK/desk-api/health")
[[ "$c" == "200" ]] && pass "/desk-api/health 200" || bad "/desk-api/health → $c"
c=$(code_of GET "$DESK/mark-proxy/btc")
[[ "$c" == "200" ]] && pass "/mark-proxy/btc 200" || bad "/mark-proxy/btc → $c"
c=$(code_of GET "$DESK/mark-proxy/evil")
[[ "$c" == "404" ]] && pass "/mark-proxy/* open-proxy blocked" || bad "/mark-proxy/evil → $c (want 404)"

if [[ "$fail" -gt 0 ]]; then
  echo "RESULT FAIL ($fail)"
  exit 1
fi
echo "RESULT PASS"
