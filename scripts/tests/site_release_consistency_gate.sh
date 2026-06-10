#!/usr/bin/env bash
# Verify hackme.tech site pages and CURRENT_VERSION download URLs match assets/app.js RELEASE_VER.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SITE="${SITE_BASE:-https://hackme.tech}"
VER="$(grep -oE 'RELEASE_VER = "[^"]+"' "$ROOT/web/site/assets/app.js" | sed 's/.*"\([^"]*\)".*/\1/')"
ISO_VER="$(tr -d ' \n\r' <"$ROOT/scripts/release/CURRENT_ISO_VERSION" 2>/dev/null || echo 0.1.0-rc11l)"
[[ -n "$VER" ]] || { echo "[site-release] FAIL: no RELEASE_VER in app.js" >&2; exit 1; }
BASE="${SITE}/dist/release_${VER}"
ISO_BASE="${SITE}/dist/release_${ISO_VER}"
fail=0
check_head() {
  local name="$1" url="$2" min="${3:-1}"
  local code len
  code="$(curl -sS -o /dev/null -w '%{http_code}' -I --max-time 30 "$url" || echo 000)"
  len="$(curl -sS -I --max-time 30 "$url" 2>/dev/null | grep -i '^content-length:' | awk '{print $2}' | tr -d '\r' | head -1)"
  if [[ "$code" == "200" && -n "$len" && "$len" -ge "$min" ]]; then
    echo "[pass] $name HTTP $code Content-Length=$len"
    return 0
  fi
  echo "[fail] $name HTTP $code len=${len:-?} url=$url" >&2
  fail=$((fail + 1))
  return 1
}
check_get_small() {
  local name="$1" url="$2" min="${3:-1}"
  local code len
  code="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 30 "$url" || echo 000)"
  len="$(curl -sS -o /dev/null -w '%{size_download}' --max-time 30 "$url" 2>/dev/null || echo 0)"
  if [[ "$code" == "200" && "${len:-0}" -ge "$min" ]]; then
    echo "[pass] $name HTTP $code size=$len"
    return 0
  fi
  echo "[fail] $name HTTP $code size=$len url=$url" >&2
  fail=$((fail + 1))
  return 1
}
echo "[site-release] RELEASE_VER=$VER"
for p in / /index.html /downloads.html /news.html /contacts.html /developers.html /docs.html /coins.html /economics-model.html /operator-checklist.html /security-rewards.html /fuzz-guide.html; do
  code="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 20 "${SITE}${p}")" || code=000
  if [[ "$code" == "200" ]]; then echo "[pass] page $p"; else echo "[fail] page $p HTTP $code" >&2; fail=$((fail+1)); fi
done
js="$(curl -fsS --max-time 15 "${SITE}/assets/app.js" | grep -oE 'RELEASE_VER = "[^"]+"' || true)"
if [[ "$js" == *"$VER"* ]]; then echo "[pass] prod app.js RELEASE_VER"; else echo "[fail] prod app.js mismatch: $js" >&2; fail=$((fail+1)); fi
check_head "installer" "${BASE}/HackMe-Setup-${VER}.exe" 5000000
check_head "linux" "${BASE}/hackme_${VER}_linux.tar.gz" 5000000
check_head "fuzzing_win" "${BASE}/hackme-fuzzing-${VER}-windows-amd64.exe" 5000000
check_head "fuzzing_linux" "${BASE}/hackme-fuzzing-${VER}-linux-amd64" 5000000
check_get_small "sha256" "${BASE}/SHA256SUMS.txt" 100
check_get_small "manifest" "${BASE}/RELEASE_MANIFEST.json" 100
check_head "iso" "${ISO_BASE}/HackMe-OS-${ISO_VER}-amd64.iso" 800000000
lite="$(curl -fsS --max-time 15 "${SITE}/api/status?lite=1" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('version',''),d.get('commit','')[:12])" 2>/dev/null || echo FAIL)"
echo "[site-release] api/status?lite=1 → $lite"
if [[ "$fail" -gt 0 ]]; then echo "[site-release] FAIL ($fail checks)" >&2; exit 1; fi
echo "[site-release] PASS — site + ${VER} artifacts consistent"
