#!/usr/bin/env bash
set -euo pipefail

# Configure public read-only explorer on a dedicated Nginx vhost.
#
# Usage (on VPS):
#   EXPLORER_HOST=explorer.example.com \
#   UPSTREAM=127.0.0.1:18080 \
#   bash scripts/ops/explorer_subdomain_up.sh
#
# Optional:
#   ENABLE_TLS=1 EMAIL=ops@example.com
#
# Notes:
# - This vhost exposes only /explorer and readonly explorer API subset.
# - All other /api/* routes are blocked (403) to avoid exposing admin surface.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TEMPLATE="${ROOT_DIR}/scripts/ops/nginx/hackme-explorer.conf.template"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[explorer-up] missing command: $1" >&2
    exit 1
  }
}

require_cmd sudo
require_cmd nginx
require_cmd sed
require_cmd curl

EXPLORER_HOST="${EXPLORER_HOST:-}"
UPSTREAM="${UPSTREAM:-127.0.0.1:18080}"
ENABLE_TLS="${ENABLE_TLS:-0}"
EMAIL="${EMAIL:-}"

if [[ -z "${EXPLORER_HOST}" ]]; then
  echo "[explorer-up] EXPLORER_HOST is required (example: explorer.example.com)" >&2
  exit 2
fi
if [[ ! -f "${TEMPLATE}" ]]; then
  echo "[explorer-up] template not found: ${TEMPLATE}" >&2
  exit 2
fi

target_avail="/etc/nginx/sites-available/hackme-explorer.conf"
target_enabled="/etc/nginx/sites-enabled/hackme-explorer.conf"
tmp_conf="$(mktemp)"

sed \
  -e "s/__EXPLORER_HOST__/${EXPLORER_HOST}/g" \
  -e "s/__UPSTREAM__/${UPSTREAM}/g" \
  "${TEMPLATE}" > "${tmp_conf}"

echo "[explorer-up] installing nginx vhost for ${EXPLORER_HOST} -> ${UPSTREAM}"
sudo cp "${tmp_conf}" "${target_avail}"
sudo ln -sf "${target_avail}" "${target_enabled}"
rm -f "${tmp_conf}"

echo "[explorer-up] nginx -t"
sudo nginx -t
echo "[explorer-up] reload nginx"
sudo systemctl reload nginx

if [[ "${ENABLE_TLS}" == "1" ]]; then
  require_cmd certbot
  if [[ -z "${EMAIL}" ]]; then
    echo "[explorer-up] ENABLE_TLS=1 requires EMAIL=you@example.com" >&2
    exit 2
  fi
  echo "[explorer-up] requesting TLS certificate for ${EXPLORER_HOST}"
  sudo certbot --nginx -d "${EXPLORER_HOST}" --non-interactive --agree-tos -m "${EMAIL}" --redirect
fi

proto="http"
if [[ "${ENABLE_TLS}" == "1" ]]; then
  proto="https"
fi
url="${proto}://${EXPLORER_HOST}/explorer"

echo "[explorer-up] smoke check: ${url}"
if curl -fsS "${url}" | grep -qi "HackMe Explorer"; then
  echo "[explorer-up] PASS: explorer is live at ${url}"
else
  echo "[explorer-up] WARN: explorer responded but signature text not found" >&2
  exit 1
fi
