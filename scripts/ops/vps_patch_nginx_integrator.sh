#!/usr/bin/env bash
# Ensure /api/integrator is proxied on hackme.tech (not caught by location /api/ { return 403; }).
# Run on VPS: sudo bash scripts/ops/vps_patch_nginx_integrator.sh
# Or from dev: ssh hackme-vps 'sudo bash -s' < scripts/ops/vps_patch_nginx_integrator.sh
set -euo pipefail
CONF="${1:-/etc/nginx/sites-available/hackme-site-domain.conf}"
if [[ ! -f "$CONF" ]]; then
  echo "[nginx-integrator] missing $CONF" >&2
  exit 1
fi
if grep -q 'api/integrator' "$CONF"; then
  echo "[nginx-integrator] already present in $CONF"
else
  BLOCK='    location ~ ^/api/integrator(/.*)?$ {
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        client_max_body_size 64k;
        proxy_pass http://127.0.0.1:18080;
    }

'
  # Insert before "location ~ ^/api/tasks" or before "location /api/ { return 403"
  if grep -q 'location ~ \^/api/tasks' "$CONF"; then
    sed -i '/location ~ \^\/api\/tasks/i\'"$BLOCK" "$CONF" 2>/dev/null || \
    python3 - "$CONF" "$BLOCK" <<'PY'
import sys
path, block = sys.argv[1], sys.argv[2]
text = open(path, encoding="utf-8").read()
needle = "    location ~ ^/api/tasks"
if needle not in text:
    sys.exit("needle not found")
open(path, "w", encoding="utf-8").write(text.replace(needle, block + needle, 1))
print("[nginx-integrator] inserted before api/tasks")
PY
  else
    echo "[nginx-integrator] WARN: could not auto-insert; deploy full hackme-site-domain.tls.conf" >&2
    exit 2
  fi
  echo "[nginx-integrator] patched $CONF"
fi
nginx -t
systemctl reload nginx
echo "[nginx-integrator] reload ok"
