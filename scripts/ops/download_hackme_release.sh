#!/usr/bin/env bash
# Reliable release download — GitHub first, then origin, then site CDN.
#
#   bash scripts/ops/download_hackme_release.sh
#   bash scripts/ops/download_hackme_release.sh 0.1.0-rc13 linux
#   OUT_DIR=~/Downloads bash scripts/ops/download_hackme_release.sh
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VER="${1:-$(grep -oE 'RELEASE_VER = "[^"]+"' "$ROOT_DIR/web/site/assets/app.js" | sed -n 's/.*"\([^"]*\)".*/\1/p')}"
KIND="${2:-linux}"
OUT_DIR="${OUT_DIR:-$PWD}"
ORIGIN_IP="${HACKME_ORIGIN_IP:-132.243.112.100}"
MIN_BYTES="${MIN_BYTES:-1000000}"
GH_REPO="${HACKME_GH_REPO:-jokeez/hackme}"

case "$KIND" in
  linux) FILE="hackme_${VER}_linux.tar.gz" ;;
  windows) FILE="hackme_${VER}_windows_setup.zip" ;;
  iso) FILE="HackMe-OS-${VER}-amd64.iso" ;;
  *) echo "usage: $0 [VERSION] [linux|windows|iso]" >&2; exit 2 ;;
esac

REL="release_${VER}"
PATH_ON_SITE="/dist/${REL}/${FILE}"
OUT="${OUT_DIR}/${FILE}"
mkdir -p "$OUT_DIR"

# Prefer GitHub (authoritative for fix re-uploads). Site CDN may stay stale when
# /dist/ was previously advertised as immutable.
MIRRORS=(
  "https://github.com/${GH_REPO}/releases/download/${VER}/${FILE}|GitHub Releases"
  "https://${ORIGIN_IP}${PATH_ON_SITE}|origin IP (direct)"
  "https://dl.hackme.tech${PATH_ON_SITE}|dl.hackme.tech (direct)"
  "https://hackme.tech/dist/${REL}/live/${FILE}|hackme.tech live/ (short-TTL)"
  "https://hackme.tech${PATH_ON_SITE}|hackme.tech CDN"
)

for entry in "${MIRRORS[@]}"; do
  url="${entry%%|*}"
  label="${entry##*|}"
  extra=()
  max_time=600
  if [[ "$url" == https://hackme.tech/dist/*/hackme_* ]]; then
    # Stale CF HIT can hang/truncate — fail fast and try next mirror.
    max_time=25
  fi
  if [[ "$url" == https://${ORIGIN_IP}* ]]; then
    extra=(-H "Host: hackme.tech" -k)
  fi
  echo "[download] try ${label}: ${url}"
  if curl -fL --retry 1 --connect-timeout 10 --max-time "$max_time" \
      "${extra[@]}" -C - -o "${OUT}.part" "$url"; then
    n="$(wc -c <"${OUT}.part" | tr -d ' ')"
    if [[ "$n" -ge "$MIN_BYTES" ]]; then
      mv -f "${OUT}.part" "$OUT"
      echo "[download] OK via ${label} — ${n} bytes → ${OUT}"
      exit 0
    fi
    echo "[download] WARN ${label}: only ${n} bytes" >&2
  else
    echo "[download] WARN ${label} failed" >&2
  fi
  rm -f "${OUT}.part"
done

echo "[download] FAIL all mirrors — try: curl -fL -k -H 'Host: hackme.tech' -o '${OUT}' 'https://${ORIGIN_IP}${PATH_ON_SITE}'" >&2
exit 1
