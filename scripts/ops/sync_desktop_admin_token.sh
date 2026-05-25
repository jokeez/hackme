#!/usr/bin/env bash
# Sync HACKME_ADMIN_TOKEN in .env.desktop from .secrets/hackme_admin_token (no token printed).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SECRET="${SECRET:-$ROOT/.secrets/hackme_admin_token}"
ENV="${DESKTOP_ENV_FILE:-$ROOT/.env.desktop}"

if [[ ! -f "$SECRET" ]]; then
  echo "[sync-admin] missing $SECRET" >&2
  exit 1
fi
TOK="$(head -n1 "$SECRET" | tr -d '\r\n' | tr -d ' ')"
if [[ -z "$TOK" ]]; then
  echo "[sync-admin] empty token in $SECRET" >&2
  exit 1
fi
if [[ ! -f "$ENV" ]]; then
  cp "$ROOT/.env.desktop.example" "$ENV"
  chmod 600 "$ENV"
fi
if grep -q '^HACKME_ADMIN_TOKEN=' "$ENV"; then
  sed -i "s|^HACKME_ADMIN_TOKEN=.*|HACKME_ADMIN_TOKEN=$TOK|" "$ENV"
else
  echo "HACKME_ADMIN_TOKEN=$TOK" >>"$ENV"
fi
chmod 600 "$ENV"
echo "[sync-admin] OK — .env.desktop updated from .secrets/hackme_admin_token"
echo "[sync-admin] restart: bash scripts/ops/desktop_mode_down.sh 2>/dev/null; DESKTOP_PROFILE=command bash scripts/ops/desktop_mode_up.sh"
