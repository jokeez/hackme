# HackMe public site

Static front-end for [hackme.tech](https://hackme.tech). Served separately from the node and coordinator (Nginx/CDN root = this folder).

## Pages

| File | Purpose |
|------|---------|
| `index.html` | Landing |
| `downloads.html` | Release artifacts and checksums |
| `docs.html` | Documentation hub |
| `economics-model.html` | Chain / pool / orders economics |
| `pool/explorer` (proxied) | Live explorer on production |

Release label and download URLs: `assets/app.js` → `RELEASE_VER` (must match `scripts/release/CURRENT_VERSION` and `dist/release_<VERSION>/` on the server). HackMe OS ISO uses `ISO_CHANNEL` in `app.js` / `scripts/release/CURRENT_ISO_VERSION` until the ISO is rebuilt for the Win/Linux channel.

## Local preview

From repo root:

```bash
python3 -m http.server 8090
# http://127.0.0.1:8090/web/site/index.html
```

## Deploy

```bash
NODE_SSH=hackme-vps NODE_DEPLOY_DIR=/opt/hackme bash scripts/ops/deploy_hackme_site.sh
```

Do **not** commit operator tokens, VPS passwords, or analytics exports into this tree.

## Verify after deploy

```bash
bash scripts/tests/public_site_smoke.sh
```

Node/coordinator source: [README.md](../../README.md) · setup: [docs/SETUP.md](../../docs/SETUP.md)
