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

# Orders / Setup must not promise local Hunt engines on Windows.
check "orders.bat" scripts/release/windows/start_hackme_orders.bat 'local Dig/Hunt|for Dig / Hunt'
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
# Positive: workerfuzz required in ISS (no skipifsourcedoesntexist on workerfuzz line).
if grep -n 'workerfuzz.exe' scripts/release/windows/hackme.iss | grep -i skipifsourcedoesntexist >/dev/null; then
  echo "[win-ootb] FAIL hackme.iss workerfuzz still skipifsourcedoesntexist"
  fail=1
else
  echo "[win-ootb] OK hackme.iss workerfuzz required"
fi

# Public pool still up (Windows miners join here for Dig/PoH).
curl -fsS --max-time 20 -A 'Mozilla/5.0 (HackMe-win-ootb)' https://hackme.tech/api/status >/dev/null
echo "[win-ootb] OK public status"

if [[ "$fail" != "0" ]]; then
  exit 1
fi
echo "[win-ootb] PASS"
