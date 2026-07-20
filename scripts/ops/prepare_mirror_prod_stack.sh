#!/usr/bin/env bash
# Prepare mirror host for production cutover (node/coordinator/nginx stack).
# Does NOT switch DNS and does NOT enable continuous traffic.
#
# Usage:
#   MIRROR_SSH=hackme-mirror bash scripts/ops/prepare_mirror_prod_stack.sh
set -euo pipefail

NODE_SSH="${NODE_SSH:-hackme-vps}"
MIRROR_SSH="${MIRROR_SSH:-}"
DEPLOY="${NODE_DEPLOY_DIR:-/opt/hackme}"
LOG_TAG="[mirror-prod-prepare $(date -u +%Y-%m-%dT%H:%M:%SZ)]"

if [[ -z "$MIRROR_SSH" ]]; then
  echo "$LOG_TAG ERROR: set MIRROR_SSH" >&2
  exit 1
fi

echo "$LOG_TAG install nginx on mirror"
ssh -o BatchMode=yes "$MIRROR_SSH" \
  "sudo apt-get update -y >/dev/null && sudo DEBIAN_FRONTEND=noninteractive apt-get install -y nginx >/dev/null"

echo "$LOG_TAG sync nginx site configs from hub"
for f in hackme-site-domain.conf hackme-explorer-domain.conf hackme-pool-direct.conf hackme-dl-origin.conf; do
  if ssh -o BatchMode=yes "$NODE_SSH" "sudo test -f /etc/nginx/sites-available/${f}"; then
    ssh -o BatchMode=yes "$NODE_SSH" "sudo cat /etc/nginx/sites-available/${f}" \
      | ssh -o BatchMode=yes "$MIRROR_SSH" "sudo tee /etc/nginx/sites-available/${f} >/dev/null"
  elif ssh -o BatchMode=yes "$NODE_SSH" "sudo test -f /etc/nginx/sites-enabled/${f}"; then
    ssh -o BatchMode=yes "$NODE_SSH" "sudo cat /etc/nginx/sites-enabled/${f}" \
      | ssh -o BatchMode=yes "$MIRROR_SSH" "sudo tee /etc/nginx/sites-available/${f} >/dev/null"
  else
    echo "$LOG_TAG WARN missing nginx config on hub: ${f}"
  fi
done

ssh -o BatchMode=yes "$MIRROR_SSH" "sudo rm -f /etc/nginx/sites-enabled/*"
ssh -o BatchMode=yes "$MIRROR_SSH" "for f in hackme-site-domain.conf hackme-explorer-domain.conf hackme-pool-direct.conf hackme-dl-origin.conf; do sudo ln -sf /etc/nginx/sites-available/\$f /etc/nginx/sites-enabled/\$f; done"

echo "$LOG_TAG sync nginx snippets/includes from hub"
ssh -o BatchMode=yes "$NODE_SSH" "sudo tar -C /etc/nginx -cf - snippets conf.d" \
  | ssh -o BatchMode=yes "$MIRROR_SSH" "sudo tar -C /etc/nginx -xf -"

# Compatibility: older distro nginx may not support standalone "http2 on;" directive.
ssh -o BatchMode=yes "$MIRROR_SSH" \
  "sudo sed -i '/^[[:space:]]*http2[[:space:]]\\+on;/d' /etc/nginx/sites-available/hackme-*.conf 2>/dev/null || true"

if ssh -o BatchMode=yes "$NODE_SSH" "sudo test -d /etc/letsencrypt/live"; then
  echo "$LOG_TAG sync letsencrypt cert material (root-only)"
  ssh -o BatchMode=yes "$NODE_SSH" "sudo tar -C /etc -cf - letsencrypt" \
    | ssh -o BatchMode=yes "$MIRROR_SSH" "sudo tar -C /etc -xf -"
fi

if ssh -o BatchMode=yes "$NODE_SSH" "sudo test -f /etc/ssl/certs/hackme-origin.crt"; then
  echo "$LOG_TAG sync origin cert/key from hub"
  ssh -o BatchMode=yes "$NODE_SSH" \
    "sudo tar -C /etc/ssl -cf - certs/hackme-origin.crt private/hackme-origin.key" \
    | ssh -o BatchMode=yes "$MIRROR_SSH" "sudo tar -C /etc/ssl -xf -"
fi

ssh -o BatchMode=yes "$MIRROR_SSH" "sudo nginx -t"
ssh -o BatchMode=yes "$MIRROR_SSH" "sudo systemctl enable nginx hackme-node hackme-coordinator >/dev/null 2>&1 || true"
ssh -o BatchMode=yes "$MIRROR_SSH" "sudo systemctl stop nginx hackme-node hackme-coordinator 2>/dev/null || true"

echo "$LOG_TAG PASS: mirror prod stack prepared (services left stopped)"
