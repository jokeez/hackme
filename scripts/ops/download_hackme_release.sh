#!/usr/bin/env bash
# Reliable release download — tries hackme.tech CDN, then direct origin mirror.
#
#   bash scripts/ops/download_hackme_release.sh
#   bash scripts/ops/download_hackme_release.sh 0.1.0-rc11r linux
#   OUT_DIR=~/Downloads bash scripts/ops/download_hackme_release.sh
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VER="${1:-$(grep -oE 'RELEASE_VER = "[^"]+"' "$ROOT_DIR/web/site/assets/app.js" | sed -n 's/.*"\([^"]*\)".*/\1/p')}"
KIND="${2:-linux}"
OUT_DIR="${OUT_DIR:-$PWD}"
ORIGIN_IP="${HACKME_ORIGIN_IP:-132.243.112.100}"
MIN_BYTES="${MIN_BYTES:-1000000}"

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

try_url() {
  local url="$1" label="$2"
  local tmp="${OUT}.part"
  rm -f "$tmp"
  echo "[download] ${label}: ${url}"
  if curl -fL --retry 2 --retry-delay 2 --connect-timeout 15 --max-time 600 \
      -C - -o "$tmp" "$url"; then
    local n
    n="$(wc -c <"$tmp" | tr -d ' ')"
    if [[ "$n" -ge "$MIN_BYTES" ]]; then
      mv -f "$tmp" "$OUT"
      echo "[download] OK ${n} bytes → ${OUT}"
      return 0
    fi
    echo "[download] WARN ${label}: only ${n} bytes (too small)" >&2
  else
    echo "[download] WARN ${label} failed" >&2
  fi
  rm -f "$tmp"
  return 1
}

MIRRORS=(
  "https://${ORIGIN_IP}${PATH_ON_SITE}|origin IP (direct)"
  "https://dl.hackme.tech${PATH_ON_SITE}|dl.hackme.tech (direct)"
  "https://hackme.tech${PATH_ON_SITE}|hackme.tech (CDN)"
)

for entry in "${MIRRORS[@]}"; do
  url="${entry%%|*}"
  label="${entry##*|}"
  extra=()
  max_time=600
  if [[ "$url" == https://hackme.tech* ]]; then
    max_time=20
  fi
  if [[ "$url" == https://${ORIGIN_IP}* ]]; then
    extra=(-H "Host: hackme.tech" -k)
  fi
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
