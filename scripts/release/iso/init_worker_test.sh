#!/usr/bin/env bash
# Unit tests for HackMe OS init-worker.sh (corrupt ini, binary garbage, defaults).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
INIT="$ROOT/scripts/release/iso/init-worker.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

export HACKME_INIT_ENV="$TMP/miner.env"
export HACKME_INIT_LOG="$TMP/init.log"
export HACKME_OS_DEFAULT_WALLET="HMC-719006d93916ad52"

run_case() {
  local name="$1" ini="$2" expect_wallet="${3:-}"
  rm -f "$HACKME_INIT_ENV" "$HACKME_INIT_LOG"
  export HACKME_INI="$ini"
  bash "$INIT" >/dev/null
  if [[ -n "$expect_wallet" ]]; then
    grep -q "HACKME_PAYOUT_WALLET=$expect_wallet" "$HACKME_INIT_ENV" || {
      echo "FAIL $name: missing wallet $expect_wallet"
      cat "$HACKME_INIT_ENV"
      exit 1
    }
  fi
  echo "PASS $name"
}

# Missing ini → default fund wallet
run_case "missing_ini" "$TMP/none.ini" "HMC-719006d93916ad52"

# Valid ini
cat >"$TMP/good.ini" <<'EOF'
# rig
wallet=HMC-91fe007e4036c602
pool=https://hackme.tech/pool/coordinator
EOF
run_case "good_ini" "$TMP/good.ini" "HMC-91fe007e4036c602"

# Special chars / empty lines / comments
cat >"$TMP/messy.ini" <<'EOF'

# comment only

wallet=   HMC-abcdef0123456789   

;;;;;
EOF
run_case "messy_ini" "$TMP/messy.ini" "HMC-abcdef0123456789"

# Invalid wallet → default
cat >"$TMP/badwallet.ini" <<'EOF'
wallet=not-a-wallet
wallet=HMC-TOOSHORT
EOF
run_case "bad_wallet" "$TMP/badwallet.ini" "HMC-719006d93916ad52"

# Binary garbage file
printf '\x00\x01\x02\xff\xfe' >"$TMP/binary.ini"
run_case "binary_ini" "$TMP/binary.ini" "HMC-719006d93916ad52"

echo "init_worker_test: all PASS"
