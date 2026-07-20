#!/usr/bin/env bash
# Operator-only. Prefer DRY_RUN=1 when the script supports it. Confirm remote target before run.
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
#   SYNC_NGINX_SITE_CONF=1 — install scripts/ops/nginx/hackme-site-domain.tls.conf (wallet + pool routes)

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"
_OPS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "$_OPS_DIR/_deploy_ssh_retry.sh"

# Optional: HACKME_DEPLOY_SSH_IDENTITY=/path/to/ed25519 (chmod 600, never commit)
_deploy_ssh() {
  if [[ -n "${HACKME_DEPLOY_SSH_IDENTITY:-}" && -f "${HACKME_DEPLOY_SSH_IDENTITY}" ]]; then
    ssh -i "${HACKME_DEPLOY_SSH_IDENTITY}" -o BatchMode=yes -o StrictHostKeyChecking=accept-new "$@"
  else
    ssh "$@"
  fi
}
_deploy_rsync() {
  if [[ -n "${HACKME_DEPLOY_SSH_IDENTITY:-}" && -f "${HACKME_DEPLOY_SSH_IDENTITY}" ]]; then
    rsync -e "ssh -i ${HACKME_DEPLOY_SSH_IDENTITY} -o BatchMode=yes -o StrictHostKeyChecking=accept-new" "$@"
  else
    rsync "$@"
  fi
}

NODE_SSH="${NODE_SSH:-}"
NODE_DEPLOY_DIR="${NODE_DEPLOY_DIR:-/opt/hackme}"
SKIP_DIST="${SKIP_DIST:-0}"
RELOAD_NGINX="${RELOAD_NGINX:-1}"
SYNC_NGINX_SITE_CONF="${SYNC_NGINX_SITE_CONF:-0}"
NGINX_SITE_CONF="${NGINX_SITE_CONF:-$ROOT_DIR/scripts/ops/nginx/hackme-site-domain.tls.conf}"

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
  echo "[deploy-hackme-site] build news-feed.json + news-display.json"
  python3 - <<'PY' "$ROOT_DIR/web/site/assets/news.json" "$ROOT_DIR/web/site/assets/news-feed.json" "$ROOT_DIR/web/site/assets/news-display.json"
import json, sys
src, feed_dst, display_dst = sys.argv[1], sys.argv[2], sys.argv[3]
data = json.load(open(src, encoding="utf-8"))
items = data.get("items", [])
json.dump({"items": items[:12], "feed": "recent"}, open(feed_dst, "w", encoding="utf-8"), indent=2, ensure_ascii=False)
keys = ("id", "date", "title", "summary", "impact", "action", "tags", "status")
slim = [{k: it[k] for k in keys if k in it} for it in items]
json.dump({"items": slim, "feed": "display"}, open(display_dst, "w", encoding="utf-8"), indent=2, ensure_ascii=False)
print(f"[deploy-hackme-site] news-feed items={len(items[:12])} news-display items={len(slim)}")
PY
fi

echo "[deploy-hackme-site] rsync web/site -> ${NODE_SSH}:${NODE_DEPLOY_DIR}/web/site/"
deploy_ssh_retry_run _deploy_rsync -az --delete --mkpath \
  "${ROOT_DIR}/web/site/" "${NODE_SSH}:${NODE_DEPLOY_DIR}/web/site/"

if [[ "$SKIP_DIST" != "1" && -d "${ROOT_DIR}/dist" ]]; then
  echo "[deploy-hackme-site] rsync dist/ -> ${NODE_SSH}:${NODE_DEPLOY_DIR}/dist/"
  deploy_ssh_retry_run _deploy_rsync -az --mkpath "${ROOT_DIR}/dist/" "${NODE_SSH}:${NODE_DEPLOY_DIR}/dist/"
fi

if [[ "$SYNC_NGINX_SITE_CONF" == "1" ]]; then
  if [[ ! -f "$NGINX_SITE_CONF" ]]; then
    echo "[deploy-hackme-site] missing NGINX_SITE_CONF: $NGINX_SITE_CONF" >&2
    exit 2
  fi
  echo "[deploy-hackme-site] sync nginx site conf -> /etc/nginx/sites-available/hackme-site-domain.conf"
  deploy_ssh_retry_run _deploy_rsync -az "$NGINX_SITE_CONF" "${NODE_SSH}:/tmp/hackme-site-domain.conf.new"
  deploy_ssh_retry_run _deploy_ssh "$NODE_SSH" \
    "sudo cp /tmp/hackme-site-domain.conf.new /etc/nginx/sites-available/hackme-site-domain.conf"
fi

if [[ "$RELOAD_NGINX" == "1" ]]; then
  echo "[deploy-hackme-site] nginx reload (best-effort)"
  deploy_ssh_retry_run _deploy_ssh "$NODE_SSH" "sudo nginx -t && sudo systemctl reload nginx" || {
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

if command -v jq >/dev/null 2>&1; then
  wcode="$(curl -fsS --max-time 15 -o /dev/null -w "%{http_code}" "https://hackme.tech/api/wallet" || true)"
  if [[ "$wcode" == "200" ]]; then
    echo "[deploy-hackme-site] GET /api/wallet HTTP 200"
  else
    echo "[deploy-hackme-site] WARN: GET /api/wallet HTTP ${wcode:-error} (run with SYNC_NGINX_SITE_CONF=1 if 403)" >&2
  fi
fi
