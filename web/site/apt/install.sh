#!/usr/bin/env bash
# One-shot: trust HackMe apt + install hackme-node.
#   curl -fsSL https://hackme.tech/apt/install.sh | sudo bash
#
# Env:
#   HACKME_APT_BASE   default https://hackme.tech/apt
#   HACKME_APT_SKIP_INSTALL=1  — only keyring + sources.list
set -euo pipefail

if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
  echo "[hackme-apt] run as root: curl -fsSL https://hackme.tech/apt/install.sh | sudo bash" >&2
  exit 1
fi

APT_BASE="${HACKME_APT_BASE:-https://hackme.tech/apt}"
APT_BASE="${APT_BASE%/}"
KEYRING_DST=/usr/share/keyrings/hackme-archive-keyring.gpg
LIST_DST=/etc/apt/sources.list.d/hackme.list
EXPECTED_FPR="${HACKME_APT_FPR:-C2779678AA76099672C3ACED5D8F54B6E2FC3742}"

export DEBIAN_FRONTEND=noninteractive
command -v curl >/dev/null || { echo "[hackme-apt] need curl" >&2; exit 2; }
command -v gpg >/dev/null || apt-get install -y -qq gnupg ca-certificates >/dev/null

TMP="$(mktemp)"
trap 'rm -f "$TMP"' EXIT

echo "[hackme-apt] fetch keyring ← ${APT_BASE}/hackme-archive-keyring.gpg"
curl -fsSL "${APT_BASE}/hackme-archive-keyring.gpg" -o "$TMP"
install -d -m 0755 /usr/share/keyrings
install -m 0644 "$TMP" "$KEYRING_DST"

if command -v gpg >/dev/null; then
  got="$(gpg --no-default-keyring --keyring "$KEYRING_DST" --with-colons --list-keys 2>/dev/null \
    | awk -F: '/^fpr:/ {print $10; exit}')"
  if [[ -n "$got" && "$got" != "$EXPECTED_FPR" ]]; then
    echo "[hackme-apt] WARN: key fingerprint $got (expected $EXPECTED_FPR)" >&2
  else
    echo "[hackme-apt] key OK ${EXPECTED_FPR:0:16}…"
  fi
fi

echo "deb [signed-by=${KEYRING_DST}] ${APT_BASE} stable main" >"$LIST_DST"
chmod 0644 "$LIST_DST"
echo "[hackme-apt] wrote $LIST_DST"

apt-get update -qq -o Dir::Etc::sourcelist="$LIST_DST" -o Dir::Etc::sourceparts=- -o APT::Get::List-Cleanup=0 \
  || apt-get update -qq

if [[ "${HACKME_APT_SKIP_INSTALL:-0}" == "1" ]]; then
  echo "[hackme-apt] skip install (HACKME_APT_SKIP_INSTALL=1). Next: sudo apt install hackme-node"
  exit 0
fi

echo "[hackme-apt] install hackme-node"
apt-get install -y hackme-node
echo "[hackme-apt] OK — binaries in /opt/hackme"
echo "[hackme-apt] start:  bash /opt/hackme/start_hackme_miner.sh"
echo "[hackme-apt] later:  sudo apt upgrade hackme-node"
