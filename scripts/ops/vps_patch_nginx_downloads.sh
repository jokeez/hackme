#!/usr/bin/env bash
# Fix slow / stalled large downloads via Cloudflare:
#   - cache-friendly /dist/ headers (edge cache instead of broken origin stream)
#   - optional HTTP/1.1-only for hackme.tech (workaround for CF↔origin HTTP/2 stalls)
#   - origin direct vhost on 132.243.112.100 for dl.hackme.tech (DNS grey-cloud required)
#
# Run on VPS as root:
#   ssh hackme-vps 'sudo bash -s' < scripts/ops/vps_patch_nginx_downloads.sh
# Or from repo:
#   NODE_SSH=hackme-vps bash scripts/ops/vps_patch_nginx_downloads.sh
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SITE_CONF="${NGINX_SITE_CONF:-/etc/nginx/sites-available/hackme-site-domain.conf}"
SNIPPET_SRC="${ROOT_DIR}/scripts/ops/nginx/hackme-dist-downloads.snippet.conf"
ORIGIN_CONF="/etc/nginx/sites-available/hackme-dl-origin.conf"
DISABLE_HTTP2="${DISABLE_HTTP2:-1}"
ORIGIN_IP="${ORIGIN_IP:-132.243.112.100}"

if [[ "$(id -u)" -ne 0 ]]; then
  echo "[dl-patch] run as root (sudo)" >&2
  exit 1
fi

if [[ ! -f "$SITE_CONF" ]]; then
  echo "[dl-patch] missing $SITE_CONF" >&2
  exit 1
fi

cp -a "$SITE_CONF" "${SITE_CONF}.bak.$(date +%Y%m%dT%H%M%S)"

# Inject snippet into location /dist/ if not present.
if ! grep -q 'hackme-dist-downloads.snippet' "$SITE_CONF"; then
  python3 - <<'PY' "$SITE_CONF" "$SNIPPET_SRC"
import pathlib, sys
site, snippet = pathlib.Path(sys.argv[1]), pathlib.Path(sys.argv[2]).read_text()
text = site.read_text()
needle = "    location /dist/ {\n        alias /opt/hackme/dist/;\n"
if needle not in text:
    raise SystemExit("location /dist/ block not found — patch manually")
insert = needle + "        include /etc/nginx/snippets/hackme-dist-downloads.conf;\n"
text = text.replace(needle, insert, 1)
site.write_text(text)
print("[dl-patch] included snippet in location /dist/")
PY
fi

mkdir -p /etc/nginx/snippets
cp -f "$SNIPPET_SRC" /etc/nginx/snippets/hackme-dist-downloads.conf

if [[ "$DISABLE_HTTP2" == "1" ]] && grep -q 'http2 on;' "$SITE_CONF"; then
  sed -i 's/^[[:space:]]*http2 on;/    # http2 off — CF large download workaround\n    # http2 on;/' "$SITE_CONF"
  echo "[dl-patch] disabled http2 on hackme.tech TLS vhost"
fi

# Direct origin vhost for dl.hackme.tech (Cloudflare DNS-only / grey cloud).
cat >"$ORIGIN_CONF" <<EOF
# Direct download mirror — set dl.hackme.tech A ${ORIGIN_IP} with proxy OFF in Cloudflare.
server {
    listen 443 ssl;
    listen [::]:443 ssl;
    server_name dl.hackme.tech origin.hackme.tech;

    ssl_certificate /etc/ssl/certs/hackme-origin.crt;
    ssl_certificate_key /etc/ssl/private/hackme-origin.key;
    ssl_protocols TLSv1.2 TLSv1.3;

    root /opt/hackme/web/site;
    index index.html;

    location /dist/ {
        alias /opt/hackme/dist/;
        include /etc/nginx/snippets/hackme-dist-downloads.conf;
    }

    location / {
        return 302 https://hackme.tech/downloads.html;
    }
}
EOF

ln -sf "$ORIGIN_CONF" /etc/nginx/sites-enabled/hackme-dl-origin.conf

nginx -t
systemctl reload nginx
echo "[dl-patch] OK nginx reloaded"
echo "[dl-patch] Next: Cloudflare DNS → dl.hackme.tech A ${ORIGIN_IP} (grey cloud / DNS only)"
echo "[dl-patch] Test: curl -I https://dl.hackme.tech/dist/release_0.1.0-rc11r/SHA256SUMS.txt"
