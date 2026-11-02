#!/usr/bin/env bash
# One-shot: move HMC onto the settler payer (primary node wallet) so unpaid can drain.
#
# Hub settler spends TREASURY_ADDR / node wallet (HMC-dfc…), NOT the legacy float (HMC-381…).
# Default fund seed is the operator treasury hex (maps to HMC-719… on hub) — not genesis mint.
#
#   DRY_RUN=1 bash scripts/ops/heal_settlement_primary_float.sh
#   CONFIRM=1 bash scripts/ops/heal_settlement_primary_float.sh
#   NODE_SSH=hackme-vps CONFIRM=1 bash scripts/ops/heal_settlement_primary_float.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
NODE_SSH="${NODE_SSH:-}"
DEPLOY="${NODE_DEPLOY_DIR:-/opt/hackme}"
DRY_RUN="${DRY_RUN:-1}"
CONFIRM="${CONFIRM:-0}"

if [[ -n "$NODE_SSH" ]]; then
  rsync -az "$ROOT/scripts/ops/heal_settlement_primary_float.sh" "$NODE_SSH:$DEPLOY/scripts/ops/"
  ssh -o BatchMode=yes "$NODE_SSH" \
    "DEPLOY='$DEPLOY' DRY_RUN='$DRY_RUN' CONFIRM='$CONFIRM' AMOUNT_HMC='${AMOUNT_HMC:-}' MIN_FLOAT_HMC='${MIN_FLOAT_HMC:-20}' BUFFER_HMC='${BUFFER_HMC:-5}' bash '$DEPLOY/scripts/ops/heal_settlement_primary_float.sh'"
  exit $?
fi

DEPLOY="${DEPLOY:-/opt/hackme}"
cd "$DEPLOY"
CHAIN_BASE="${CHAIN_BASE:-http://127.0.0.1:18080}"
PRIMARY="${SETTLEMENT_PRIMARY:-${TREASURY_ADDR:-HMC-dfc143732a1fc3bc}}"
FUND_SEED_HEX="${TREASURY_FUND_SEED_HEX:-${DEPLOY}/.secrets/hackme_treasury_ed25519_seed.hex}"
MIN_FLOAT_HMC="${MIN_FLOAT_HMC:-20}"
BUFFER_HMC="${BUFFER_HMC:-5}"

if [[ -f "${DEPLOY}/.env.settlement" ]]; then
  # shellcheck disable=SC1090
  set -a && source "${DEPLOY}/.env.settlement" && set +a
  PRIMARY="${SETTLEMENT_PRIMARY:-${TREASURY_ADDR:-$PRIMARY}}"
  FUND_SEED_HEX="${TREASURY_FUND_SEED_HEX:-$FUND_SEED_HEX}"
fi

bal() {
  curl -fsS --max-time 10 "${CHAIN_BASE}/api/address/$1" | jq -r '.balance_hmc // 0'
}
unpaid="$(curl -fsS --max-time 10 "${CHAIN_BASE}/api/worker/settlement" | jq -r '.fleet_unpaid_hmc // .total_unpaid_hmc // 0')"
prim="$(bal "$PRIMARY")"
need="$(python3 -c "import math; u=float('$unpaid'); m=float('$MIN_FLOAT_HMC'); b=float('$BUFFER_HMC'); p=float('$prim'); target=max(m, u+b); print(max(0.0, math.ceil((target-p)*100)/100))")"
if [[ -n "${AMOUNT_HMC:-}" ]]; then
  need="$AMOUNT_HMC"
fi

echo "[heal-primary] unpaid=${unpaid} primary=${PRIMARY} bal=${prim} need=${need} dry_run=${DRY_RUN}"
if python3 -c "import sys; sys.exit(0 if float(sys.argv[1])<=0 else 1)" "$need"; then
  echo "[heal-primary] OK already covered"
  exit 0
fi
if [[ ! -r "$FUND_SEED_HEX" ]]; then
  echo "[heal-primary] ERROR missing fund seed $FUND_SEED_HEX" >&2
  exit 1
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
tr -d '\r\n' <"$FUND_SEED_HEX" >"${tmp}/node_ed25519.seed"
chmod 600 "${tmp}/node_ed25519.seed"

SENDX=(go run ./cmd/sendtransfer)
if [[ -x "${DEPLOY}/bin/sendtransfer" ]]; then
  SENDX=("${DEPLOY}/bin/sendtransfer")
elif [[ -x "${ROOT}/bin/sendtransfer" ]]; then
  SENDX=("${ROOT}/bin/sendtransfer")
fi

if [[ "$DRY_RUN" == "1" || "$CONFIRM" != "1" ]]; then
  echo "[heal-primary] DRY_RUN — would: ${SENDX[*]} -data-dir <seed> -to $PRIMARY -amount-hmc $need"
  echo "[heal-primary] re-run with DRY_RUN=0 CONFIRM=1 to send"
  exit 0
fi

"${SENDX[@]}" \
  -data-dir "$tmp" \
  -to "$PRIMARY" \
  -amount-hmc "$need" \
  -base "$CHAIN_BASE" \
  -memo settlement_primary_heal

for _ in $(seq 1 24); do
  prim="$(bal "$PRIMARY")"
  if python3 -c "import sys; sys.exit(0 if float(sys.argv[1])>=float(sys.argv[2]) else 1)" "$prim" "$need"; then
    echo "[heal-primary] OK primary=${prim}"
    exit 0
  fi
  sleep 3
done
echo "[heal-primary] WARN transfer sent but primary bal=${prim} still below need=${need}" >&2
exit 0
