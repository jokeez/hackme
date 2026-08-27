#!/usr/bin/env bash
# One-shot: trust HackMe apt + install hackme-node.
#   curl -fsSL https://hackme.tech/apt/install.sh | sudo bash
#
# Fetches Packages from apt (signed index), downloads .deb from the first working
# mirror (GitHub → hackme.tech/dist → apt pool → origin IP), verifies SHA256,
# then apt-installs locally. Apt repo stays configured for later upgrades.
#
# Env:
#   HACKME_APT_BASE            default https://hackme.tech/apt
#   HACKME_APT_SKIP_INSTALL=1  only keyring + sources.list
#   HACKME_APT_DEB_URL         force a single .deb URL
#   HACKME_ORIGIN_IP           default 132.243.112.100 (grey-cloud bypass)
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
ORIGIN_IP="${HACKME_ORIGIN_IP:-132.243.112.100}"
SITE="${HACKME_SITE:-https://hackme.tech}"

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
VER_TAG="${BASENAME#hackme-node_}"
VER_TAG="${VER_TAG%_amd64.deb}"
DEB_PATH="${WORKDIR}/${BASENAME}"

download_try() {
  local label="$1" url="$2"
  shift 2
  echo "[hackme-apt] try ${label}"
  echo "[hackme-apt]   ${url}"
  if curl -fL --retry 2 --retry-delay 1 --connect-timeout 15 --max-time 600 \
      -o "$DEB_PATH" "$@" "$url"; then
    return 0
  fi
  rm -f "$DEB_PATH"
  return 1
}

ok=0
if [[ -n "${HACKME_APT_DEB_URL:-}" ]]; then
  download_try "HACKME_APT_DEB_URL" "$HACKME_APT_DEB_URL" && ok=1
fi
if [[ "$ok" -eq 0 ]]; then
  download_try "GitHub Releases" "${GH_BASE}/${VER_TAG}/${BASENAME}" && ok=1 || true
fi
if [[ "$ok" -eq 0 ]]; then
  download_try "hackme.tech/dist" \
    "${SITE}/dist/release_${VER_TAG}/${BASENAME}" && ok=1 || true
fi
if [[ "$ok" -eq 0 ]]; then
  download_try "apt pool (Cloudflare)" \
    "${APT_BASE}/${FILENAME}" && ok=1 || true
fi
if [[ "$ok" -eq 0 && -n "$ORIGIN_IP" ]]; then
  # Bypass Cloudflare when GH/CF are blocked or crawling.
  download_try "origin ${ORIGIN_IP} (Host hackme.tech)" \
    "https://${ORIGIN_IP}/dist/release_${VER_TAG}/${BASENAME}" \
    -k -H "Host: hackme.tech" && ok=1 || true
fi
if [[ "$ok" -eq 0 && -n "$ORIGIN_IP" ]]; then
  download_try "origin apt pool" \
    "https://${ORIGIN_IP}/apt/${FILENAME}" \
    -k -H "Host: hackme.tech" && ok=1 || true
fi
if [[ "$ok" -eq 0 ]]; then
  echo "[hackme-apt] FAIL: all download mirrors failed" >&2
  exit 5
fi

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
