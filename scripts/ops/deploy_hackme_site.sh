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
  echo "[deploy-hackme-site] build news-feed.json + news-display chunks"
  python3 - <<'PY' "$ROOT_DIR/web/site/assets/news.json" "$ROOT_DIR/web/site/assets/news-feed.json" "$ROOT_DIR/web/site/assets"
import json, sys
from pathlib import Path
src, feed_dst, assets = sys.argv[1], sys.argv[2], Path(sys.argv[3])
data = json.load(open(src, encoding="utf-8"))
items = data.get("items", [])
json.dump({"items": items[:12], "feed": "recent"}, open(feed_dst, "w", encoding="utf-8"), indent=2, ensure_ascii=False)
keys = ("id", "date", "title", "summary", "impact", "action", "tags", "status")

def slim_item(it):
    return {k: it[k] for k in keys if k in it}

# Preserve full newsroom archive: merge news.json into existing display (by id).
archive_path = assets / "news-display.json"
existing = []
if archive_path.exists():
    try:
        existing = json.load(open(archive_path, encoding="utf-8")).get("items", [])
    except Exception:
        existing = []
by_id = {it.get("id"): slim_item(it) for it in existing if it.get("id")}
for it in items:
    sid = it.get("id")
    if not sid:
        continue
    by_id[sid] = {**by_id.get(sid, {}), **slim_item(it)}

def sort_key(it):
    return str(it.get("date") or "")

