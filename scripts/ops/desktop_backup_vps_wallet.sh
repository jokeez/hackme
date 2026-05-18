#!/usr/bin/env bash
# Backup VPS miner wallet (HMC-381c…) + tokens to Desktop folder.
# Primary desktop mining wallet stays in repo: logs/desktop/data → HMC-91fe…
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
DESKTOP_BACKUP="${DESKTOP_BACKUP:-$HOME/Desktop/HackMe-backup-VPS-HMC-381c0c5e2cfcc714}"
mkdir -p "$DESKTOP_BACKUP"/{seeds,secrets,env,notes}

addr_vps="$(cd "$ROOT" && go run ./tools/show_node_addr data 2>/dev/null || true)"
addr_primary="$(cd "$ROOT" && go run ./tools/show_node_addr "$ROOT/logs/desktop/data" 2>/dev/null || true)"

{
  echo "HackMe credential backup — $STAMP"
  echo ""
  echo "VPS / legacy miner address (repo ./data): $addr_vps"
  echo "Primary desktop wallet (logs/desktop/data): $addr_primary"
  echo ""
  echo "HackMe uses 32-byte Ed25519 seeds (64 hex chars), not BIP39 mnemonics."
  echo "Keep this folder offline and chmod 700. Do not commit to git."
  echo ""
  echo "Mining on desktop should use: $addr_primary"
  echo "This backup is the old VPS miner key at: $addr_vps"
} >"$DESKTOP_BACKUP/README.txt"

printf '%s\n' "$addr_vps" >"$DESKTOP_BACKUP/address_vps_HMC-381c.txt"
printf '%s\n' "$addr_primary" >"$DESKTOP_BACKUP/address_primary_HMC-91fe.txt"

install_seed() {
  local src="$1" dst="$2"
  [[ -f "$src" ]] || return 0
  install -m 600 "$src" "$dst"
  xxd -p -c 64 "$dst" >"${dst}.hex.txt"
  chmod 600 "${dst}.hex.txt"
}

install_seed "$ROOT/data/node_ed25519.seed" "$DESKTOP_BACKUP/seeds/vps_node_ed25519.seed"
install_seed "$ROOT/data/miner_submit_ed25519_seed.hex" "$DESKTOP_BACKUP/seeds/vps_miner_submit_ed25519_seed.hex"
[[ -f "$ROOT/tmp/vps_node_ed25519.seed" ]] && install_seed "$ROOT/tmp/vps_node_ed25519.seed" "$DESKTOP_BACKUP/seeds/vps_node_ed25519.seed.from_tmp"

install_seed "$ROOT/logs/desktop/data/node_ed25519.seed" "$DESKTOP_BACKUP/seeds/primary_desktop_node_ed25519.seed"
install_seed "$ROOT/logs/desktop/data/miner_submit_ed25519_seed.hex" "$DESKTOP_BACKUP/seeds/primary_desktop_miner_submit_ed25519_seed.hex"

for f in hackme_admin_token hackme_coordinator_admin_token hackme_treasury_ed25519_seed.hex; do
  [[ -f "$ROOT/.secrets/$f" ]] && install -m 600 "$ROOT/.secrets/$f" "$DESKTOP_BACKUP/secrets/$f"
done

if [[ -f "$ROOT/.env.desktop" ]]; then
  install -m 600 "$ROOT/.env.desktop" "$DESKTOP_BACKUP/env/dotenv.desktop.snapshot"
fi
if [[ -f "$ROOT/.secrets/hackme.public.extra.env" ]]; then
  install -m 600 "$ROOT/.secrets/hackme.public.extra.env" "$DESKTOP_BACKUP/env/hackme.public.extra.env"
fi

{
  echo "HACKME_ADMIN_TOKEN source: .env.desktop + .secrets/hackme_admin_token"
  echo "HACKME_POOL_COORDINATOR_TOKEN source: .secrets/hackme_coordinator_admin_token"
  echo "Coordinator URL: https://hackme.tech/pool/coordinator"
  echo "Canonical: https://hackme.tech"
  echo "Desktop API: http://127.0.0.1:8080"
} >"$DESKTOP_BACKUP/notes/tokens_where.txt"

chmod 700 "$DESKTOP_BACKUP" "$DESKTOP_BACKUP/seeds" "$DESKTOP_BACKUP/secrets"
echo "[backup] wrote $DESKTOP_BACKUP"
echo "[backup] VPS=$addr_vps primary=$addr_primary"
