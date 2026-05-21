#!/usr/bin/env bash
# HackMe OS worker init — safe parse of hackme.ini (wallet, pool URL, rig profile).
# Never aborts boot; falls back to treasury default on corrupt input.
set -euo pipefail

INI="${HACKME_INI:-/etc/hackme/hackme.ini}"
OUT_ENV="${HACKME_INIT_ENV:-/var/lib/hackme/miner.env}"
DEFAULT_WALLET="${HACKME_OS_DEFAULT_WALLET:-HMC-719006d93916ad52}"
DEFAULT_POOL="${HACKME_OS_DEFAULT_POOL:-https://hackme.tech/pool/coordinator}"
LOG="${HACKME_INIT_LOG:-/var/log/hackme-init-worker.log}"

mkdir -p "$(dirname "$OUT_ENV")" "$(dirname "$LOG")"
log() { echo "[hackme-init] $*" | tee -a "$LOG"; }

# HMC- + 16 hex (consensus dev/treasury format)
valid_hmc() {
  local a="$1"
  [[ "$a" =~ ^HMC-[0-9a-fA-F]{16}$ ]]
}

# Reject control chars and non-printable (binary garbage in ini).
sanitized_line() {
  local line="$1"
  line="${line//$'\r'/}"
  line="$(printf '%s' "$line" | tr -cd '[:print:]')"
  printf '%s' "$line"
}

read_ini_wallet() {
  local wallet=""
  if [[ ! -f "$INI" ]]; then
    return 1
  fi
  if command -v file >/dev/null 2>&1; then
    if ! file -b "$INI" 2>/dev/null | grep -qiE 'text|ascii|empty|ini'; then
      log "WARN: $INI looks binary — using default wallet"
      return 1
    fi
  elif grep -q $'\x00' "$INI" 2>/dev/null; then
    log "WARN: $INI contains NUL bytes — using default wallet"
    return 1
  fi
  while IFS= read -r raw || [[ -n "$raw" ]]; do
    local line
    line="$(sanitized_line "$raw")"
    [[ -z "$line" || "$line" =~ ^[[:space:]]*# ]] && continue
    case "$line" in
      wallet=*|payout=*|miner_address=*|HACKME_WALLET=*)
        local val="${line#*=}"
        val="$(printf '%s' "$val" | tr -d '[:space:]')"
        if valid_hmc "$val"; then
          wallet="$val"
        fi
        ;;
    esac
  done <"$INI"
  if [[ -n "$wallet" ]]; then
    printf '%s' "$wallet"
    return 0
  fi
  return 1
}

WALLET="$DEFAULT_WALLET"
if w="$(read_ini_wallet 2>/dev/null)"; then
  WALLET="$w"
  log "wallet from ini: $WALLET"
else
  log "wallet default (fund): $WALLET"
fi

POOL_URL="$DEFAULT_POOL"
if [[ -f "$INI" ]]; then
  while IFS= read -r raw || [[ -n "$raw" ]]; do
    line="$(sanitized_line "$raw")"
    [[ -z "$line" || "$line" =~ ^[[:space:]]*# ]] && continue
    case "$line" in
      pool=*|coord=*|COORD_URL=*)
        val="${line#*=}"
        val="$(printf '%s' "$val" | tr -d '[:space:]')"
        if [[ "$val" =~ ^https?:// ]]; then
          POOL_URL="$val"
        fi
        ;;
    esac
  done <"$INI"
fi

# Merge with existing state (firstboot seed) without clobbering secrets.
touch "$OUT_ENV"
if ! grep -q '^WORKER_ID=' "$OUT_ENV" 2>/dev/null; then
  echo "WORKER_ID=worker-$(hostname -s 2>/dev/null || echo os)-$(openssl rand -hex 2 2>/dev/null || echo 00)" >>"$OUT_ENV"
fi
{
  echo "# init-worker.sh $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "HACKME_PAYOUT_WALLET=$WALLET"
  echo "COORD_URL=$POOL_URL"
  echo "HACKME_OS_INIT=1"
} >>"$OUT_ENV"
chmod 600 "$OUT_ENV"
log "init OK env=$OUT_ENV pool=$POOL_URL"
exit 0
