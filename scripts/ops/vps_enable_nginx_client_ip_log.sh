#!/usr/bin/env bash
# Add a dedicated nginx access log with Cloudflare client IP (CF-Connecting-IP).
#
# Without this, /var/log/nginx/access.log only shows Cloudflare edge IPs.
# Run on VPS as root:
#   bash scripts/ops/vps_enable_nginx_client_ip_log.sh
#
# Then use:
#   NGINX_CLIENT_LOG=/var/log/nginx/hackme-site-clients.log \
#     bash scripts/ops/nginx_downloads_interest.sh report --minutes 60
#
set -euo pipefail

SNIPPET_AVAILABLE="/etc/nginx/conf.d/hackme-log-format.conf"
SNIPPET_SITE="/etc/nginx/snippets/hackme-client-log.conf"
LOG_PATH="/var/log/nginx/hackme-site-clients.log"
SITE_CONF="/etc/nginx/sites-available/hackme-site-domain.conf"

require_root() {
  if [[ "$(id -u)" -ne 0 ]]; then
    echo "[client-ip-log] run as root on VPS" >&2
    exit 1
  fi
}

require_root

cat >"$SNIPPET_AVAILABLE" <<'EOF'
# HackMe: log real visitor IP when behind Cloudflare (CF-Connecting-IP).
log_format hackme_client '$remote_addr - $remote_user [$time_local] '
  '"$request" $status $body_bytes_sent "$http_referer" "$http_user_agent" '
  'cf_ip=$http_cf_connecting_ip xff=$http_x_forwarded_for';
EOF

cat >"$SNIPPET_SITE" <<EOF
    access_log ${LOG_PATH} hackme_client;
EOF

if [[ ! -f "$SITE_CONF" ]]; then
  echo "[client-ip-log] missing $SITE_CONF" >&2
  exit 1
fi

if grep -q 'hackme-site-clients.log' "$SITE_CONF"; then
  echo "[client-ip-log] already configured in $SITE_CONF"
else
  # Insert after first server_name in TLS server block (443).
  awk -v snip="    include ${SNIPPET_SITE};" '
    /listen 443/ { in_tls=1 }
    in_tls && /server_name/ && !done {
      print
      print snip
      done=1
      next
    }
    { print }
  ' "$SITE_CONF" >"${SITE_CONF}.tmp"
  mv "${SITE_CONF}.tmp" "$SITE_CONF"
  echo "[client-ip-log] patched $SITE_CONF"
fi

touch "$LOG_PATH"
chown www-data:adm "$LOG_PATH"
chmod 640 "$LOG_PATH"

nginx -t
systemctl reload nginx

echo "[client-ip-log] OK — tail: tail -F ${LOG_PATH}"
echo "[client-ip-log] parser: bash scripts/ops/nginx_downloads_interest.sh live"
