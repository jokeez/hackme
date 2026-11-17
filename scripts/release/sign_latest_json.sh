#!/usr/bin/env bash
# Detached ed25519 signature over latest.json bytes (OpenSSL rawin → 64-byte .sig).
# Never prints the private key.
#
#   bash scripts/release/sign_latest_json.sh
#   bash scripts/release/sign_latest_json.sh /path/to/latest.json
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PRIV="${HACKME_UPDATE_SIGN_KEY:-${ROOT}/.secrets/hackme_update_ed25519.pem}"
PUB="${ROOT}/scripts/release/keys/hackme_update_ed25519.pub.pem"
JSON="${1:-}"

if [[ -z "$JSON" ]]; then
  VERSION="${VERSION:-$(tr -d ' \n\r' <"${ROOT}/scripts/release/CURRENT_VERSION" 2>/dev/null || true)}"
  if [[ -f "${ROOT}/dist/latest.json" ]]; then
    JSON="${ROOT}/dist/latest.json"
  elif [[ -n "$VERSION" && -f "${ROOT}/dist/release_${VERSION}/latest.json" ]]; then
    JSON="${ROOT}/dist/release_${VERSION}/latest.json"
  else
    echo "[sign-latest] missing dist/latest.json — generate first" >&2
    exit 2
  fi
fi

[[ -f "$JSON" ]] || { echo "[sign-latest] missing $JSON" >&2; exit 2; }
[[ -f "$PRIV" ]] || {
  echo "[sign-latest] missing private key at ${PRIV#"$ROOT"/}" >&2
  echo "[sign-latest] run: bash scripts/release/keygen_update_ed25519.sh" >&2
  exit 2
}
command -v openssl >/dev/null || { echo "[sign-latest] openssl required" >&2; exit 2; }

SIG="${JSON}.sig"
openssl pkeyutl -sign -inkey "$PRIV" -rawin -in "$JSON" -out "$SIG"
chmod 644 "$SIG"

# Self-check with committed pubkey when present
if [[ -f "$PUB" ]]; then
  openssl pkeyutl -verify -pubin -inkey "$PUB" -rawin -in "$JSON" -sigfile "$SIG" >/dev/null
fi

# Mirror next to CDN root + release dir when signing the root copy
if [[ "$JSON" == "${ROOT}/dist/latest.json" ]]; then
  VERSION="$(jq -r '.version // empty' "$JSON" 2>/dev/null || true)"
  if [[ -n "$VERSION" && -d "${ROOT}/dist/release_${VERSION}" ]]; then
    cp -f "$JSON" "${ROOT}/dist/release_${VERSION}/latest.json"
    cp -f "$SIG" "${ROOT}/dist/release_${VERSION}/latest.json.sig"
  fi
elif [[ "$JSON" == */release_*/latest.json ]]; then
  mkdir -p "${ROOT}/dist"
  cp -f "$JSON" "${ROOT}/dist/latest.json"
  cp -f "$SIG" "${ROOT}/dist/latest.json.sig"
fi

echo "[sign-latest] wrote ${SIG#"$ROOT"/} ($(wc -c <"$SIG" | tr -d ' ') bytes)"
