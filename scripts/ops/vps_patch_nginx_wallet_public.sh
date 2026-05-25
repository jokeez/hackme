#!/usr/bin/env bash
# Allow GET /api/wallet on hackme.tech (fuzzing developer portal).
# Run on VPS or: ssh hackme-vps 'sudo bash -s' < scripts/ops/vps_patch_nginx_wallet_public.sh
set -euo pipefail
CONF="${1:-/etc/nginx/sites-available/hackme-site-domain.conf}"
if [[ ! -f "$CONF" ]]; then
  echo "[nginx-wallet] missing $CONF" >&2
  exit 1
fi
# Idempotent: add wallet| before wallet/earnings in public node API location blocks.
if grep -q 'wallet|wallet/earnings' "$CONF"; then
  echo "[nginx-wallet] already patched"
else
  sed -i 's#network/stats|wallet/earnings)#network/stats|wallet|wallet/earnings)#g' "$CONF"
  echo "[nginx-wallet] patched $CONF"
fi
nginx -t
systemctl reload nginx
echo "[nginx-wallet] reload ok"
