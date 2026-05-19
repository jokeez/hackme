#!/usr/bin/env bash
# Backup consensus treasury (50k genesis DevFeeAddress) to Desktop — OFFLINE ONLY, never commit.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
DESKTOP="${DESKTOP_BACKUP:-$HOME/Desktop/HackMe-Treasury-Backup-$STAMP}"
NODE_SSH="${NODE_SSH:-hackme-vps}"
VPS_DATA="${VPS_DATA_DIR:-/opt/hackme/data}"
TREASURY_ADDR="HMC-719006d93916ad52"

mkdir -p "$DESKTOP"/{seeds,secrets,database,notes,env}

install_seed() {
  local src="$1" name="$2"
  [[ -f "$src" ]] || return 0
  install -m 600 "$src" "$DESKTOP/seeds/$name"
  tr -d '\r\n' <"$src" >"$DESKTOP/seeds/${name}.hex.txt"
  chmod 600 "$DESKTOP/seeds/${name}.hex.txt"
}

echo "[treasury-backup] target: $DESKTOP"

# Treasury key (controls DevFeeAddress / 50k genesis recipient)
install_seed "$ROOT/.secrets/hackme_treasury_ed25519_seed.hex" "hackme_treasury_ed25519_seed.hex"
if [[ -f "$DESKTOP/seeds/hackme_treasury_ed25519_seed.hex" ]]; then
  cp -a "$DESKTOP/seeds/hackme_treasury_ed25519_seed.hex" "$DESKTOP/seeds/node_ed25519.seed.treasury-copy"
  chmod 600 "$DESKTOP/seeds/node_ed25519.seed.treasury-copy"
fi

# Operator tokens (optional but useful for restore / settlement)
for f in hackme_admin_token hackme_coordinator_admin_token; do
  [[ -f "$ROOT/.secrets/$f" ]] && install -m 600 "$ROOT/.secrets/$f" "$DESKTOP/secrets/$f"
done

[[ -f "$ROOT/.env.desktop" ]] && install -m 600 "$ROOT/.env.desktop" "$DESKTOP/env/dotenv.desktop.snapshot"
[[ -f "$ROOT/.env.settlement.example" ]] && cp "$ROOT/scripts/ops/settlement.env.example" "$DESKTOP/env/settlement.env.example" 2>/dev/null || true

# Local chain DB (if present)
if [[ -f "$ROOT/data/hackme.db" ]]; then
  echo "[treasury-backup] copy local data/hackme.db (may be stale vs VPS)"
  cp -a "$ROOT/data/hackme.db" "$DESKTOP/database/hackme.db.local-copy"
  chmod 600 "$DESKTOP/database/hackme.db.local-copy"
fi

# Canonical VPS chain DB
if ssh -o BatchMode=yes -o ConnectTimeout=15 "$NODE_SSH" "test -f '$VPS_DATA/hackme.db'"; then
  echo "[treasury-backup] rsync canonical $NODE_SSH:$VPS_DATA/hackme.db"
  rsync -az --progress "${NODE_SSH}:${VPS_DATA}/hackme.db" "$DESKTOP/database/hackme.db.canonical-vps"
  chmod 600 "$DESKTOP/database/hackme.db.canonical-vps"
  rsync -az "${NODE_SSH}:${VPS_DATA}/coordinator.db" "$DESKTOP/database/coordinator.db.vps" 2>/dev/null || true
  for f in hackme_admin_token hackme_coordinator_admin_token hackme_treasury_ed25519_seed.hex; do
    ssh -o BatchMode=yes "$NODE_SSH" "test -f /opt/hackme/.secrets/$f" 2>/dev/null && \
      rsync -az "${NODE_SSH}:/opt/hackme/.secrets/$f" "$DESKTOP/secrets/vps-$f" && chmod 600 "$DESKTOP/secrets/vps-$f" || true
  done
else
  echo "[treasury-backup] WARN: VPS hackme.db not reachable" >&2
fi

# Verify seed → address
TREASURY_FROM_SEED="$(python3 - "$DESKTOP/seeds/hackme_treasury_ed25519_seed.hex" <<'PY'
import hashlib, sys
from pathlib import Path
p = Path(sys.argv[1])
if not p.exists():
    print("")
    raise SystemExit(0)
h = p.read_text().strip()
seed = bytes.fromhex(h)
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
pk = Ed25519PrivateKey.from_private_bytes(seed).public_key().public_bytes_raw()
print("HMC-" + hashlib.sha256(pk).hexdigest()[:16])
PY
)" || TREASURY_FROM_SEED=""

{
  echo "HackMe treasury backup — $STAMP"
  echo ""
  echo "Consensus treasury (genesis 50,000 HMC mint recipient):"
  echo "  DevFeeAddress: $TREASURY_ADDR"
  echo "  Seed file: seeds/hackme_treasury_ed25519_seed.hex"
  echo "  Derived from local seed: ${TREASURY_FROM_SEED:-verify manually}"
  echo ""
  echo "Canonical chain state: database/hackme.db.canonical-vps"
  echo "Local copy (if any):   database/hackme.db.local-copy"
  echo ""
  echo "Restore node with treasury key:"
  echo "  mkdir -p data && cp seeds/node_ed25519.seed.treasury-copy data/node_ed25519.seed"
  echo "  chmod 600 data/node_ed25519.seed"
  echo "  # Or set HACKME_DATA_DIR to a copy of hackme.db.canonical-vps"
  echo ""
  echo "NEVER upload this folder to GitHub, cloud drives, or chat."
  echo "chmod 700 this directory. Consider encrypted USB / VeraCrypt."
} >"$DESKTOP/README.txt"

chmod 700 "$DESKTOP" "$DESKTOP/seeds" "$DESKTOP/secrets" "$DESKTOP/database" 2>/dev/null || true
echo "[treasury-backup] done: $DESKTOP"
echo "[treasury-backup] treasury=$TREASURY_ADDR seed_match=${TREASURY_FROM_SEED:-unknown}"
