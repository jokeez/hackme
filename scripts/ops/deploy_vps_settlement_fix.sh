#!/usr/bin/env bash
# Deploy settlement fixes: scripts, treasury float, reconcile state, settle, publish canonical JSON.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
NODE_SSH="${NODE_SSH:-hackme-vps}"
NODE_DEPLOY_DIR="${NODE_DEPLOY_DIR:-/opt/hackme}"
WALLET="${WALLET:-HMC-91fe007e4036c602}"

log() { echo "[settle-deploy] $*"; }

log "treasury float from operator wallet"
bash "$ROOT/scripts/ops/ensure_settlement_treasury_float.sh" || true

log "rsync settlement ops"
for f in settle_worker_payouts.sh sync_settlement_admin_token.sh repair_worker_settlement_state.sh \
  reconcile_settlement_state.sh publish_settlement_state.sh settlement_healthcheck.sh \
  ensure_settlement_treasury_float.sh; do
  rsync -az "$ROOT/scripts/ops/$f" "$NODE_SSH:$NODE_DEPLOY_DIR/scripts/ops/"
done
rsync -az "$ROOT/scripts/ops/systemd/hackme-worker-settlement.timer" "$NODE_SSH:/tmp/"
ssh "$NODE_SSH" "sudo cp /tmp/hackme-worker-settlement.timer /etc/systemd/system/ && sudo systemctl daemon-reload && sudo systemctl enable --now hackme-worker-settlement.timer"

log "nginx canonical settlement JSON"
ssh "$NODE_SSH" "sudo mkdir -p /opt/hackme/data && sudo chown hackme:hackme /opt/hackme/data"
if ! ssh "$NODE_SSH" "grep -q settlement/canonical.json /etc/nginx/sites-available/hackme-site-domain.conf 2>/dev/null"; then
  ssh "$NODE_SSH" 'sudo sed -i "/location ~ \^\\/api\\/(status/i\\
    location = /api/settlement/canonical.json {\\
        alias /opt/hackme/data/settlement_canonical_public.json;\\
        default_type application/json;\\
        add_header Cache-Control \"no-store\";\\
    }\\
" /etc/nginx/sites-available/hackme-site-domain.conf && sudo nginx -t && sudo systemctl reload nginx'
fi

NODE_SSH="$NODE_SSH" NODE_DEPLOY_DIR="$NODE_DEPLOY_DIR" bash "$ROOT/scripts/ops/sync_settlement_admin_token.sh"

log "reconcile state (advance settled to coordinator payout)"
ADVANCE_SETTLED=1 NODE_SSH="$NODE_SSH" NODE_DEPLOY_DIR="$NODE_DEPLOY_DIR" \
  bash "$ROOT/scripts/ops/reconcile_settlement_state.sh"

log "run settlement"
ssh "$NODE_SSH" "cd '$NODE_DEPLOY_DIR' && set -a && . ./.env.settlement && set +a && bash scripts/ops/settle_worker_payouts.sh"

log "sync desktop canonical + rebuild node if local"
bash "$ROOT/scripts/ops/sync_desktop_settlement_canonical.sh" || true

if command -v go >/dev/null; then
  log "rebuild local hackme-node (canonical merge)"
  (cd "$ROOT" && go build -o logs/desktop/hackme-node-desktop .) || true
fi

log "verify"
curl -fsS --max-time 20 "https://hackme.tech/api/settlement/canonical.json" | jq '{source,updated_unix,workers:(.workers|keys|length)}'
curl -fsS --max-time 15 "http://127.0.0.1:8080/api/worker/settlement" | jq '{ok,wallet_unpaid_hmc,wallet_settled_hmc,total_unpaid_hmc}' || true
log "done"
