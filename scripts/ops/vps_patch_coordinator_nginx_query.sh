#!/usr/bin/env bash
# Fix nginx dropping ?details=1 on /pool/coordinator/api/* (workers{} missing on HTTPS).
set -euo pipefail
CONF="${1:-/etc/nginx/sites-available/hackme-site-domain.conf}"
if [[ ! -f "$CONF" ]]; then
  echo "config not found: $CONF" >&2
  exit 1
fi
if grep -q '18081/api/\$1\$is_args\$args' "$CONF"; then
  echo "[nginx] coordinator query pass-through already patched in $CONF"
else
  sed -i 's|proxy_pass http://127.0.0.1:18081/api/\$1;|proxy_pass http://127.0.0.1:18081/api/$1$is_args$args;|g' "$CONF"
  echo "[nginx] patched coordinator proxy_pass in $CONF"
fi
nginx -t
systemctl reload nginx
echo "[nginx] reloaded"
curl -fsS "https://hackme.tech/pool/coordinator/api/work/stats?details=1" | jq 'has("workers"), (.workers|keys|length)'
