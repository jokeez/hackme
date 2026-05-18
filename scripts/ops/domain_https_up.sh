#!/usr/bin/env bash
set -euo pipefail

# Configure domain-based nginx vhosts + HTTPS certbot for HackMe site and explorer.
#
# Usage on VPS:
#   SITE_DOMAIN=hackme.tech SITE_WWW_DOMAIN=www.hackme.tech EXPLORER_DOMAIN=explorer.hackme.tech \
#   EMAIL=you@example.com UPSTREAM=127.0.0.1:18080 \
#   bash scripts/ops/domain_https_up.sh

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SITE_TPL="${ROOT_DIR}/scripts/ops/nginx/hackme-site-domain.conf.template"
EXP_TPL="${ROOT_DIR}/scripts/ops/nginx/hackme-explorer-domain.conf.template"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[domain-up] missing command: $1" >&2
    exit 1
  }
}

require_cmd sudo
require_cmd sed
require_cmd nginx
require_cmd curl

SITE_DOMAIN="${SITE_DOMAIN:-}"
SITE_WWW_DOMAIN="${SITE_WWW_DOMAIN:-}"
EXPLORER_DOMAIN="${EXPLORER_DOMAIN:-}"
UPSTREAM="${UPSTREAM:-127.0.0.1:18080}"
EMAIL="${EMAIL:-}"
ENABLE_CERTBOT="${ENABLE_CERTBOT:-1}"

if [[ -z "$SITE_DOMAIN" || -z "$SITE_WWW_DOMAIN" || -z "$EXPLORER_DOMAIN" ]]; then
  echo "[domain-up] SITE_DOMAIN, SITE_WWW_DOMAIN, EXPLORER_DOMAIN are required" >&2
  exit 2
fi

if [[ ! -f "$SITE_TPL" || ! -f "$EXP_TPL" ]]; then
  echo "[domain-up] nginx templates not found" >&2
  exit 2
fi

if [[ "$ENABLE_CERTBOT" == "1" ]]; then
  require_cmd certbot
  if [[ -z "$EMAIL" ]]; then
    echo "[domain-up] EMAIL required when ENABLE_CERTBOT=1" >&2
    exit 2
  fi
fi

tmp_site="$(mktemp)"
tmp_exp="$(mktemp)"

sed -e "s/__SITE_DOMAIN__/${SITE_DOMAIN}/g" \
    -e "s/__SITE_WWW_DOMAIN__/${SITE_WWW_DOMAIN}/g" \
    "$SITE_TPL" > "$tmp_site"

sed -e "s/__EXPLORER_DOMAIN__/${EXPLORER_DOMAIN}/g" \
    -e "s#__UPSTREAM__#${UPSTREAM}#g" \
    "$EXP_TPL" > "$tmp_exp"

echo "[domain-up] install vhosts"
sudo cp "$tmp_site" /etc/nginx/sites-available/hackme-site-domain.conf
sudo cp "$tmp_exp" /etc/nginx/sites-available/hackme-explorer-domain.conf
sudo ln -sf /etc/nginx/sites-available/hackme-site-domain.conf /etc/nginx/sites-enabled/hackme-site-domain.conf
sudo ln -sf /etc/nginx/sites-available/hackme-explorer-domain.conf /etc/nginx/sites-enabled/hackme-explorer-domain.conf

# disable old IP-only vhost to avoid overlap once domain vhosts are active.
sudo rm -f /etc/nginx/sites-enabled/hackme-site-ip.conf || true

rm -f "$tmp_site" "$tmp_exp"

echo "[domain-up] nginx -t"
sudo nginx -t
sudo systemctl reload nginx

if [[ "$ENABLE_CERTBOT" == "1" ]]; then
  echo "[domain-up] certbot issue/renew for ${SITE_DOMAIN}, ${SITE_WWW_DOMAIN}, ${EXPLORER_DOMAIN}"
  sudo certbot --nginx --non-interactive --agree-tos -m "$EMAIL" \
    -d "$SITE_DOMAIN" -d "$SITE_WWW_DOMAIN" -d "$EXPLORER_DOMAIN" --redirect
fi

echo "[domain-up] smoke checks"
curl -fsS "http://${SITE_DOMAIN}/" >/dev/null
curl -fsS "http://${EXPLORER_DOMAIN}/explorer" >/dev/null
if [[ "$ENABLE_CERTBOT" == "1" ]]; then
  curl -fsS "https://${SITE_DOMAIN}/" >/dev/null
  curl -fsS "https://${EXPLORER_DOMAIN}/explorer" >/dev/null
fi

echo "[domain-up] PASS"
echo "  site: https://${SITE_DOMAIN}/"
echo "  explorer: https://${EXPLORER_DOMAIN}/explorer"
