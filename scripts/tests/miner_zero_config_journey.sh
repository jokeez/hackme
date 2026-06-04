#!/usr/bin/env bash
# End-to-end: download like a miner, verify SHA256, unpack, one-click start (Linux).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SITE="${SITE_BASE:-https://hackme.tech}"
VERSION="${VERSION:-0.1.0-rc11l}"
REL="release_${VERSION}"
DIST_URL="$SITE/dist/$REL"
WORKDIR="${WORKDIR:-/tmp/hackme-miner-journey-$$}"
LOCAL_DIST="${LOCAL_DIST:-$ROOT/dist/$REL}"

mkdir -p "$WORKDIR"
trap 'rm -rf "$WORKDIR"' EXIT

fail=0
pass() { echo "[journey] PASS: $*"; }
fail_msg() { echo "[journey] FAIL: $*" >&2; fail=$((fail + 1)); }

fetch() {
  local name="$1"
  local url="$2"
  local dest="$WORKDIR/$name"
  if [[ -f "$LOCAL_DIST/$name" ]]; then
    cp -f "$LOCAL_DIST/$name" "$dest"
    echo "[journey] using local $name" >&2
  else
    echo "[journey] curl $url" >&2
    curl -fSL --max-time 600 -o "$dest" "$url"
  fi
  printf '%s\n' "$dest"
}

echo "[journey] workdir=$WORKDIR site=$SITE"

# --- Site smoke ---
bash "$ROOT/scripts/tests/public_site_smoke.sh" || fail_msg "public_site_smoke"

# --- Artifacts ---
SHA_FILE="$(fetch SHA256SUMS.txt "$DIST_URL/SHA256SUMS.txt")"
LINUX_TGZ="$(fetch "hackme_${VERSION}_linux.tar.gz" "$DIST_URL/hackme_${VERSION}_linux.tar.gz")"
WIN_ZIP="$(fetch "hackme_${VERSION}_windows_setup.zip" "$DIST_URL/hackme_${VERSION}_windows_setup.zip")"

verify_one_sha() {
  local file="$1" sums="$2"
  local exp got
  exp="$(awk -v n="$(basename "$file")" '$NF==n {print $1; exit}' "$sums")"
  got="$(sha256sum "$file" | awk '{print $1}')"
  [[ -n "$exp" && "$exp" == "$got" ]]
}

if verify_one_sha "$LINUX_TGZ" "$SHA_FILE"; then
  pass "linux SHA256"
else
  fail_msg "linux SHA256 mismatch ($(basename "$LINUX_TGZ"))"
fi
if verify_one_sha "$WIN_ZIP" "$SHA_FILE"; then
  pass "windows setup SHA256"
else
  fail_msg "windows setup SHA256 mismatch"
fi

# --- Linux unpack + layout ---
EXTRACT="$WORKDIR/linux-extract"
mkdir -p "$EXTRACT"
tar -xzf "$LINUX_TGZ" -C "$EXTRACT"
LINUX_DIR="$EXTRACT/linux"
for f in hackme pool.miner.token start_hackme_miner.sh setup_hackme_miner.sh workerpoh; do
  [[ -e "$LINUX_DIR/$f" ]] && pass "linux/$f present" || fail_msg "linux/$f missing"
done
[[ -x "$LINUX_DIR/hackme" ]] && pass "hackme executable" || fail_msg "hackme not executable"

POOL_LEN="$(wc -c <"$LINUX_DIR/pool.miner.token" | tr -d ' ')"
if [[ "$POOL_LEN" -gt 20 ]]; then
  pass "pool.miner.token length=$POOL_LEN"
else
  fail_msg "pool.miner.token too short ($POOL_LEN)"
fi

# --- Windows portable layout ---
WIN_EX="$WORKDIR/win-extract"
mkdir -p "$WIN_EX"
unzip -q "$WIN_ZIP" -d "$WIN_EX"
for f in hackme.exe pool.miner.token start_hackme_miner.bat setup_hackme_miner.bat workerpoh.exe; do
  [[ -f "$WIN_EX/$f" ]] && pass "windows/$f present" || fail_msg "windows/$f missing"
done

# --- ISO verify (local or HEAD only) ---
ISO_LOCAL="$LOCAL_DIST/HackMe-OS-${VERSION}-amd64.iso"
if [[ -f "$ISO_LOCAL" ]]; then
  bash "$ROOT/scripts/tests/verify_hackme_iso.sh" "$ISO_LOCAL" && pass "ISO verify_hackme_iso" || fail_msg "ISO verify"
else
  ISO_LEN="$(curl -sSI "$DIST_URL/HackMe-OS-${VERSION}-amd64.iso" | awk 'BEGIN{IGNORECASE=1} /^content-length:/ {print $2}' | tr -d '\r')"
  if [[ -n "${ISO_LEN:-}" && "$ISO_LEN" -gt 800000000 ]]; then
    pass "ISO on CDN size=$ISO_LEN"
  else
    fail_msg "ISO size check ($ISO_LEN)"
  fi
fi

# --- Linux miner start (short run) ---
if [[ "${SKIP_LIVE_MINER_START:-0}" != "1" ]]; then
  cd "$LINUX_DIR"
  bash setup_hackme_miner.sh
  [[ -f .env ]] && pass "setup wrote .env" || fail_msg "no .env after setup"
  HACKME_MINER_DAEMON=1 bash start_hackme_miner.sh
  sleep 8
  if curl -fsS http://127.0.0.1:8080/api/status >/dev/null 2>&1; then
    pass "node /api/status healthy"
  else
    fail_msg "node did not respond on :8080"
    tail -n 20 logs/hackme-node.log 2>/dev/null || true
  fi
  if [[ -f logs/hackme-node.pid ]]; then
    kill "$(cat logs/hackme-node.pid)" 2>/dev/null || true
  fi
  pkill -f "$LINUX_DIR/hackme" 2>/dev/null || true
fi

if [[ "$fail" -gt 0 ]]; then
  echo "[journey] $fail check(s) failed"
  exit 1
fi
echo "[journey] all checks passed"
