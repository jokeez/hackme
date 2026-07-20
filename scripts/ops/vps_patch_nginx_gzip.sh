#!/usr/bin/env bash
# Enable gzip for JSON/JS/CSS on nginx (news.json CDN timeouts without compression).
set -euo pipefail
NODE_SSH="${NODE_SSH:-hackme-vps}"
ssh "$NODE_SSH" 'python3 - <<'"'"'PY'"'"'
from pathlib import Path
p = Path("/etc/nginx/nginx.conf")
text = p.read_text()
repl = {
    "# gzip on;": "gzip on;",
    "# gzip_vary on;": "gzip_vary on;",
    "# gzip_proxied any;": "gzip_proxied any;",
    "# gzip_comp_level 6;": "gzip_comp_level 6;",
    "# gzip_buffers 16 8k;": "gzip_buffers 16 8k;",
    "# gzip_http_version 1.1;": "gzip_http_version 1.1;",
}
for a, b in repl.items():
    text = text.replace(a, b)
old = "# gzip_types text/plain text/css application/json application/javascript text/xml application/xml application/xml+rss text/javascript;"
new = "gzip_types text/plain text/css application/json application/javascript text/xml application/xml application/xml+rss text/javascript;"
text = text.replace(old, new)
p.write_text(text)
print("[gzip] nginx.conf patched")
PY
sudo nginx -t && sudo systemctl reload nginx'
echo "[vps_patch_nginx_gzip] PASS"
