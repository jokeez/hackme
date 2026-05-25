#!/usr/bin/env bash
# Post a small fuzzing order on VPS canonical + verify coordinator orders mode.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
NODE_SSH="${NODE_SSH:-hackme-vps}"
WALLET="${WALLET:-HMC-91fe007e4036c602}"

ssh -i "${HOME}/.ssh/id_ed25519" -o ConnectTimeout=15 "$NODE_SSH" bash -s <<'REMOTE'
set -euo pipefail
cd /opt/hackme
ADMIN=$(grep '^HACKME_ADMIN_TOKEN=' .env.vps | cut -d= -f2-)
TS=$(date +%s)
ID="fuzzing-prep-$TS"
HTTP=$(curl -sS -o /tmp/ph.json -w '%{http_code}' -X POST http://127.0.0.1:18080/api/tasks \
  -H "Content-Type: application/json" -H "X-Hackme-Admin-Token: $ADMIN" \
  -d "{\"id\":\"$ID\",\"kind\":\"synthetic_poh_v1\",\"difficulty_score\":4,\"reward_hmc\":0.01,\"target_solves\":2,\"payer_ref\":\"fuzzing:prep\"}")
echo "order $ID HTTP $HTTP"
cat /tmp/ph.json | head -c 200
echo
sleep 4
curl -fsS http://127.0.0.1:18081/api/work/stats | jq '{orders_active,scheduler_mode,target_mod}'
curl -fsS http://127.0.0.1:18080/api/tasks -H "X-Hackme-Admin-Token: $ADMIN" | \
  jq --arg id "$ID" '.tasks[] | select(.id==$id) | {id,status,progress_pct,difficulty_score}'
REMOTE

TOKEN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_coordinator_admin_token")"
curl -fsS -H "X-Hackme-Admin-Token: $TOKEN" "https://hackme.tech/pool/coordinator/api/work/stats" | \
  jq '{orders_active,scheduler_mode}'
