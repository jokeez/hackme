#!/usr/bin/env bash
# Revoke pool abuser: roll back HMC/SUP accrual, permaban worker + IP, prune node mirror.
#
#   WORKER_ID=worker-hdssh01-public-rust bash scripts/ops/revoke_pool_abuser.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

WORKER_ID="${WORKER_ID:-worker-hdssh01-public-rust}"
COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_COORDINATOR_ADMIN_TOKEN:-}}"

if [[ -z "$ADMIN_TOKEN" && -f "$ROOT/.secrets/hackme_coordinator_admin_token" ]]; then
  ADMIN_TOKEN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_coordinator_admin_token")"
fi
if [[ -z "$ADMIN_TOKEN" && -f "$ROOT/.secrets/hackme_admin_token" ]]; then
  ADMIN_TOKEN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_admin_token")"
fi
[[ -n "$ADMIN_TOKEN" ]] || { echo "[revoke-abuser] set ADMIN_TOKEN" >&2; exit 2; }

body="$(jq -nc --arg w "$WORKER_ID" '{worker_id:$w}')"

echo "[revoke-abuser] before stats (worker row)"
curl -fsS --max-time 20 "${COORD_URL%/}/api/work/stats?details=1" \
  -H "X-Hackme-Admin-Token: $ADMIN_TOKEN" \
  | jq --arg w "$WORKER_ID" '{total_payout_hmc, worker:.workers[$w]}' || true

echo "[revoke-abuser] POST revoke-worker id=$WORKER_ID"
resp="$(curl -fsS --max-time 30 -X POST "${COORD_URL%/}/api/work/admin/revoke-worker" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: $ADMIN_TOKEN" \
  -d "$body")"
echo "$resp" | jq .

# Local node mirror (desktop proxies coordinator but mirror may retain abuser row).
if curl -fsS --max-time 5 'http://127.0.0.1:8080/api/status?lite=1' >/dev/null 2>&1; then
  echo "[revoke-abuser] local node revoke proxy"
  curl -fsS --max-time 30 -X POST 'http://127.0.0.1:8080/api/work/admin/revoke-worker' \
    -H "Content-Type: application/json" \
    -H "X-Hackme-Admin-Token: $ADMIN_TOKEN" \
    -d "$body" | jq . || true
fi

echo "[revoke-abuser] after stats"
curl -fsS --max-time 20 "${COORD_URL%/}/api/work/stats?details=1" \
  -H "X-Hackme-Admin-Token: $ADMIN_TOKEN" \
  | jq --arg w "$WORKER_ID" '{total_payout_hmc, has_abuser:(.workers[$w]!=null), top_workers:(.workers|to_entries|map({id:.key,payout:.value.payout_hmc})|sort_by(-.payout)|.[0:5])}'
