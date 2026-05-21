#!/usr/bin/env bash
# Sync public landing (web/site) + release bundles (dist/) to VPS nginx root.
#
# Remote layout matches scripts/ops/site_ip_up.sh:
#   /opt/hackme/web/site   — static HTML/CSS/JS
#   /opt/hackme/dist       — release_* artifacts for downloads
#
# Usage (from dev machine with SSH key):
#   NODE_SSH=hackme-vps NODE_DEPLOY_DIR=/opt/hackme bash scripts/ops/deploy_hackme_site.sh
#
# Optional:
#   SKIP_DIST=1       — do not rsync dist/ (large); site-only refresh
#   RELOAD_NGINX=1    — default on; set RELOAD_NGINX=0 to skip systemctl reload

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"
_OPS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "$_OPS_DIR/_deploy_ssh_retry.sh"

NODE_SSH="${NODE_SSH:-}"
NODE_DEPLOY_DIR="${NODE_DEPLOY_DIR:-/opt/hackme}"
SKIP_DIST="${SKIP_DIST:-0}"
RELOAD_NGINX="${RELOAD_NGINX:-1}"

if [[ -z "$NODE_SSH" ]]; then
  echo "[deploy-hackme-site] set NODE_SSH (e.g. hackme-vps or root@host)" >&2
  exit 1
fi

for x in ssh rsync curl; do
  command -v "$x" >/dev/null || {
    echo "[deploy-hackme-site] missing: $x" >&2
    exit 1
  }
done

if [[ ! -d "$ROOT_DIR/web/site" ]]; then
  echo "[deploy-hackme-site] missing web/site" >&2
  exit 2
fi

if [[ -f "$ROOT_DIR/web/site/assets/news.json" ]]; then
  echo "[deploy-hackme-site] build news-feed.json (recent items for Telegram/pollers)"
  python3 - <<'PY' "$ROOT_DIR/web/site/assets/news.json" "$ROOT_DIR/web/site/assets/news-feed.json"
import json, sys
src, dst = sys.argv[1], sys.argv[2]
data = json.load(open(src, encoding="utf-8"))
items = data.get("items", [])[:12]
json.dump({"items": items, "feed": "recent"}, open(dst, "w", encoding="utf-8"), indent=2, ensure_ascii=False)
print(f"[deploy-hackme-site] news-feed items={len(items)}")
PY
fi

echo "[deploy-hackme-site] rsync web/site -> ${NODE_SSH}:${NODE_DEPLOY_DIR}/web/site/"
deploy_ssh_retry_run rsync -az --delete --mkpath \
  "${ROOT_DIR}/web/site/" "${NODE_SSH}:${NODE_DEPLOY_DIR}/web/site/"

if [[ "$SKIP_DIST" != "1" && -d "${ROOT_DIR}/dist" ]]; then
  echo "[deploy-hackme-site] rsync dist/ -> ${NODE_SSH}:${NODE_DEPLOY_DIR}/dist/"
  deploy_ssh_retry_run rsync -az --mkpath "${ROOT_DIR}/dist/" "${NODE_SSH}:${NODE_DEPLOY_DIR}/dist/"
fi

if [[ "$RELOAD_NGINX" == "1" ]]; then
  echo "[deploy-hackme-site] nginx reload (best-effort)"
  deploy_ssh_retry_run ssh "$NODE_SSH" "sudo nginx -t && sudo systemctl reload nginx" || {
    echo "[deploy-hackme-site] WARN: nginx reload failed (check sudo/nginx on host)" >&2
  }
fi

echo "[deploy-hackme-site] smoke https://hackme.tech/"
code="$(curl -fsS --max-time 15 -o /dev/null -w "%{http_code}" "https://hackme.tech/" || true)"
if [[ "$code" == "200" ]]; then
  echo "[deploy-hackme-site] PASS (HTTP ${code})"
else
  echo "[deploy-hackme-site] WARN: unexpected HTTP ${code:-error}" >&2
fi
