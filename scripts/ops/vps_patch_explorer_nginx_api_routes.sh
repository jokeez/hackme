#!/usr/bin/env bash
# Run on the VPS (as root or with sudo) after pulling a new tree, or via:
#   ssh hackme-vps 'sudo bash -s' < scripts/ops/vps_patch_explorer_nginx_api_routes.sh
#
# Expands the public explorer Nginx whitelist to match explorer.html:
#   chain, reports/blocks, reports/block (singular), tx/pool, status.
# Idempotent: skips if already applied.

set -euo pipefail

SUDO=()
if [[ "$(id -u)" != "0" ]]; then
  SUDO=(sudo)
fi

CONF="${EXPLORER_NGINX_CONF:-/etc/nginx/sites-available/hackme-explorer-domain.conf}"
if [[ ! -f "$CONF" ]]; then
  echo "[vps-patch-explorer] missing $CONF" >&2
  exit 2
fi

needle='    location ~ ^/api/(status|reports/blocks|tx/pool)$ {'
repl='    location ~ ^/api/(status|chain|reports/blocks|reports/block|tx/pool)$ {'

if grep -qF 'reports/block' "$CONF" && grep -qF '|chain|' "$CONF"; then
  echo "[vps-patch-explorer] already patched ($CONF)"
  exit 0
fi

if ! grep -qF "$needle" "$CONF"; then
  echo "[vps-patch-explorer] expected line not found; inspect $CONF manually" >&2
  exit 3
fi

"${SUDO[@]}" sed -i "s#${needle}#${repl}#" "$CONF"
"${SUDO[@]}" nginx -t
"${SUDO[@]}" systemctl reload nginx
echo "[vps-patch-explorer] OK: reloaded nginx ($CONF)"
