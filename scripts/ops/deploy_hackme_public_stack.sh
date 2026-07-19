#!/usr/bin/env bash
# Operator-only. Prefer DRY_RUN=1 when the script supports it. Confirm remote target before run.
# One-shot public deploy: TLS nginx vhost → hackme-node (+ coordinator restart) → static site (+ optional dist).
#
# Requires passwordless SSH and sudo nginx on the remote (see README → hackme-vps).
#
# Usage:
#   NODE_SSH=hackme-vps NODE_DEPLOY_DIR=/opt/hackme bash scripts/ops/deploy_hackme_public_stack.sh
#
# Optional:
#   SKIP_NGINX=1           — skip uploading nginx vhost
#   SKIP_NODE=1           — skip deploy_hackme_node.sh (binary + repo rsync + systemd restart)
#   SKIP_SITE=1           — skip deploy_hackme_site.sh (default 1: web/dist ride along SYNC_DIST node rsync)
#   SYNC_DIST=1           — default for this script only; include dist/ in deploy_node rsync (release bundles)
#   NGINX_SITE_CONF=path — override vhost file (default: scripts/ops/nginx/hackme-site-domain.tls.conf)

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"
_OPS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "$_OPS_DIR/_deploy_ssh_retry.sh"

_deploy_ssh() {
  if [[ -n "${HACKME_DEPLOY_SSH_IDENTITY:-}" && -f "${HACKME_DEPLOY_SSH_IDENTITY}" ]]; then
    ssh -i "${HACKME_DEPLOY_SSH_IDENTITY}" -o IdentitiesOnly=yes -o BatchMode=yes -o StrictHostKeyChecking=accept-new "$@"
  else
    ssh "$@"
  fi
}
_deploy_scp() {
  if [[ -n "${HACKME_DEPLOY_SSH_IDENTITY:-}" && -f "${HACKME_DEPLOY_SSH_IDENTITY}" ]]; then
    scp -i "${HACKME_DEPLOY_SSH_IDENTITY}" -o IdentitiesOnly=yes -o BatchMode=yes -o StrictHostKeyChecking=accept-new "$@"
  else
    scp "$@"
  fi
}

NODE_SSH="${NODE_SSH:-}"
NODE_DEPLOY_DIR="${NODE_DEPLOY_DIR:-/opt/hackme}"
SKIP_NGINX="${SKIP_NGINX:-0}"
SKIP_NODE="${SKIP_NODE:-0}"
SKIP_SITE="${SKIP_SITE:-1}"
SYNC_DIST="${SYNC_DIST:-1}"
NGINX_SITE_CONF="${NGINX_SITE_CONF:-$ROOT_DIR/scripts/ops/nginx/hackme-site-domain.tls.conf}"

if [[ -z "$NODE_SSH" ]]; then
  echo "[deploy-public-stack] set NODE_SSH (e.g. hackme-vps)" >&2
  exit 1
fi

for x in ssh rsync scp curl; do
  command -v "$x" >/dev/null || {
    echo "[deploy-public-stack] missing: $x" >&2
    exit 1
  }
done

if [[ "$SKIP_NGINX" != "1" ]]; then
  if [[ ! -f "$NGINX_SITE_CONF" ]]; then
    echo "[deploy-public-stack] nginx conf not found: $NGINX_SITE_CONF" >&2
    exit 2
  fi
  echo "[deploy-public-stack] install nginx vhost from $NGINX_SITE_CONF"
  deploy_ssh_retry_run _deploy_scp "$NGINX_SITE_CONF" "${NODE_SSH}:/tmp/hackme-site-domain.conf.new"
  deploy_ssh_retry_run _deploy_ssh "$NODE_SSH" "sudo cp /tmp/hackme-site-domain.conf.new /etc/nginx/sites-available/hackme-site-domain.conf && sudo nginx -t && sudo systemctl reload nginx"
fi

if [[ "$SKIP_NODE" != "1" ]]; then
  NODE_SSH="$NODE_SSH" NODE_DEPLOY_DIR="$NODE_DEPLOY_DIR" SYNC_DIST="$SYNC_DIST" \
    HACKME_DEPLOY_SSH_IDENTITY="${HACKME_DEPLOY_SSH_IDENTITY:-}" \
    bash "$ROOT_DIR/scripts/ops/deploy_hackme_node.sh"
fi

if [[ "$SKIP_SITE" != "1" ]]; then
  NODE_SSH="$NODE_SSH" NODE_DEPLOY_DIR="$NODE_DEPLOY_DIR" \
    HACKME_DEPLOY_SSH_IDENTITY="${HACKME_DEPLOY_SSH_IDENTITY:-}" \
    bash "$ROOT_DIR/scripts/ops/deploy_hackme_site.sh"
elif [[ "$SKIP_NODE" != "1" ]]; then
  echo "[deploy-public-stack] nginx reload after node rsync (SKIP_SITE=1)"
  deploy_ssh_retry_run _deploy_ssh "$NODE_SSH" "sudo nginx -t && sudo systemctl reload nginx" || {
    echo "[deploy-public-stack] WARN: nginx reload failed" >&2
  }
fi

code="$(curl -fsS --max-time 15 -o /dev/null -w "%{http_code}" "https://hackme.tech/" || true)"
if [[ "$code" == "200" ]]; then
  echo "[deploy-public-stack] smoke home -> HTTP ${code}"
else
  echo "[deploy-public-stack] WARN: home HTTP ${code:-err}" >&2
fi

echo "[deploy-public-stack] done"
