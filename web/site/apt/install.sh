#!/usr/bin/env bash
# One-shot: trust HackMe apt + install hackme-node.
#   curl -fsSL https://hackme.tech/apt/install.sh | sudo bash
#
# Downloads the .deb from GitHub Releases (fast CDN), verifies SHA256 against
# the signed apt Packages index, then apt-installs locally. Apt repo stays
# configured so later `apt upgrade hackme-node` works (pool .deb URLs 302 → GitHub).
#
# Env:
#   HACKME_APT_BASE          default https://hackme.tech/apt
#   HACKME_APT_SKIP_INSTALL=1  — only keyring + sources.list
#   HACKME_APT_DEB_URL       override .deb download URL
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
GH_BASE="${HACKME_GH_RELEASE_BASE:-https://github.com/jokeez/hackme/releases/download}"

export DEBIAN_FRONTEND=noninteractive
command -v curl >/dev/null || { echo "[hackme-apt] need curl" >&2; exit 2; }
command -v gpg >/dev/null || apt-get install -y -qq gnupg ca-certificates >/dev/null
command -v sha256sum >/dev/null || { echo "[hackme-apt] need sha256sum" >&2; exit 2; }

WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

echo "[hackme-apt] fetch keyring ← ${APT_BASE}/hackme-archive-keyring.gpg"
curl -fsSL "${APT_BASE}/hackme-archive-keyring.gpg" -o "${WORKDIR}/keyring.gpg"
install -d -m 0755 /usr/share/keyrings
install -m 0644 "${WORKDIR}/keyring.gpg" "$KEYRING_DST"

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

echo "[hackme-apt] read Packages index"
curl -fsSL "${APT_BASE}/dists/stable/main/binary-amd64/Packages" -o "${WORKDIR}/Packages"
# First hackme-node stanza
awk '
  BEGIN{p=0}
  /^Package: hackme-node$/{p=1; next}
  p && /^Package:/{exit}
  p {print}
' "${WORKDIR}/Packages" >"${WORKDIR}/pkg.stanza"
FILENAME="$(awk '/^Filename:/{print $2; exit}' "${WORKDIR}/pkg.stanza")"
SIZE="$(awk '/^Size:/{print $2; exit}' "${WORKDIR}/pkg.stanza")"
SHA="$(awk '/^SHA256:/{print $2; exit}' "${WORKDIR}/pkg.stanza")"
[[ -n "$FILENAME" && -n "$SHA" && -n "$SIZE" ]] || {
  echo "[hackme-apt] FAIL: could not parse hackme-node from Packages" >&2
  exit 3
}
BASENAME="$(basename "$FILENAME")"
# Filename uses 0.1.0-rc16; GitHub tag is the same string.
VER_TAG="${BASENAME#hackme-node_}"
VER_TAG="${VER_TAG%_amd64.deb}"
DEB_URL="${HACKME_APT_DEB_URL:-${GH_BASE}/${VER_TAG}/${BASENAME}}"
DEB_PATH="${WORKDIR}/${BASENAME}"

echo "[hackme-apt] download ${BASENAME} (${SIZE} bytes) ← GitHub CDN"
echo "[hackme-apt]   ${DEB_URL}"
# -L follow redirects; --retry for flaky links; show progress
curl -fL --retry 3 --retry-delay 2 --connect-timeout 20 \
  -o "$DEB_PATH" "$DEB_URL"

got_size="$(wc -c <"$DEB_PATH" | tr -d ' ')"
got_sha="$(sha256sum "$DEB_PATH" | awk '{print $1}')"
if [[ "$got_size" != "$SIZE" ]]; then
  echo "[hackme-apt] FAIL: size ${got_size} != ${SIZE} (Packages)" >&2
  exit 4
fi
if [[ "$got_sha" != "$SHA" ]]; then
  echo "[hackme-apt] FAIL: sha256 mismatch" >&2
  echo "  got  $got_sha" >&2
  echo "  want $SHA" >&2
  exit 4
fi
echo "[hackme-apt] sha256 OK ${SHA:0:16}…"

echo "[hackme-apt] apt install ./${BASENAME}"
apt-get install -y "$DEB_PATH"
echo "[hackme-apt] OK — binaries in /opt/hackme"
echo "[hackme-apt] start:  bash /opt/hackme/start_hackme_miner.sh"
echo "[hackme-apt] later:  sudo apt upgrade hackme-node"
