#!/usr/bin/env bash
# Raise nginx worker_connections (fixes "768 worker_connections are not enough" under pool load).
# Run on VPS as root:
#   ssh hackme-vps 'sudo bash -s' < scripts/ops/vps_patch_nginx_connections.sh
set -euo pipefail

CONF="${NGINX_MAIN_CONF:-/etc/nginx/nginx.conf}"
if [[ ! -f "$CONF" ]]; then
  echo "[nginx-conn] missing $CONF" >&2
  exit 1
fi

cp -a "$CONF" "${CONF}.bak.$(date +%Y%m%dT%H%M%S)"

TARGET="${NGINX_WORKER_CONNECTIONS:-4096}"
if grep -qE '^\s*worker_connections\s+' "$CONF"; then
  sed -i -E "s/^\s*worker_connections\s+[0-9]+;/    worker_connections ${TARGET};/" "$CONF"
else
  sed -i "/events\s*{/a\\    worker_connections ${TARGET};" "$CONF"
fi

# Optional: raise file descriptor limit for nginx master
LIMITS_DROPIN=/etc/systemd/system/nginx.service.d/limits.conf
mkdir -p "$(dirname "$LIMITS_DROPIN")"
cat >"$LIMITS_DROPIN" <<EOF
[Service]
LimitNOFILE=65535
EOF

nginx -t
systemctl daemon-reload
systemctl reload nginx
echo "[nginx-conn] OK worker_connections=${TARGET} (nginx reloaded)"
