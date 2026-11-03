#!/usr/bin/env bash
# Windows miner OOTB policy checklist (docs + release honesty). No Hunt engines promised.
#
#   bash scripts/ops/windows_miner_ootb_checklist.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
fail=0

check() {
  local label="$1" file="$2" pat_bad="$3"
  if [[ ! -f "$file" ]]; then
    echo "[win-ootb] MISS $label ($file)"
    fail=1
    return
  fi
  if grep -nEi "$pat_bad" "$file" >/dev/null 2>&1; then
    echo "[win-ootb] FAIL $label — still implies Hunt engines on Windows:"
    grep -nEi "$pat_bad" "$file" | head -5
    fail=1
  else
    echo "[win-ootb] OK $label"
  fi
}

require() {
  local label="$1" file="$2" pat_good="$3"
  if [[ ! -f "$file" ]]; then
    echo "[win-ootb] MISS $label ($file)"
    fail=1
    return
  fi
  if ! grep -qiE "$pat_good" "$file"; then
    echo "[win-ootb] FAIL $label — missing required honesty note ($pat_good)"
    fail=1
  else
    echo "[win-ootb] OK $label"
  fi
}

# Orders / Setup must not promise local Hunt engines on Windows.
check "orders.bat" scripts/release/windows/start_hackme_orders.bat 'local Dig/Hunt|for Dig / Hunt'
check "iss-welcome" scripts/release/windows/hackme.iss 'Orders \(B2B Dig/Hunt\)'
check "downloads" web/site/downloads.html 'Orders</strong> \(B2B Dig/Hunt\)'
check "installer-after-wsl2" scripts/release/windows/INSTALLER_AFTER.txt 'enable Hunt via WSL2'
check "one-click-program-files-default" docs/MINER_WINDOWS_ONE_CLICK.md 'installation in `C:\\Program Files\\HackMe` \(default'
# Dashboard still ships Dig/Hunt hero for Linux; require Windows honesty helper.
require "dashboard-win-honesty" dashboard.html 'function applyWindowsDigHonesty'
require "dashboard-win-hunt-linux" dashboard.html 'Hunt engines = .*Linux pool|Hunt = Linux miners'

# Require explicit Linux-only Hunt note; reject only clear false promises.
if ! grep -qi 'Linux-only' docs/MINER_WINDOWS_ONE_CLICK.md; then
  echo "[win-ootb] FAIL one-click doc — missing Linux-only Hunt note"
  fail=1
elif grep -nEi 'Hunt engines (are )?installed on Windows|Windows (miner )?installs (clang|libFuzzer|AFL\+\+)' docs/MINER_WINDOWS_ONE_CLICK.md >/dev/null 2>&1; then
  echo "[win-ootb] FAIL one-click doc — promises Hunt engines on Windows"
  fail=1
else
  echo "[win-ootb] OK one-click doc"
fi
require "one-click-localappdata" docs/MINER_WINDOWS_ONE_CLICK.md 'LOCALAPPDATA'
# Positive: workerfuzz required in ISS (no skipifsourcedoesntexist on workerfuzz line).
if grep -n 'workerfuzz.exe' scripts/release/windows/hackme.iss | grep -i skipifsourcedoesntexist >/dev/null; then
  echo "[win-ootb] FAIL hackme.iss workerfuzz still skipifsourcedoesntexist"
  fail=1
else
  echo "[win-ootb] OK hackme.iss workerfuzz required"
fi

# Orders must force settle-pull=1 (miner hackme.env seeds 0; escrow won't close otherwise).
if ! grep -q 'HACKME_FUZZ_SETTLE_PULL=1' scripts/release/windows/start_hackme_orders.bat; then
  echo "[win-ootb] FAIL orders.bat — missing HACKME_FUZZ_SETTLE_PULL=1 repair/seed"
  fail=1
else
  echo "[win-ootb] OK orders.bat settle-pull"
fi

# Example publish token path shipped for Operators (never a live token).
if ! grep -q 'pool.orders.token.example' scripts/release/windows/hackme.iss; then
  echo "[win-ootb] FAIL hackme.iss — missing pool.orders.token.example"
  fail=1
else
  echo "[win-ootb] OK hackme.iss pool.orders.token.example"
fi

# Public pool still up (Windows miners join here for Dig/PoH).
# CI / offline: SKIP_PUBLIC_STATUS=1
if [[ "${SKIP_PUBLIC_STATUS:-0}" == "1" ]]; then
  echo "[win-ootb] SKIP public status (SKIP_PUBLIC_STATUS=1)"
else
  curl -fsS --max-time 20 -A 'Mozilla/5.0 (HackMe-win-ootb)' https://hackme.tech/api/status >/dev/null
  echo "[win-ootb] OK public status"
fi

if [[ "$fail" != "0" ]]; then
  exit 1
fi
echo "[win-ootb] PASS"
