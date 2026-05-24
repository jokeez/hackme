#!/usr/bin/env bash
# Full release + site + miner-path verification (run before deploy).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VERSION="${VERSION:-0.1.0-rc11g}"
REL_DIR="${REL_DIR:-$ROOT/dist/release_${VERSION}}"
SITE="${SITE_BASE:-https://hackme.tech}"
VERIFY_CDN="${VERIFY_CDN:-1}"

fail=0
run() {
  echo ""
  echo "======== $* ========"
  if "$@"; then
    echo "[full-check] PASS: $*"
  else
    echo "[full-check] FAIL: $*" >&2
    fail=$((fail + 1))
  fi
}

run bash "$ROOT/scripts/release/verify_artifacts.sh" "$REL_DIR"
run bash "$ROOT/scripts/release/smoke_artifacts.sh" "$REL_DIR"

ISO="$REL_DIR/HackMe-OS-${VERSION}-amd64.iso"
if [[ -f "$ISO" ]]; then
  run bash "$ROOT/scripts/tests/verify_hackme_iso.sh" "$ISO"
  run bash "$ROOT/scripts/tests/iso_qemu_boot_smoke.sh" "$ISO"
else
  echo "[full-check] WARN: ISO missing at $ISO" >&2
  fail=$((fail + 1))
fi

run bash "$ROOT/scripts/tests/public_site_smoke.sh"

if [[ "$VERIFY_CDN" == "1" ]]; then
  echo ""
  echo "======== CDN SHA256 vs manifest ========"
  TMP="$(mktemp -d)"
  trap 'rm -rf "$TMP"' EXIT
  curl -fsSL --max-time 60 "$SITE/dist/release_${VERSION}/SHA256SUMS.txt" -o "$TMP/SHA256SUMS.txt"
  curl -fsSL --max-time 60 "$SITE/dist/release_${VERSION}/SHA256SUMS-iso.txt" -o "$TMP/SHA256SUMS-iso.txt"
  curl -fsSL --max-time 60 "$SITE/dist/release_${VERSION}/RELEASE_MANIFEST.json" -o "$TMP/RELEASE_MANIFEST.json"
  # CDN binary check via VPS (fast LAN) or skip if unreachable.
  cdn_exe="HackMe-Setup-${VERSION}.exe"
  exp="$(awk -v n="$cdn_exe" '$NF==n {print $1}' "$TMP/SHA256SUMS.txt")"
  if [[ -n "${NODE_SSH:-}" ]] && command -v ssh >/dev/null 2>&1; then
    got="$(ssh -o BatchMode=yes -o ConnectTimeout=15 "$NODE_SSH" \
      "sha256sum /opt/hackme/dist/release_${VERSION}/$cdn_exe 2>/dev/null" | awk '{print $1}')"
    if [[ -n "$exp" && "$exp" == "$got" ]]; then
      echo "[full-check] PASS VPS $cdn_exe sha256"
    else
      echo "[full-check] FAIL VPS $cdn_exe sha256 exp=$exp got=${got:-?}" >&2
      fail=$((fail + 1))
    fi
  else
    echo "[full-check] SKIP CDN binary dl (set NODE_SSH=hackme-vps for VPS sha check)"
  fi
  if [[ -f "$REL_DIR/hackme_${VERSION}_linux.tar.gz" ]]; then
    exp_linux="$(awk '/linux\.tar\.gz$/ {print $1}' "$TMP/SHA256SUMS.txt")"
    got_linux="$(sha256sum "$REL_DIR/hackme_${VERSION}_linux.tar.gz" | awk '{print $1}')"
    if [[ "$exp_linux" == "$got_linux" ]]; then
      echo "[full-check] PASS local linux matches CDN SHA256SUMS"
    else
      echo "[full-check] FAIL local linux vs CDN sums" >&2
      fail=$((fail + 1))
    fi
  fi
  iso_name="HackMe-OS-${VERSION}-amd64.iso"
  iso_exp="$(awk -v n="$iso_name" '$NF==n {print $1}' "$TMP/SHA256SUMS-iso.txt")"
  man_iso="$(jq -r '.artifacts[]|select(.platform=="hackme-os")|.sha256' "$TMP/RELEASE_MANIFEST.json")"
  if [[ -n "$iso_exp" && "$iso_exp" == "$man_iso" ]]; then
    echo "[full-check] PASS manifest ISO sha matches CDN"
  else
    echo "[full-check] FAIL manifest ISO sha ($man_iso) vs CDN ($iso_exp)" >&2
    fail=$((fail + 1))
  fi
  if [[ -f "$ISO" ]] && grep -aqE 'noplymouth.*console=tty1' "$ISO" 2>/dev/null; then
    echo "[full-check] PASS ISO has noplymouth console boot params"
  else
    echo "[full-check] WARN: could not confirm noplymouth in ISO (re-download head)" >&2
  fi
fi

# Local miner journey (no CDN download for archives)
VERIFY_CDN=0 NODE_SSH="${NODE_SSH:-}" env LOCAL_DIST="$REL_DIR" bash "$ROOT/scripts/tests/miner_zero_config_journey.sh"

if [[ "$fail" -gt 0 ]]; then
  echo "[full-check] $fail suite(s) failed"
  exit 1
fi
echo "[full-check] all suites passed"
