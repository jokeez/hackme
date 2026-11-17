#!/usr/bin/env bash
# Generate (or refresh pubkey for) the L1 latest.json ed25519 signing key.
# Private key stays under .secrets/ (gitignored). Never prints key material.
#
#   bash scripts/release/keygen_update_ed25519.sh
#   FORCE=1 bash scripts/release/keygen_update_ed25519.sh   # rotate private key
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PRIV="${HACKME_UPDATE_SIGN_KEY:-${ROOT}/.secrets/hackme_update_ed25519.pem}"
PUB_DIR="${ROOT}/scripts/release/keys"
PUB="${PUB_DIR}/hackme_update_ed25519.pub.pem"

command -v openssl >/dev/null || { echo "[keygen-update] openssl required" >&2; exit 2; }
mkdir -p "${ROOT}/.secrets" "$PUB_DIR"
chmod 700 "${ROOT}/.secrets" 2>/dev/null || true

if [[ -f "$PRIV" && "${FORCE:-0}" != "1" ]]; then
  echo "[keygen-update] private key exists at ${PRIV#"$ROOT"/} (FORCE=1 to rotate)"
else
  umask 077
  tmp="$(mktemp "${ROOT}/.secrets/.hackme_update_ed25519.XXXXXX")"
  openssl genpkey -algorithm ED25519 -out "$tmp"
  chmod 600 "$tmp"
  mv -f "$tmp" "$PRIV"
  chmod 600 "$PRIV"
  echo "[keygen-update] wrote private key (path only): ${PRIV#"$ROOT"/}"
fi

openssl pkey -in "$PRIV" -pubout -out "$PUB"
chmod 644 "$PUB"
echo "[keygen-update] public key → ${PUB#"$ROOT"/}"
echo "[keygen-update] next: bash scripts/release/sign_latest_json.sh"
