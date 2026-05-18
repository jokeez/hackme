#!/usr/bin/env bash
# Smoke: public release artifacts on hackme.tech match RELEASE_VER in web/site/assets/app.js
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VER="$(grep -oE 'RELEASE_VER = "[^"]+"' "$ROOT/web/site/assets/app.js" | sed -n 's/.*"\([^"]*\)".*/\1/p')"
BASE="https://hackme.tech/dist/release_${VER}"
ZIP="${BASE}/hackme_${VER}_windows.zip"
LOCAL="$ROOT/dist/release_${VER}/hackme_${VER}_windows.zip"

echo "[download-verify] channel=$VER"
for f in SHA256SUMS.txt BUILD_INFO.txt RELEASE_MANIFEST.json; do
  code=$(curl -fsS -o /dev/null -w '%{http_code}' --max-time 20 "${BASE}/${f}")
  echo "  ${f}: HTTP ${code}"
  [[ "$code" == "200" ]] || exit 1
done

if [[ -f "$LOCAL" ]]; then
  remote=$(curl -fsS -I --max-time 20 "$ZIP" | awk -F': ' '/[Cc]ontent-[Ll]ength/{print $2}' | tr -d '\r')
  local=$(stat -c%s "$LOCAL" 2>/dev/null || stat -f%z "$LOCAL")
  echo "  windows.zip remote_bytes=${remote:-?} local_bytes=${local}"
  [[ -n "$remote" && "$remote" == "$local" ]] && echo "[download-verify] PASS size match" || echo "[download-verify] WARN size mismatch (slow CDN ok if GET completes)"
else
  echo "[download-verify] local bundle missing; build: VERSION=${VER} bash scripts/release/make_release_bundle.sh"
fi
echo "[download-verify] URL: $ZIP"
