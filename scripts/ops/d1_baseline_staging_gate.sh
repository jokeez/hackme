#!/usr/bin/env bash
# RFC D1 readiness / soak observer (econ-v2).
#
# Does NOT flip live local PoH off. Safe default: report-only.
#
#   COORD_URL=https://hackme.tech/pool/coordinator bash scripts/ops/d1_baseline_staging_gate.sh
#   ENABLE_DRY=1 NODE_SSH=hackme-vps bash scripts/ops/d1_baseline_staging_gate.sh
#     → sets HACKME_POOL_BASELINE_RELAY=dry on remote .env.coord and restarts coordinator
#   SOAK_SEC=300 …  → poll baseline_relay_* for SOAK_SEC seconds after dry enable
#
# Live mint (RELAY=on + LOCAL_POH=0) is intentionally NOT automated here.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
COORD_URL="${COORD_URL%/}"
NODE_SSH="${NODE_SSH:-}"
ENABLE_DRY="${ENABLE_DRY:-0}"
SOAK_SEC="${SOAK_SEC:-0}"
ENV_COORD_REMOTE="${ENV_COORD_REMOTE:-/opt/hackme/.env.coord}"
ENV_VPS_REMOTE="${ENV_VPS_REMOTE:-/opt/hackme/.env.vps}"

_ssh() {
  ssh -o BatchMode=yes -o ConnectTimeout=12 "$@"
}

echo "[d1-gate] coord=$COORD_URL enable_dry=$ENABLE_DRY soak_sec=$SOAK_SEC"

stats="$(curl -fsS --max-time 20 "${COORD_URL}/api/work/stats?details=0" || true)"
if [[ -z "$stats" ]]; then
  echo "[d1-gate] FAIL: cannot fetch work/stats" >&2
  exit 1
fi

python3 - <<'PY' "$stats"
import json,sys
d=json.loads(sys.argv[1])
keys=("baseline_relay_enabled","baseline_relay_mode","baseline_relay_ok","baseline_relay_reject","baseline_relay_would",
      "share_strict","share_accepts","found_hits","tip_height","pool_hashrate_gh_s")
print("[d1-gate] live stats:")
for k in keys:
    if k in d:
        print(f"  {k}={d[k]}")
mode=str(d.get("baseline_relay_mode") or ("on" if d.get("baseline_relay_enabled") else "off"))
print(f"[d1-gate] mode={mode}")
if d.get("share_strict") is not True and str(d.get("share_strict")).lower() not in ("1","true"):
    print("[d1-gate] WARN: SHARE_STRICT not true — D10 expected live before emission pool")
PY

if [[ -n "$NODE_SSH" ]]; then
  echo "[d1-gate] remote env probe ($NODE_SSH)"
  _ssh "$NODE_SSH" "grep -E '^HACKME_POOL_BASELINE_RELAY=|^HACKME_CHAIN_LEADER_LOCAL_POH=' '$ENV_COORD_REMOTE' '$ENV_VPS_REMOTE' 2>/dev/null || true"
fi

if [[ "$ENABLE_DRY" == "1" ]]; then
  if [[ -z "$NODE_SSH" ]]; then
    echo "[d1-gate] FAIL: ENABLE_DRY=1 requires NODE_SSH" >&2
    exit 1
  fi
  echo "[d1-gate] enabling DRY relay on $NODE_SSH ($ENV_COORD_REMOTE)"
  _ssh "$NODE_SSH" "bash -s" <<EOF
set -euo pipefail
f='$ENV_COORD_REMOTE'
if grep -q '^HACKME_POOL_BASELINE_RELAY=' "\$f" 2>/dev/null; then
  sed -i 's/^HACKME_POOL_BASELINE_RELAY=.*/HACKME_POOL_BASELINE_RELAY=dry/' "\$f"
else
  printf '\\nHACKME_POOL_BASELINE_RELAY=dry\\n' >>"\$f"
fi
grep '^HACKME_POOL_BASELINE_RELAY=' "\$f"
systemctl restart hackme-coordinator
sleep 2
systemctl is-active hackme-coordinator
EOF
  echo "[d1-gate] dry enabled; LOCAL_POH left unchanged (safe observe)"
fi

if [[ "$SOAK_SEC" -gt 0 ]]; then
  echo "[d1-gate] soaking ${SOAK_SEC}s…"
  end=$((SECONDS + SOAK_SEC))
  while (( SECONDS < end )); do
    s="$(curl -fsS --max-time 15 "${COORD_URL}/api/work/stats?details=0" || echo '{}')"
    python3 - <<'PY' "$s"
import json,sys
d=json.loads(sys.argv[1] or '{}')
print("[d1-soak]", {k:d.get(k) for k in (
  "baseline_relay_mode","baseline_relay_would","baseline_relay_ok","baseline_relay_reject","found_hits")})
PY
    sleep 30
  done
fi

echo "[d1-gate] next (manual, not automated):"
echo "  1) Confirm baseline_relay_would grows under dry"
echo "  2) Staging: HACKME_CHAIN_LEADER_LOCAL_POH=0 + HACKME_POOL_BASELINE_RELAY=1"
echo "  3) Watch tip growth + baseline_relay_ok vs reject + settlement float"
echo "[d1-gate] OK"
