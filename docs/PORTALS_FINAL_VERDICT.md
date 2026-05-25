# Portals — final architecture (localhost-only orders, May 2026)

## Verdict

**Orders and wallet escrow live on the customer's local `hackme-node` (`http://127.0.0.1:8080/`).**  
**hackme.tech** is downloads, docs, pool explorer, and read-only trackers — **no** web portal for creating orders.

This matches the mining model: process on the PC, pool on VPS, nothing extra in the browser on the public site.

## Roles

| Role | Entry | Capabilities |
|------|--------|----------------|
| **Miner** | Downloads / HackMe OS / worker | PoW, pool, wallet on local or rig |
| **Customer (fuzz / useful-PoW)** | Same node binary + `127.0.0.1:8080` | `#wallet` → fund escrow · `#orders` → upload WASM, POST task · pool fills work |
| **Operator** | Local node + **admin token** | Above + `#fuzz` campaign admin, `from_code` |

## Customer flow (simple)

1. Download node (see `downloads.html#local-node`).
2. Open `http://127.0.0.1:8080/`.
3. **Wallet** — see balance in local DB; top up via normal HMC flows on that node.
4. Header → **Developer token** → **Issue** (or `hackme-fuzzing register --base http://127.0.0.1:8080 --save`).
5. **Market** tab → build WASM locally → upload → **POST /api/tasks**.
6. Watch **Workers** / pool stats while network miners complete the order.

## Removed on purpose

- `/pool/developer` and public order UI — redirect to `downloads.html#local-node`.
- Posting orders from hackme.tech (burns operator escrow, wrong wallet story).
- Developer token in sessionStorage on the public origin (XSS / confusion risk).

## What stays on hackme.tech

- `GET /api/tasks` — redacted public list (tracker).
- `GET /api/wallet` — redacted (no treasury).
- `GET /api/fuzz/.../report*` — deliverables with report token only.
- Pool coordinator, explorer, downloads.

## nginx

`/pool/developer`, `/developer-dashboard.html`, `/developer-console.html` → **302** ` /downloads.html#local-node`.

## Deploy

1. `SYNC_NGINX_SITE_CONF=1 ./scripts/ops/deploy_hackme_site.sh`
2. `./scripts/ops/deploy_hackme_node.sh` (embedded `dashboard.html` with developer token + Market tab)
3. `./scripts/tests/fuzzing_developer_portal_smoke.sh`
4. `./scripts/tests/fuzzing_public_hardening_smoke.sh`
