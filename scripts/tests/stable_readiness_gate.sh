#!/usr/bin/env bash
# Structural gate: refuse to claim a "stable" cut while known critical crutches remain.
#
# Always runs structural checks (release scripts fail-closed, soft-pickup tightened,
# dashboard honesty patterns). When VERSION is X.Y.Z (no -rc/-dev) or
# HACKME_CLAIM_STABLE=1, also fails on documented product blockers
# (unsigned L1 self-update, paper exchange as sole desk).
#
# Usage:
#   bash scripts/tests/stable_readiness_gate.sh
#   VERSION=0.1.0 HACKME_CLAIM_STABLE=1 bash scripts/tests/stable_readiness_gate.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

VERSION="${VERSION:-$(tr -d ' \n\r' <"$ROOT/scripts/release/CURRENT_VERSION" 2>/dev/null || true)}"
CLAIM="${HACKME_CLAIM_STABLE:-0}"
failures=0
blockers=()

is_stable_version() {
  [[ "$CLAIM" == "1" ]] && return 0
  [[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]
}

check_file_has() {
  local file="$1" needle="$2" label="$3"
  if [[ ! -f "$file" ]]; then
    fail_msg "missing file for $label: $file"
    failures=$((failures + 1))
    return
  fi
  if grep -qE "$needle" "$file"; then
    pass "$label"
  else
    fail_msg "$label (need /$needle/ in $(basename "$file"))"
    failures=$((failures + 1))
  fi
}

check_file_lacks() {
  local file="$1" needle="$2" label="$3"
  if [[ ! -f "$file" ]]; then
    fail_msg "missing file for $label: $file"
    failures=$((failures + 1))
    return
  fi
  if grep -qE "$needle" "$file"; then
    fail_msg "$label still present in $(basename "$file")"
    failures=$((failures + 1))
  else
    pass "$label absent"
  fi
}

fail_msg() { printf '[FAIL] %s\n' "$*" >&2; }

echo "[stable-readiness] VERSION=$VERSION CLAIM_STABLE=$CLAIM"

# --- Structural (always) ---
python3 "$ROOT/scripts/tests/lib/soft_pickup_gate.py" || {
  fail_msg "soft_pickup_gate self-check"
  failures=$((failures + 1))
}

check_file_has "$ROOT/scripts/release/make_release_bundle.sh" \
  'workerpoh-opencl\.exe.*missing|ERROR:.*workerpoh-opencl\.exe' \
  "release fails closed without Windows OpenCL"

check_file_has "$ROOT/scripts/release/make_release_bundle.sh" \
  'assert_clean_release_tree\.sh' \
  "bundle invokes dirty-tree gate"

check_file_has "$ROOT/scripts/release/make_release_bundle.sh" \
  'vcs\.modified' \
  "bundle rejects vcs.modified binaries"

check_file_has "$ROOT/scripts/tests/lib/soft_pickup_gate.py" \
  'HACKME_SOFT_PICKUP_REASON' \
  "soft pickup requires reason"

check_file_has "$ROOT/scripts/tests/rc20_customer_pool_e2e.sh" \
  'soft_pickup_gate' \
  "pool E2E uses shared soft-pickup gate"

check_file_has "$ROOT/dashboard.html" \
  'hub_prepaid' \
  "dashboard honors hub prepaid source"

check_file_lacks "$ROOT/dashboard.html" \
  "title=\"Live HMC/USDT from exchange\.hackme\.tech\"" \
  "wallet chip must not claim Live CEX price"

check_file_has "$ROOT/dashboard.html" \
  'Paper ref mid|PAPER · REF MID|paper ref' \
  "wallet/exchange paper labeling present"

check_file_has "$ROOT/scripts/release/run_full_release_local.sh" \
  'stable_readiness_gate\.sh' \
  "full release runs stable readiness gate"

# Installer must require OpenCL (no skipifsourcedoesntexist on opencl).
if grep -E 'workerpoh-opencl\.exe.*skipifsourcedoesntexist' \
  "$ROOT/scripts/release/windows/hackme.iss" >/dev/null 2>&1; then
  fail_msg "hackme.iss still skips missing workerpoh-opencl.exe"
  failures=$((failures + 1))
else
  pass "installer requires workerpoh-opencl.exe"
fi

# --- Product blockers when claiming stable ---
if is_stable_version; then
  echo "[stable-readiness] claiming stable — evaluating product blockers"

  # L1 self-update must verify ed25519 latest.json.sig and default to require for stable.
  if ! grep -qE 'ed25519.*sig|pkeyutl.*-verify' "$ROOT/scripts/ops/update_hackme_miner.sh"; then
    blockers+=("L1 self-update lacks ed25519 latest.json.sig verify — prefer signed apt L3 or ship detached sig")
  fi
  if ! grep -qE 'HACKME_UPDATE_SIG_MODE:-require' "$ROOT/scripts/ops/update_hackme_miner.sh"; then
    blockers+=("L1 HACKME_UPDATE_SIG_MODE must default to require before tagging stable (rc may keep warn)")
  fi
  if [[ ! -f "$ROOT/scripts/release/keys/hackme_update_ed25519.pub.pem" ]]; then
    blockers+=("missing committed scripts/release/keys/hackme_update_ed25519.pub.pem")
  fi

  # Paper exchange as the only desk is OK only if clearly labeled; claiming
  # "live CEX" or omitting paper/D0 in release notes is a blocker.
  if [[ -f "$ROOT/docs/releases/HACKME_${VERSION}_RELEASE.md" ]]; then
    if ! grep -qiE 'paper|D0' "$ROOT/docs/releases/HACKME_${VERSION}_RELEASE.md"; then
      blockers+=("stable release notes must label exchange as paper/D0")
    fi
  else
    blockers+=("missing docs/releases/HACKME_${VERSION}_RELEASE.md with paper/D0 disclosure")
  fi

  # Soft pickup must never be greenlit for stable CI.
  blockers+=("money-path E2E must pass without ALLOW_SOFT_PICKUP on public hub before stable tag")

  if [[ "${#blockers[@]}" -gt 0 ]]; then
    for b in "${blockers[@]}"; do
      fail_msg "stable blocker: $b"
      failures=$((failures + 1))
    done
  fi
else
  echo "[stable-readiness] prerelease ($VERSION) — product blockers deferred (structural only)"
fi

if [[ "$failures" -gt 0 ]]; then
  fail "stable_readiness_gate FAIL ($failures) — do not tag stable"
fi
pass "stable_readiness_gate PASS (VERSION=$VERSION)"
