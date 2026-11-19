#!/usr/bin/env bash
# Place one Bootstrap Hunt campaign (ASAN rail) onto the public pool.
#
#   bash place_bootstrap_hunt.sh jsmn [hunt_standard|hunt_lite] [shards]
#
# Env:
#   BOOTSTRAP_INSTALL  default /opt/hackme-bootstrap
#   BASE               default http://127.0.0.1:8080
#   COORD              default https://hackme.tech/pool/coordinator
#   STAMP              optional id stamp
set -euo pipefail
INSTALL="${BOOTSTRAP_INSTALL:-/opt/hackme-bootstrap}"
BASE="${BASE:-http://127.0.0.1:8080}"
COORD="${COORD:-https://hackme.tech/pool/coordinator}"
TARGET="${1:-jsmn}"
PKG="${2:-hunt_standard}"
SHARDS="${3:-32}"
STAMP="${STAMP:-$(date -u +%Y%m%dT%H%M%SZ)}"
CID="hunt-bootstrap-${TARGET}-${STAMP}"
TITLE="Bootstrap Hunt · ${TARGET} · ${PKG}"
LOG_DIR="${LOG_DIR:-$INSTALL/logs/bootstrap/hunt}"
mkdir -p "$LOG_DIR"

# Hunt escrow enforces HuntMinShards=8. Empty/0 → omit (package default: lite/standard/heavy).
if [[ -n "${SHARDS}" && "$SHARDS" =~ ^[0-9]+$ && "$SHARDS" -gt 0 && "$SHARDS" -lt 8 ]]; then
  echo "[bootstrap-hunt] clamp shards $SHARDS -> 8 (HuntMinShards)" >&2
  SHARDS=8
fi

ADMIN="$(grep -m1 '^HACKME_ADMIN_TOKEN=' "$INSTALL/.env" | cut -d= -f2- | tr -d '\r\n')"
[[ -n "$ADMIN" ]] || { echo "[bootstrap-hunt] missing HACKME_ADMIN_TOKEN" >&2; exit 2; }
# shellcheck source=load_coord_token.sh
source "$(dirname "$0")/load_coord_token.sh"
load_bootstrap_coord_token

# Fleet-safe defaults: prefer libfuzzer walls (AFL only when instrumented harness exists).
# Note: do not pre-set ENGINE_LEASE_SEC=0 — `${var:-90}` will not override an explicit 0.
HUNT_ENGINE="${HUNT_ENGINE:-}"
case "$PKG" in
  hunt_lite|lite)
    HUNT_ENGINE="${HUNT_ENGINE:-libfuzzer}"
    ENGINE_LEASE_SEC="${ENGINE_LEASE_SEC:-60}"
    ;;
  hunt_standard|standard|hunt_heavy|heavy)
    HUNT_ENGINE="${HUNT_ENGINE:-libfuzzer}"
    ENGINE_LEASE_SEC="${ENGINE_LEASE_SEC:-90}"
    ;;
  *)
    HUNT_ENGINE="${HUNT_ENGINE:-libfuzzer}"
    ENGINE_LEASE_SEC="${ENGINE_LEASE_SEC:-60}"
    ;;
esac
ENGINE_LEASE_SEC="${ENGINE_LEASE_SEC:-60}"

echo "[bootstrap-hunt $(date -u +%H:%M:%S)] PLACE $CID pkg=$PKG shards=${SHARDS:-pkg_default} engine=$HUNT_ENGINE lease=${ENGINE_LEASE_SEC}s"
payload="$(jq -nc \
  --arg id "$CID" \
  --arg title "$TITLE" \
  --arg pkg "$PKG" \
  --arg tid "$TARGET" \
  --arg eng "$HUNT_ENGINE" \
  --argjson lease "$ENGINE_LEASE_SEC" \
  --arg shards "${SHARDS:-}" \
  '{
    id: $id,
    package: $pkg,
    title: $title,
    pool_distributed: true,
    status: "running",
    catalog: true,
    target_id: $tid,
    hunt_engine: $eng,
    engine_lease_sec: $lease
  } + (if ($shards|tonumber? // 0) > 0 then {budget_shards: ($shards|tonumber)} else {} end)')"
resp="$(curl -sS --max-time 900 -X POST "$BASE/api/hunt/campaigns" \
  -H "Content-Type: application/json" \
  -H "X-Hackme-Admin-Token: $ADMIN" \
  -d "$payload")"
echo "$resp" | tee "$LOG_DIR/${CID}.json" >/dev/null
echo "$resp" | jq -c '{ok,id:(.campaign.id//.id),pool_sync,err:(.error//.code//.message)}' || echo "$resp" | head -c 800

cid_out="$(echo "$resp" | jq -r '.campaign.id // .id // empty')"
[[ -n "$cid_out" ]] || exit 1

# Best-effort resync + progress peek (non-fatal). Prefer repo script (has curl --max-time).
RESYNC="$(dirname "$0")/bootstrap_resync_pool.sh"
if [[ ! -x "$RESYNC" && -x "$INSTALL/scripts/bootstrap_customer/bootstrap_resync_pool.sh" ]]; then
  RESYNC="$INSTALL/scripts/bootstrap_customer/bootstrap_resync_pool.sh"
fi
POOL_SYNC="$(echo "$resp" | jq -r '.pool_sync // empty')"
if [[ "$POOL_SYNC" == "queued" || -z "$POOL_SYNC" ]]; then
  if [[ -x "$RESYNC" ]]; then
    CAMPAIGN_ID="$cid_out" RESYNC_CURL_MAX_TIME=45 bash "$RESYNC" \
      >>"$LOG_DIR/${CID}.resync.log" 2>&1 || true
  fi
fi
for i in 1 2 3; do
  prog="$(curl -fsS --max-time 15 -H "X-Hackme-Admin-Token: ${COORD_POLL_TOKEN}" "$COORD/api/fuzz/pool/campaigns/progress?id=${cid_out}" 2>/dev/null || echo '{}')"
  if echo "$prog" | jq -e '.ok==true' >/dev/null 2>&1; then
    echo "$prog" | jq -c '{ok,id,status,runs_done,budget_runs,title}'
    exit 0
  fi
  sleep "$i"
done
echo "[bootstrap-hunt] placed $cid_out (progress not yet visible)"
exit 0
