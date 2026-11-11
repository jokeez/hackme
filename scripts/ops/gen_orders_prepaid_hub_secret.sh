#!/usr/bin/env bash
# Generate HACKME_ORDERS_PREPAID_HUB_SECRET (hub-signed prepaid tickets).
# Must differ from admin / worker / Orders publish tokens.
#
#   bash scripts/ops/gen_orders_prepaid_hub_secret.sh
#   FORCE=1 bash scripts/ops/gen_orders_prepaid_hub_secret.sh
#
# Hub coordinator + Orders nodes:
#   HACKME_ORDERS_PREPAID_HUB_SECRET=<same>
# On D1 set also HACKME_ORDERS_REQUIRE_HUB_PREPAID_TICKET=1 (or HACKME_CHAIN_MINTING_HUB=1)
# on the coordinator so forgeable publish-token self-MAC is refused.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT="${1:-$ROOT/.secrets/hackme_orders_prepaid_hub_secret}"
mkdir -p "$(dirname "$OUT")"
if [[ -f "$OUT" && "${FORCE:-0}" != "1" ]]; then
  echo "exists: $OUT (set FORCE=1 to overwrite)" >&2
  exit 1
fi
tok="$(openssl rand -hex 32)"
for other in \
  "$ROOT/.secrets/hackme_coordinator_worker_token" \
  "$ROOT/.secrets/hackme_coordinator_admin_token" \
  "$ROOT/.secrets/hackme_coordinator_orders_publish_token"; do
  if [[ -f "$other" ]]; then
    o="$(tr -d '\r\n' <"$other")"
    if [[ -n "$o" && "$o" == "$tok" ]]; then
      echo "collision with $(basename "$other") — retry" >&2
      exit 1
    fi
  fi
done
printf '%s' "$tok" >"$OUT"
chmod 600 "$OUT"
echo "wrote $OUT (${#tok} hex chars)"
echo "Hub+Orders: HACKME_ORDERS_PREPAID_HUB_SECRET=<value>"
echo "D1 coord:   HACKME_ORDERS_REQUIRE_HUB_PREPAID_TICKET=1  (or HACKME_CHAIN_MINTING_HUB=1)"
echo "Admin mint: POST /api/fuzz/pool/prepaid-ticket (admin token only)"
