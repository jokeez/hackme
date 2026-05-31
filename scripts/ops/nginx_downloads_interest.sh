#!/usr/bin/env bash
# HackMe — nginx interest tracker for downloads.html + HackMe OS ISO (956 MB).
#
# Run on VPS hackme-vps (or any host with nginx access logs):
#
#   # Real-time dashboard (last 60 min, refreshes every 2s)
#   bash scripts/ops/nginx_downloads_interest.sh live
#
#   # Report: today / last hour / full filtered scan
#   bash scripts/ops/nginx_downloads_interest.sh report --since 2026-05-22
#   bash scripts/ops/nginx_downloads_interest.sh report --minutes 60
#   bash scripts/ops/nginx_downloads_interest.sh report --minutes 1440
#
#   # After enabling real client IP log (recommended behind Cloudflare):
#   bash scripts/ops/vps_enable_nginx_client_ip_log.sh
#   bash scripts/ops/nginx_downloads_interest.sh report --minutes 60 --client-log
#
# Env:
#   NGINX_ACCESS_LOG   default /var/log/nginx/access.log
#   NGINX_CLIENT_LOG   default /var/log/nginx/hackme-site-clients.log (if exists)
#   HACKME_ISO_SUBPATH default HackMe-OS-0.1.0-rc11i-amd64.iso
#
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PY="${ROOT_DIR}/scripts/ops/nginx_downloads_interest.py"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[downloads-interest] missing: $1" >&2
    exit 1
  }
}

require_cmd python3

NGINX_ACCESS_LOG="${NGINX_ACCESS_LOG:-/var/log/nginx/access.log}"
NGINX_CLIENT_LOG="${NGINX_CLIENT_LOG:-/var/log/nginx/hackme-site-clients.log}"
HACKME_ISO_SUBPATH="${HACKME_ISO_SUBPATH:-HackMe-OS-0.1.0-rc11i-amd64.iso}"

CLIENT_ARG=()
if [[ -f "$NGINX_CLIENT_LOG" ]]; then
  CLIENT_ARG=(--client-log "$NGINX_CLIENT_LOG")
  export NGINX_ACCESS_LOG="$NGINX_CLIENT_LOG"
fi

CMD="${1:-live}"
shift || true

case "$CMD" in
  live|report|tail)
    exec python3 "$PY" "$CMD" --log "$NGINX_ACCESS_LOG" --iso-subpath "$HACKME_ISO_SUBPATH" "${CLIENT_ARG[@]}" "$@"
    ;;
  help|-h|--help)
    sed -n '2,22p' "$0"
    exit 0
    ;;
  *)
    echo "usage: $0 {live|report|tail} [python args...]" >&2
    exit 2
    ;;
esac
