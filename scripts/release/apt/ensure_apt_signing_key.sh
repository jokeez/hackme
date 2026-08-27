#!/usr/bin/env bash
# Generate (once) or load HackMe apt signing key into .secrets/apt/ (gitignored).
# Public keyring is written to web/site/apt/ for HTTPS distribution.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
SEC="${ROOT}/.secrets/apt"
WEB_APT="${ROOT}/web/site/apt"
mkdir -p "$SEC" "$WEB_APT"
chmod 700 "$SEC"

export GNUPGHOME="${SEC}/gnupg"
mkdir -p "$GNUPGHOME"
chmod 700 "$GNUPGHOME"

NAME="${HACKME_APT_GPG_NAME:-HackMe Packages}"
EMAIL="${HACKME_APT_GPG_EMAIL:-packages@hackme.tech}"

if ! gpg --list-secret-keys --with-colons 2>/dev/null | grep -q '^sec:'; then
  echo "[apt-gpg] generating signing key (no passphrase — store .secrets/apt securely)"
  gpg --batch --pinentry-mode loopback --passphrase '' \
    --quick-generate-key "${NAME} <${EMAIL}>" rsa4096 sign 3y
fi

FPR="$(gpg --list-secret-keys --with-colons | awk -F: '/^fpr:/ {print $10; exit}')"
[[ -n "$FPR" ]] || { echo "[apt-gpg] no fingerprint" >&2; exit 1; }
echo "$FPR" >"${SEC}/fingerprint.txt"
gpg --export --armor "$FPR" >"${SEC}/hackme-archive-keyring.asc"
gpg --export "$FPR" >"${SEC}/hackme-archive-keyring.gpg"
cp -f "${SEC}/hackme-archive-keyring.asc" "${WEB_APT}/hackme-archive-keyring.asc"
cp -f "${SEC}/hackme-archive-keyring.gpg" "${WEB_APT}/hackme-archive-keyring.gpg"
chmod 644 "${WEB_APT}/hackme-archive-keyring."*

cat >"${WEB_APT}/README.txt" <<EOF
HackMe apt archive keyring
==========================
Install:
  curl -fsSL https://hackme.tech/apt/hackme-archive-keyring.gpg \\
    | sudo tee /usr/share/keyrings/hackme-archive-keyring.gpg >/dev/null

  echo 'deb [signed-by=/usr/share/keyrings/hackme-archive-keyring.gpg] https://hackme.tech/apt stable main' \\
    | sudo tee /etc/apt/sources.list.d/hackme.list

  sudo apt update && sudo apt install hackme-node

Fingerprint: ${FPR}
EOF

echo "[apt-gpg] OK fpr=${FPR}"
echo "[apt-gpg] public → ${WEB_APT}/hackme-archive-keyring.gpg"
echo "[apt-gpg] secret GNUPGHOME=${GNUPGHOME} (do not commit)"
