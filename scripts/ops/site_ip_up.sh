#!/usr/bin/env bash
set -euo pipefail

# Configure public website by bare IP (no domain / no TLS yet).
#
# Usage on VPS:
#   SERVER_NAME=132.243.112.100 \
#   UPSTREAM=127.0.0.1:18080 \
#   bash scripts/ops/site_ip_up.sh

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TEMPLATE="${ROOT_DIR}/scripts/ops/nginx/hackme-site-ip.conf.template"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[site-ip-up] missing command: $1" >&2
    exit 1
  }
}

require_cmd sudo
require_cmd nginx
require_cmd sed
require_cmd curl
require_cmd rsync

SERVER_NAME="${SERVER_NAME:-}"
UPSTREAM="${UPSTREAM:-127.0.0.1:18080}"

if [[ -z "${SERVER_NAME}" ]]; then
  echo "[site-ip-up] SERVER_NAME is required (for now use VPS public IP)" >&2
  exit 2
fi
if [[ ! -f "${TEMPLATE}" ]]; then
  echo "[site-ip-up] template not found: ${TEMPLATE}" >&2
  exit 2
fi
if [[ ! -d "${ROOT_DIR}/web/site" ]]; then
  echo "[site-ip-up] missing web/site directory; run from repo root with latest changes" >&2
  exit 2
fi

echo "[site-ip-up] syncing site assets into /opt/hackme"
sudo mkdir -p /opt/hackme/web/site /opt/hackme/dist
sudo rsync -a --delete "${ROOT_DIR}/web/site/" /opt/hackme/web/site/
if [[ -d "${ROOT_DIR}/dist" ]]; then
  sudo rsync -a "${ROOT_DIR}/dist/" /opt/hackme/dist/
fi
sudo chown -R root:root /opt/hackme/web/site /opt/hackme/dist

target_avail="/etc/nginx/sites-available/hackme-site-ip.conf"
target_enabled="/etc/nginx/sites-enabled/hackme-site-ip.conf"
tmp_conf="$(mktemp)"
sed \
  -e "s/__SERVER_NAME__/${SERVER_NAME}/g" \
  -e "s/__UPSTREAM__/${UPSTREAM}/g" \
  "${TEMPLATE}" > "${tmp_conf}"

echo "[site-ip-up] installing nginx vhost ${target_avail}"
sudo cp "${tmp_conf}" "${target_avail}"
sudo ln -sf "${target_avail}" "${target_enabled}"
rm -f "${tmp_conf}"

echo "[site-ip-up] nginx -t"
sudo nginx -t
echo "[site-ip-up] reloading nginx"
sudo systemctl reload nginx

site_url="http://${SERVER_NAME}/"
explorer_url="http://${SERVER_NAME}/explorer"
echo "[site-ip-up] smoke check: ${site_url}"
curl -fsS "${site_url}" | grep -qi "HackMe Network"
echo "[site-ip-up] smoke check: ${explorer_url}"
curl -fsS "${explorer_url}" | grep -qi "HackMe Explorer"

echo "[site-ip-up] PASS: site is live"
echo "  site: ${site_url}"
echo "  explorer: ${explorer_url}"
