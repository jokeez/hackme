#!/usr/bin/env bash
# Ensure hub workerfuzz uses a dedicated payout key (never treasury DevFeeAddress).
set -euo pipefail
INSTALL="${HACKME_INSTALL:-/opt/hackme}"
ENV_FILE="${INSTALL}/.env.workerfuzz"
DEV_ADDR="${DEV_TREASURY_ADDR:-HMC-719006d93916ad52}"
WORKER_ID="${WORKER_ID:-vps-canary-fuzz-01}"

derive_addr() {
  python3 - "$1" <<'PY'
import hashlib, sys
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
seed = bytes.fromhex(sys.argv[1].strip())
pk = Ed25519PrivateKey.from_private_bytes(seed)
pub = pk.public_key().public_bytes_raw()
print("HMC-" + hashlib.sha256(pub).hexdigest()[:16])
PY
}

gen_seed() {
  go run ./cmd/minersign -gen-seed 2>/dev/null | python3 -c "import json,sys; print(json.load(sys.stdin)['HACKME_MINER_ED25519_SEED_HEX'])"
}

cd "$INSTALL"
mkdir -p "$INSTALL/.secrets"
touch "$ENV_FILE"

current_seed="$(grep -m1 '^HACKME_MINER_ED25519_SEED_HEX=' "$ENV_FILE" 2>/dev/null | cut -d= -f2- | tr -d '\r\n' || true)"
if [[ -n "$current_seed" ]]; then
  current_addr="$(derive_addr "$current_seed" || true)"
else
  current_addr=""
fi

if [[ "$current_addr" == "$DEV_ADDR" ]] || [[ -z "$current_seed" ]]; then
  echo "[workerfuzz-payout] rotating treasury/missing seed -> dedicated fuzz worker key"
  new_seed="$(gen_seed)"
  new_addr="$(derive_addr "$new_seed")"
  if [[ "$new_addr" == "$DEV_ADDR" ]]; then
    echo "[workerfuzz-payout] FATAL: generated seed still maps to treasury" >&2
    exit 1
  fi
  if grep -q '^HACKME_MINER_ED25519_SEED_HEX=' "$ENV_FILE"; then
    sed -i "s|^HACKME_MINER_ED25519_SEED_HEX=.*|HACKME_MINER_ED25519_SEED_HEX=${new_seed}|" "$ENV_FILE"
  else
    echo "HACKME_MINER_ED25519_SEED_HEX=${new_seed}" >>"$ENV_FILE"
  fi
  printf '%s\n' "$new_seed" >"$INSTALL/.secrets/workerfuzz_ed25519_seed.hex"
  chmod 600 "$INSTALL/.secrets/workerfuzz_ed25519_seed.hex"
  current_addr="$new_addr"
  echo "[workerfuzz-payout] new payout ${WORKER_ID} -> ${current_addr}"
else
  echo "[workerfuzz-payout] ok ${WORKER_ID} -> ${current_addr}"
fi

for f in "$INSTALL/.env" "$INSTALL/.env.vps" "$INSTALL/.env.settlement" "$INSTALL/.env.coord"; do
  [[ -f "$f" ]] || continue
  entry="${WORKER_ID}=${current_addr}"
  if grep -q '^WORKER_PAYOUT_MAP=' "$f"; then
    map="$(grep '^WORKER_PAYOUT_MAP=' "$f" | head -1 | cut -d= -f2-)"
    if [[ "$map" != *"${WORKER_ID}="* ]]; then
      sed -i "s|^WORKER_PAYOUT_MAP=.*|WORKER_PAYOUT_MAP=${map},${entry}|" "$f"
      echo "[workerfuzz-payout] appended ${entry} to $(basename "$f")"
    fi
  fi
done

go build -o "$INSTALL/bin/workerfuzz" ./cmd/workerfuzz
go build -o "$INSTALL/bin/coordinator" ./cmd/coordinator
systemctl restart hackme-coordinator.service
systemctl restart hackme-workerfuzz.service
systemctl is-active hackme-coordinator.service hackme-workerfuzz.service
