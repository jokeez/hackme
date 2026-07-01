#!/usr/bin/env bash
# Rotate HACKME_ADMIN_TOKEN on VPS (.env.vps), restart node, sync settlement secrets.
# Does not print the new token (check .secrets/hackme_admin_token on server or pull locally).
#
#   NODE_SSH=hackme-vps bash scripts/ops/rotate_vps_admin_token.sh
#   NODE_SSH=hackme-vps PULL_LOCAL=1 bash scripts/ops/rotate_vps_admin_token.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
NODE_SSH="${NODE_SSH:-}"
DEPLOY="${NODE_DEPLOY_DIR:-/opt/hackme}"
PULL_LOCAL="${PULL_LOCAL:-1}"

if [[ -z "$NODE_SSH" ]]; then
  echo "[rotate-admin] set NODE_SSH=hackme-vps" >&2
  exit 1
fi

ssh -o BatchMode=yes -o ConnectTimeout=20 "$NODE_SSH" bash -s <<REMOTE
set -euo pipefail
deploy="$DEPLOY"
vps_env="\$deploy/.env.vps"
[[ -f "\$vps_env" ]] || { echo "[rotate-admin] missing \$vps_env" >&2; exit 1; }
new="HMC_ADMIN_\$(openssl rand -hex 16)"
if grep -q '^HACKME_ADMIN_TOKEN=' "\$vps_env"; then
  sed -i "s|^HACKME_ADMIN_TOKEN=.*|HACKME_ADMIN_TOKEN=\${new}|" "\$vps_env"
else
  echo "HACKME_ADMIN_TOKEN=\${new}" >>"\$vps_env"
fi
chmod 600 "\$vps_env"
mkdir -p "\$deploy/.secrets"
printf '%s' "\$new" >"\$deploy/.secrets/hackme_admin_token"
chmod 600 "\$deploy/.secrets/hackme_admin_token"
echo "[rotate-admin] token updated in .env.vps (not echoed)"
sudo systemctl restart hackme-node
sleep 3
for _ in \$(seq 1 30); do
  curl -fsS --max-time 3 http://127.0.0.1:8080/api/status?lite=1 >/dev/null 2>&1 && break
  sleep 1
done
DEPLOY="\$deploy" bash "\$deploy/scripts/ops/sync_settlement_admin_token.sh" || true
code=\$(curl -sS -o /dev/null -w '%{http_code}' -X POST http://127.0.0.1:8080/api/tasks \\
  -H 'Content-Type: application/json' -d '{}' 2>/dev/null || echo 000)
echo "[rotate-admin] unauth POST /api/tasks -> HTTP \$code (want 401)"
REMOTE

if [[ "$PULL_LOCAL" == "1" ]]; then
  mkdir -p "$ROOT/.secrets"
  scp -q "$NODE_SSH:$DEPLOY/.secrets/hackme_admin_token" "$ROOT/.secrets/hackme_admin_token.new"
  mv "$ROOT/.secrets/hackme_admin_token.new" "$ROOT/.secrets/hackme_admin_token"
  chmod 600 "$ROOT/.secrets/hackme_admin_token"
  echo "[rotate-admin] pulled token to $ROOT/.secrets/hackme_admin_token (local, not echoed)"
fi

echo "[rotate-admin] OK on $NODE_SSH"