slim = sorted(by_id.values(), key=sort_key, reverse=True)
chunk_dir = assets / "news-chunks"
chunk_dir.mkdir(parents=True, exist_ok=True)
chunk_size = 17
chunks = []
for i in range(0, len(slim), chunk_size):
    part = slim[i : i + chunk_size]
    name = f"news-display-{i // chunk_size:02d}.json"
    path = assets / name
    json.dump({"items": part, "chunk": i // chunk_size}, open(path, "w", encoding="utf-8"), indent=2, ensure_ascii=False)
    chunks.append(f"/assets/{name}")
json.dump({"items": slim, "feed": "display"}, open(archive_path, "w", encoding="utf-8"), indent=2, ensure_ascii=False)
json.dump({"chunks": chunks, "total": len(slim)}, open(assets / "news-display-index.json", "w", encoding="utf-8"), indent=2)
print(f"[deploy-hackme-site] news-feed={len(items[:12])} display={len(slim)} chunks={len(chunks)}")
PY
  echo "[deploy-hackme-site] generate news.xml RSS from news-display.json"
  python3 "$ROOT_DIR/scripts/ops/generate_news_rss.py" || true
  # Keep sitemap lastmod current on each deploy
  python3 - <<'PY' "$ROOT_DIR/web/site/sitemap.xml"
import re, sys
from datetime import date
from pathlib import Path
p = Path(sys.argv[1])
if p.exists():
    today = date.today().isoformat()
    text = re.sub(r"<lastmod>[^<]+</lastmod>", f"<lastmod>{today}</lastmod>", p.read_text(encoding="utf-8"))
    p.write_text(text, encoding="utf-8")
    print(f"[deploy-hackme-site] sitemap lastmod={today}")
PY
fi

echo "[deploy-hackme-site] rsync web/site -> ${NODE_SSH}:${NODE_DEPLOY_DIR}/web/site/"
deploy_ssh_retry_run _deploy_rsync -az --delete --mkpath \
  "${ROOT_DIR}/web/site/" "${NODE_SSH}:${NODE_DEPLOY_DIR}/web/site/"

# Stable pool-token pointer for apt onboarding docs (tiny; publish even when SKIP_DIST=1).
publish_stable_pool_token() {
  local ver="" src=""
  if [[ -f "${ROOT_DIR}/dist/latest.json" ]] && command -v jq >/dev/null 2>&1; then
    ver="$(jq -r '.version // empty' "${ROOT_DIR}/dist/latest.json" 2>/dev/null || true)"
  fi
  if [[ -n "$ver" && -f "${ROOT_DIR}/dist/release_${ver}/linux/pool.miner.token" ]]; then
    src="${ROOT_DIR}/dist/release_${ver}/linux/pool.miner.token"
  else
    # Prefer CURRENT_VERSION / published channel — sort -V picks rc21 over rc20 if a partial cut exists.
    cur=""
    [[ -f "${ROOT_DIR}/scripts/release/CURRENT_VERSION" ]] && cur="$(tr -d " \n\r" <"${ROOT_DIR}/scripts/release/CURRENT_VERSION" || true)"
    if [[ -n "$cur" && -f "${ROOT_DIR}/dist/release_${cur}/linux/pool.miner.token" ]]; then
      src="${ROOT_DIR}/dist/release_${cur}/linux/pool.miner.token"
    else
      src="$(ls -1 "${ROOT_DIR}"/dist/release_*/linux/pool.miner.token 2>/dev/null | sort -V | tail -n1 || true)"
    fi
  fi
  if [[ -z "$src" || ! -f "$src" ]]; then
    echo "[deploy-hackme-site] WARN: no pool.miner.token to publish at /dist/pool.miner.token" >&2
    return 0
  fi
  install -m 0644 "$src" "${ROOT_DIR}/dist/pool.miner.token"
  echo "[deploy-hackme-site] rsync dist/pool.miner.token ← ${src#"$ROOT_DIR"/}"
  deploy_ssh_retry_run _deploy_rsync -az --mkpath \
    "${ROOT_DIR}/dist/pool.miner.token" "${NODE_SSH}:${NODE_DEPLOY_DIR}/dist/pool.miner.token"
}

if [[ "$SKIP_DIST" != "1" && -d "${ROOT_DIR}/dist" ]]; then
  if [[ ! -f "${ROOT_DIR}/dist/latest.json" ]]; then
    echo "[deploy-hackme-site] generating missing dist/latest.json"
    bash "${ROOT_DIR}/scripts/ops/publish_latest_json.sh" || \
      echo "[deploy-hackme-site] WARN: could not generate latest.json" >&2
  fi
  publish_stable_pool_token
  echo "[deploy-hackme-site] rsync dist/ -> ${NODE_SSH}:${NODE_DEPLOY_DIR}/dist/"
  deploy_ssh_retry_run _deploy_rsync -az --mkpath "${ROOT_DIR}/dist/" "${NODE_SSH}:${NODE_DEPLOY_DIR}/dist/"
elif [[ -d "${ROOT_DIR}/dist" ]]; then
  publish_stable_pool_token
fi

# Signed apt repo (pool + dists) — separate from dist/ release blobs
if [[ "${SKIP_APT:-0}" != "1" && -d "${ROOT_DIR}/dist/apt/repo" ]]; then
  echo "[deploy-hackme-site] rsync apt repo -> ${NODE_SSH}:${NODE_DEPLOY_DIR}/apt/"
  deploy_ssh_retry_run _deploy_rsync -az --mkpath \
    "${ROOT_DIR}/dist/apt/repo/" "${NODE_SSH}:${NODE_DEPLOY_DIR}/apt/"
  # Ensure one-shot bootstrap files exist even if an older publish omitted them
  if [[ -d "${ROOT_DIR}/web/site/apt" ]]; then
    for f in hackme-archive-keyring.gpg hackme-archive-keyring.asc hackme.list install.sh upgrade.sh README.txt; do
      [[ -f "${ROOT_DIR}/web/site/apt/${f}" ]] || continue
      deploy_ssh_retry_run _deploy_rsync -az \
        "${ROOT_DIR}/web/site/apt/${f}" "${NODE_SSH}:${NODE_DEPLOY_DIR}/apt/${f}"
    done
  fi
  # Public keyring + list also live under web/site/apt/ (rsynced with site)
fi

if [[ "$SYNC_NGINX_SITE_CONF" == "1" ]]; then
  if [[ ! -f "$NGINX_SITE_CONF" ]]; then
    echo "[deploy-hackme-site] missing NGINX_SITE_CONF: $NGINX_SITE_CONF" >&2
    exit 2
  fi
  echo "[deploy-hackme-site] sync nginx site conf -> sites-available + sites-enabled"
  deploy_ssh_retry_run _deploy_rsync -az "$NGINX_SITE_CONF" "${NODE_SSH}:/tmp/hackme-site-domain.conf.new"
  # Origin keeps a real file in sites-enabled (not always a symlink) — update both.
  deploy_ssh_retry_run _deploy_ssh "$NODE_SSH" \
    "sudo cp /tmp/hackme-site-domain.conf.new /etc/nginx/sites-available/hackme-site-domain.conf && sudo cp /tmp/hackme-site-domain.conf.new /etc/nginx/sites-enabled/hackme-site-domain.conf"
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

# Status codes cannot detect a missing page here: nginx ends in "try_files $uri $uri/
# /index.html", so a deleted page answers 200 with the home page. A deploy run from a
# branch that lacked a page once removed it from the server (rsync --delete) and every
# HTTP check still passed. Compare titles instead — a page serving the home page's title
# is a page that is gone.
echo "[deploy-hackme-site] content smoke (title match, catches try_files fallback)"
home_title="$(curl -fsS --max-time 15 "https://hackme.tech/" 2>/dev/null \
  | grep -oE '<title>[^<]*</title>' | head -1 || true)"
smoke_bad=0
smoke_n=0
for f in "$ROOT_DIR"/web/site/*.html; do
  name="$(basename "$f")"
  case "$name" in healthz.html) continue ;; esac
  want="$(grep -oE '<title>[^<]*</title>' "$f" | head -1 || true)"
  [[ -z "$want" ]] && continue
  got="$(curl -fsS --max-time 12 "https://hackme.tech/${name}" 2>/dev/null \
    | grep -oE '<title>[^<]*</title>' | head -1 || true)"
  smoke_n=$((smoke_n + 1))
  if [[ "$got" != "$want" ]]; then
    if [[ -n "$home_title" && "$got" == "$home_title" ]]; then
      echo "[deploy-hackme-site] FAIL ${name} serves the home page — it is missing on the server" >&2
    else
      echo "[deploy-hackme-site] FAIL ${name} title mismatch: want ${want:-<none>} got ${got:-<none>}" >&2
    fi
    smoke_bad=$((smoke_bad + 1))
  fi
done
if [[ "$smoke_bad" -gt 0 ]]; then
  echo "[deploy-hackme-site] content smoke FAILED for ${smoke_bad}/${smoke_n} page(s)" >&2
  exit 1
fi
echo "[deploy-hackme-site] content smoke PASS (${smoke_n} pages)"

if command -v jq >/dev/null 2>&1; then
  wcode="$(curl -fsS --max-time 15 -o /dev/null -w "%{http_code}" "https://hackme.tech/api/wallet" || true)"
  if [[ "$wcode" == "200" ]]; then
    echo "[deploy-hackme-site] GET /api/wallet HTTP 200"
  else
    echo "[deploy-hackme-site] WARN: GET /api/wallet HTTP ${wcode:-error} (run with SYNC_NGINX_SITE_CONF=1 if 403)" >&2
  fi
  lcode="$(curl -fsS --max-time 15 -o /tmp/hackme-latest-smoke.json -w "%{http_code}" "https://hackme.tech/dist/latest.json" || true)"
  if [[ "$lcode" == "200" ]] && jq -e '.schema=="hackme.release.latest.v1"' /tmp/hackme-latest-smoke.json >/dev/null 2>&1; then
    echo "[deploy-hackme-site] GET /dist/latest.json OK → $(jq -r .version /tmp/hackme-latest-smoke.json)"
  else
    echo "[deploy-hackme-site] WARN: /dist/latest.json HTTP ${lcode:-error} (publish via scripts/ops/publish_latest_json.sh + dist rsync)" >&2
  fi
  rm -f /tmp/hackme-latest-smoke.json
  acode="$(curl -fsS --max-time 15 -o /dev/null -w "%{http_code}" "https://hackme.tech/apt/dists/stable/InRelease" || true)"
  if [[ "$acode" == "200" ]]; then
    echo "[deploy-hackme-site] GET /apt/dists/stable/InRelease HTTP 200"
  else
    echo "[deploy-hackme-site] WARN: /apt InRelease HTTP ${acode:-error} (rsync dist/apt/repo + nginx /apt/)" >&2
  fi
  tcode="$(curl -fsS --max-time 15 -o /tmp/hackme-pool-token-smoke -w "%{http_code}" "https://hackme.tech/dist/pool.miner.token" || true)"
  tlen="$(wc -c </tmp/hackme-pool-token-smoke 2>/dev/null | tr -d ' ' || echo 0)"
  rm -f /tmp/hackme-pool-token-smoke
  if [[ "$tcode" == "200" && "${tlen:-0}" -ge 16 ]]; then
    echo "[deploy-hackme-site] GET /dist/pool.miner.token OK (${tlen} bytes)"
  else
    echo "[deploy-hackme-site] WARN: /dist/pool.miner.token HTTP ${tcode:-error} len=${tlen:-0}" >&2
  fi
fi
