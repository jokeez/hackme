#!/usr/bin/env bash
# Verify pool accrual, settlement API, hybrid signer, and optional growth over a short window.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
DESKTOP_ENV="${DESKTOP_ENV_FILE:-$ROOT/.env.desktop}"
BASE="${BASE_URL:-http://127.0.0.1:8080}"
EXPECTED_ADDR="${EXPECTED_WALLET:-HMC-91fe007e4036c602}"
WATCH_SEC="${WATCH_SEC:-0}"

[[ -f "$DESKTOP_ENV" ]] || { echo "[accrual-audit] missing $DESKTOP_ENV" >&2; exit 2; }
set -a
# shellcheck disable=SC1090
. "$DESKTOP_ENV"
set +a

fail() { echo "[accrual-audit] FAIL: $*" >&2; exit 1; }
ok() { echo "[accrual-audit] OK: $*"; }

echo "[accrual-audit] node + wallet address"
st="$(curl -fsS --max-time 10 "$BASE/api/status?lite=1")"
node_addr="$(printf '%s' "$st" | python3 -c 'import sys,json; print(json.load(sys.stdin).get("node_address",""))')"
[[ "$node_addr" == "$EXPECTED_ADDR" ]] || fail "node_address=$node_addr want=$EXPECTED_ADDR"
ok "node_address=$node_addr"

echo "[accrual-audit] hybrid seed / data dir"
bash "$ROOT/scripts/ops/desktop_verify_miner_address.sh" "$EXPECTED_ADDR"

fetch_coord_payout() {
  curl -fsS --max-time 15 'https://hackme.tech/pool/coordinator/api/work/stats?details=1' \
    | python3 -c 'import sys,json; d=json.load(sys.stdin); print(float(d.get("total_payout_hmc") or 0))'
}

fetch_settlement() {
  curl -fsS --max-time 10 "$BASE/api/worker/settlement"
}

echo "[accrual-audit] coordinator + settlement snapshot"
coord1="$(fetch_coord_payout)"
settle1="$(fetch_settlement)"
SETTLE_JSON="$settle1" python3 - <<'PY'
import json,os
s=json.loads(os.environ["SETTLE_JSON"])
if not s.get("ok"):
    raise SystemExit("settlement not ok: "+str(s))
wu=float(s.get("wallet_unpaid_hmc") or s.get("total_unpaid_hmc") or 0)
cp=float(s.get("coordinator_total_payout_hmc") or 0)
ls=s.get("last_signed_miner_address") or ""
src=s.get("accrual_source") or ""
print(f"  wallet_unpaid_hmc={wu:.6f} coordinator_total={cp:.6f} last_signer={ls} source={src}")
if wu <= 0:
    raise SystemExit("wallet unpaid is zero — check WORKER_PAYOUT_MAP / worker / last_signer")
if cp > 0 and abs(wu - cp) > max(0.02, cp * 0.15):
    print(f"  WARN: wallet unpaid differs from coordinator total by >15% (multi-worker pool?)")
PY
ok "coordinator total_payout_hmc=$coord1"

ws="$(curl -fsS --max-time 10 "$BASE/api/worker/status")"
running="$(printf '%s' "$ws" | python3 -c 'import sys,json; print(1 if json.load(sys.stdin).get("running") else 0)')"
if [[ "$running" != "1" ]]; then
  echo "[accrual-audit] WARN: GPU worker not running — start: bash scripts/ops/desktop_worker_reset.sh"
else
  ok "worker running"
fi

if [[ "$WATCH_SEC" -gt 0 ]]; then
  echo "[accrual-audit] watching ${WATCH_SEC}s for accrual growth..."
  sleep "$WATCH_SEC"
  coord2="$(fetch_coord_payout)"
  settle2="$(fetch_settlement)"
  SETTLE1="$settle1" SETTLE2="$settle2" python3 - "$coord1" "$coord2" <<'PY'
import os,sys,json
c1,c2=float(sys.argv[1]),float(sys.argv[2])
s1,s2=json.loads(os.environ["SETTLE1"]),json.loads(os.environ["SETTLE2"])
u1=float(s1.get("wallet_unpaid_hmc") or 0)
u2=float(s2.get("wallet_unpaid_hmc") or 0)
dc=c2-c1
du=u2-u1
print(f"  coord delta={dc:.6f} HMC | unpaid delta={du:.6f} HMC")
if dc <= 0 and du <= 0:
    raise SystemExit("no growth in watch window — worker may be idle or signer mismatch")
PY
  ok "accrual growing"
fi

echo "[accrual-audit] PASS"
