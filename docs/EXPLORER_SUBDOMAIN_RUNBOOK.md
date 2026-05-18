# Explorer Subdomain Runbook

Goal: expose public block/tx/address explorer on a dedicated subdomain with read-only API surface.

This setup intentionally **does not** expose mutating/admin endpoints.

## 1) Prerequisites on VPS

- Node is up (`hackme-node`) and reachable locally (`http://127.0.0.1:18080` by default in worker mode)
- Nginx installed and running
- DNS `A` record for explorer host points to VPS

## 2) One-command Nginx setup

Run on VPS from project root:

```bash
cd /opt/hackme
EXPLORER_HOST=explorer.example.com \
UPSTREAM=127.0.0.1:18080 \
bash scripts/ops/explorer_subdomain_up.sh
```

This will:

- install `sites-available/hackme-explorer.conf`
- enable vhost in `sites-enabled`
- `nginx -t` + reload
- run smoke check `http://explorer.example.com/explorer`

## 3) Optional TLS (Let's Encrypt)

```bash
cd /opt/hackme
EXPLORER_HOST=explorer.example.com \
UPSTREAM=127.0.0.1:18080 \
ENABLE_TLS=1 \
EMAIL=ops@example.com \
bash scripts/ops/explorer_subdomain_up.sh
```

## 4) Public endpoints exposed by this vhost

- `GET /explorer`
- `GET /api/chain` (blocks may include `miner_address_effective` when legacy JSON omitted `miner_address` but pubkey is present)
- `GET /api/reports/blocks` and `GET /api/reports/block?index=N` (summaries use effective miner for display)
- `GET /api/tx/pool`
- `GET /api/tx/{hash}`
- `GET /api/address/{address}`

All other `/api/*` paths return `403` on this public explorer host.

## 5) Quick validation

```bash
curl -iS http://explorer.example.com/explorer
curl -iS http://explorer.example.com/api/reports/blocks?limit=5
curl -iS "http://explorer.example.com/api/reports/block?index=1"
curl -iS http://explorer.example.com/api/status   # expected: 403
```

Если explorer уже на **HTTPS с ручным vhost** (не из шаблона `domain_https_up.sh` целиком), после обновления репозитория можно один раз прогнать whitelist API на сервере:  
`ssh … 'sudo bash -s' < scripts/ops/vps_patch_explorer_nginx_api_routes.sh` — добавляет `chain` и `reports/block` в `location ~ ^/api/…` для `hackme-explorer-domain.conf`.

## 6) Rollback

```bash
sudo rm -f /etc/nginx/sites-enabled/hackme-explorer.conf
sudo rm -f /etc/nginx/sites-available/hackme-explorer.conf
sudo nginx -t && sudo systemctl reload nginx
```
