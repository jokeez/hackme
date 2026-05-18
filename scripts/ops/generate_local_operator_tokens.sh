#!/usr/bin/env bash
# Writes fresh operator tokens into .secrets/ (gitignored). Re-run with FORCE=1 to overwrite.
# Wire into env:
#   HACKME_ADMIN_TOKEN              ← hackme_node_admin_token
#   HACKME_POOL_COORDINATOR_TOKEN   ← hackme_coordinator_admin_token (must match pool coordinator process)
#
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SEC="$ROOT_DIR/.secrets"
mkdir -p "$SEC"
chmod 700 "$SEC" 2>/dev/null || true

gen_hex() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 24
    return
  fi
  python3 - <<'PY'
import secrets
print(secrets.token_hex(24))
PY
}

write_once() {
  local path="$1"
  if [[ -f "$path" && "${FORCE:-}" != "1" ]]; then
    echo "[tokens] keep existing $(basename "$path") (set FORCE=1 to replace)"
    return
  fi
  umask 077
  gen_hex >"$path"
  chmod 600 "$path" || true
  echo "[tokens] wrote $path"
}

write_once "$SEC/hackme_node_admin_token"
write_once "$SEC/hackme_coordinator_admin_token"

echo ""
echo "--- paste into .env.desktop (or systemd) ---"
echo "HACKME_ADMIN_TOKEN=$(tr -d '\r\n' <"$SEC/hackme_node_admin_token")"
echo "HACKME_POOL_COORDINATOR_TOKEN=$(tr -d '\r\n' <"$SEC/hackme_coordinator_admin_token")"
echo "--- end ---"
