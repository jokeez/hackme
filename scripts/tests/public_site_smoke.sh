#!/usr/bin/env bash
# Curl smoke: hackme.tech pages + ISO headers.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SITE="${SITE_BASE:-https://hackme.tech}"
ISO_VER="$(tr -d ' \n\r' <"$ROOT/scripts/release/CURRENT_ISO_VERSION" 2>/dev/null || echo 0.1.0-rc11l)"
ISO_URL="${ISO_URL:-$SITE/dist/release_${ISO_VER}/HackMe-OS-${ISO_VER}-amd64.iso}"

fail=0
check() {
  local path="$1"
  local url="$SITE$path"
  local code
  code="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 90 "$url")"
  if [[ "$code" == "200" ]]; then
    echo "[site-smoke] PASS $path"
  else
    echo "[site-smoke] FAIL $path HTTP $code" >&2
    fail=$((fail + 1))
  fi
}

for p in / /index.html /downloads.html /contacts.html /security-rewards.html; do
  check "$p"
done

len="$(curl -sS -I --max-time 60 "$ISO_URL" | grep -i '^content-length:' | awk '{print $2}' | tr -d '\r')"
if [[ -z "$len" ]]; then
  # Some nginx configs omit Content-Length on HEAD; probe first byte via Range.
  hdr="$(curl -sS -I --max-time 60 -r 0-0 "$ISO_URL" | awk 'BEGIN{IGNORECASE=1} /^Content-Range:/ {print $0}')"
  if echo "$hdr" | grep -q '/'; then
    len="$(echo "$hdr" | sed -n 's|.*/\([0-9]*\).*|\1|p')"
  fi
fi
if [[ -n "$len" && "$len" -gt 800000000 ]]; then
  echo "[site-smoke] PASS ISO size=$len bytes"
else
  echo "[site-smoke] FAIL ISO size (${len:-missing}; expected >800MB) url=$ISO_URL" >&2
  fail=$((fail + 1))
fi

if [[ "$fail" -gt 0 ]]; then
  exit 1
fi
echo "[site-smoke] all checks passed"
