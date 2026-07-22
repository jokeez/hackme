#!/usr/bin/env bash
# Allow GET /api/wallet on hackme.tech (fuzzing developer portal).
# Run on VPS or: ssh hackme-vps 'sudo bash -s' < scripts/ops/vps_patch_nginx_wallet_public.sh
set -euo pipefail
CONF="${1:-/etc/nginx/sites-available/hackme-site-domain.conf}"
if [[ ! -f "$CONF" ]]; then
  echo "[nginx-wallet] missing $CONF" >&2
  exit 1
fi
# Idempotent: allow GET /api/wallet, /api/wallet/earnings, /api/wallet/activity on public node API paths.
if grep -q 'wallet/activity' "$CONF"; then
  echo "[nginx-wallet] already patched (activity)"
else
  sed -i 's#wallet/earnings|#wallet/earnings|wallet/activity|#g' "$CONF"
  echo "[nginx-wallet] patched wallet/activity in $CONF"
fi
if grep -q 'wallet|wallet/earnings' "$CONF"; then
  echo "[nginx-wallet] wallet base path ok"
else
  sed -i 's#network/stats|wallet/earnings)#network/stats|wallet|wallet/earnings)#g' "$CONF"
  echo "[nginx-wallet] patched wallet base in $CONF"
fi
nginx -t
systemctl reload nginx
echo "[nginx-wallet] reload ok"
