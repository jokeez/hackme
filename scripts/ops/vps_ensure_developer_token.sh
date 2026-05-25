#!/usr/bin/env bash
# Ensure HACKME_DEVELOPER_TOKEN exists in VPS node env (idempotent). Restarts hackme-node.
# Usage: NODE_SSH=hackme-vps NODE_DEPLOY_DIR=/opt/hackme bash scripts/ops/vps_ensure_developer_token.sh
set -euo pipefail
NODE_SSH="${NODE_SSH:-}"
NODE_DEPLOY_DIR="${NODE_DEPLOY_DIR:-/opt/hackme}"
if [[ -z "$NODE_SSH" ]]; then
  echo "[dev-token] set NODE_SSH" >&2
  exit 1
fi
ssh -o BatchMode=yes "$NODE_SSH" "DEPLOY='$NODE_DEPLOY_DIR' bash -s" <<'REMOTE'
set -euo pipefail
ENV="$DEPLOY/.env.vps"
if [[ ! -f "$ENV" ]]; then
  echo "[dev-token] missing $ENV" >&2
  exit 1
fi
if grep -q '^HACKME_DEVELOPER_TOKEN=' "$ENV" 2>/dev/null; then
  echo "[dev-token] already set in .env.vps (not printing)"
else
  tok="$(openssl rand -hex 24 2>/dev/null || python3 -c 'import secrets; print(secrets.token_hex(24))')"
  echo "HACKME_DEVELOPER_TOKEN=$tok" | sudo tee -a "$ENV" >/dev/null
  sudo chmod 600 "$ENV"
  echo "[dev-token] appended HACKME_DEVELOPER_TOKEN (save from server: grep ^HACKME_DEVELOPER_TOKEN= $ENV)"
fi
sudo systemctl restart hackme-node
sleep 2
systemctl is-active hackme-node
REMOTE
