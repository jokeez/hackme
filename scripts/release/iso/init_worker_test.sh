#!/usr/bin/env bash
# Unit tests for HackMe OS init-worker.sh (corrupt ini, binary garbage, Zero-Knowledge).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
INIT="$ROOT/scripts/release/iso/init-worker.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

export HACKME_INIT_ENV="$TMP/miner.env"
export HACKME_INIT_LOG="$TMP/init.log"
export HACKME_STATE_DIR="$TMP/state"
export HACKME_RUN_DIR="$TMP/run"
mkdir -p "$HACKME_STATE_DIR" "$HACKME_RUN_DIR"
export HACKME_OS_DEFAULT_WALLET="HMC-719006d93916ad52"
export HACKME_ROOT="$ROOT"
export HACKME_OS_ZERO_KNOWLEDGE=0

run_case() {
  local name="$1" ini="$2" expect_wallet="${3:-}"
  rm -f "$HACKME_INIT_ENV" "$HACKME_INIT_LOG" /run/hackme-os/zk-wallet.json 2>/dev/null || true
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

# Legacy fallback when ZK disabled (CI / no minersign in tree)
run_case "missing_ini" "$TMP/none.ini" "HMC-719006d93916ad52"

cat >"$TMP/good.ini" <<'EOF'
wallet=HMC-91fe007e4036c602
pool=https://hackme.tech/pool/coordinator
EOF
run_case "good_ini" "$TMP/good.ini" "HMC-91fe007e4036c602"

cat >"$TMP/messy.ini" <<'EOF'
wallet=   HMC-abcdef0123456789
EOF
run_case "messy_ini" "$TMP/messy.ini" "HMC-abcdef0123456789"

cat >"$TMP/badwallet.ini" <<'EOF'
wallet=HMC-TOOSHORT
EOF
run_case "bad_wallet" "$TMP/badwallet.ini" "HMC-719006d93916ad52"

printf '\x00\x01\x02\xff\xfe' >"$TMP/binary.ini"
run_case "binary_ini" "$TMP/binary.ini" "HMC-719006d93916ad52"

# Zero-Knowledge: empty ini template → new wallet + seed
if [[ -x "$ROOT/minersign" ]] || [[ -x "$ROOT/bin/minersign" ]]; then
  [[ -x "$ROOT/bin/minersign" ]] || ln -sf "$ROOT/minersign" "$ROOT/bin/minersign" 2>/dev/null || cp -f "$ROOT/minersign" "$ROOT/bin/minersign" 2>/dev/null || true
  if [[ ! -x "$ROOT/bin/minersign" ]] && command -v go >/dev/null 2>&1; then
    go build -o "$ROOT/bin/minersign" ./cmd/minersign 2>/dev/null || true
  fi
fi
if [[ -x "$ROOT/bin/minersign" ]] && command -v jq >/dev/null 2>&1; then
  export HACKME_OS_ZERO_KNOWLEDGE=1
  export HACKME_INI="$TMP/zk.ini"
  : >"$TMP/zk.ini"
  rm -f "$HACKME_INIT_ENV"
  bash "$INIT" >/dev/null
  w="$(grep '^HACKME_PAYOUT_WALLET=' "$HACKME_INIT_ENV" | cut -d= -f2)"
  if [[ ! "$w" =~ ^HMC-[0-9a-fA-F]{16}$ ]] || [[ "$w" == "HMC-719006d93916ad52" ]]; then
    echo "FAIL zk_empty_ini: unexpected wallet $w"
    exit 1
  fi
  grep -q '^HACKME_MINER_ED25519_SEED_HEX=' "$HACKME_INIT_ENV" || {
    echo "FAIL zk_empty_ini: missing seed"
    exit 1
  }
  [[ -f "$TMP/zk.ini" ]] && grep -q "^wallet=$w" "$TMP/zk.ini" || {
    echo "FAIL zk_empty_ini: ini not written ($(ls -la "$TMP/zk.ini" 2>/dev/null || echo missing))"
    exit 1
  }
  echo "PASS zk_empty_ini"
else
  echo "SKIP zk_empty_ini (minersign/jq unavailable)"
fi

echo "init_worker_test: all PASS"
