#!/usr/bin/env bash
# Live verify: pool stats TH/s, seal miners, dynamic seal_target_mod_m (coordinator must be new build).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COORD="${HACKME_HMS_COORDINATOR_URL:-http://127.0.0.1:18082}"
NODE="${HACKME_NODE_URL:-http://127.0.0.1:8080}"
POLL_SEC="${POLL_SEC:-45}"
INTERVAL="${INTERVAL:-5}"

fail() { echo "[hms-live] FAIL: $*" >&2; exit 1; }
ok() { echo "[hms-live] OK: $*"; }

need_field() {
  local j="$1" field="$2"
  if ! echo "$j" | jq -e ".$field != null" >/dev/null 2>&1; then
    fail "missing field .$field in pool stats (rebuild/restart hmscoordinator?)"
  fi
}

stats() { curl -fsS --max-time 5 "$COORD/api/pool/stats"; }
node_stats() { curl -fsS --max-time 5 "$NODE/api/hms/pool/stats"; }

echo "[hms-live] coordinator $COORD"
J="$(stats)"
echo "$J" | jq '{status, seal_miners_connected, seal_hashrate_th, seal_target_mod_m, seal_difficulty_dynamic, storage_workers_online}'
need_field "$J" seal_miners_connected
need_field "$J" seal_hashrate_th
need_field "$J" seal_target_mod_m
need_field "$J" seal_difficulty_dynamic

NJ="$(node_stats)"
need_field "$NJ" seal_hashrate_th
ok "node proxy /api/hms/pool/stats"

MOD0="$(echo "$J" | jq -r '.seal_target_mod_m')"
echo "[hms-live] poll TH/s for ${POLL_SEC}s (ASIC + optional sim)"
end=$((SECONDS + POLL_SEC))
max_th=0
last_miners=0
while ((SECONDS < end)); do
  S="$(stats)"
  th="$(echo "$S" | jq -r '.seal_hashrate_th // 0')"
  miners="$(echo "$S" | jq -r '.seal_miners_connected // 0')"
  mod="$(echo "$S" | jq -r '.seal_target_mod_m // 0')"
  echo "[hms-live] t=$((SECONDS)) miners=$miners th=$th mod_m=$mod"
  last_miners=$miners
  awk -v a="$th" -v b="$max_th" 'BEGIN{exit !(a>b)}' && max_th="$th" || true
  sleep "$INTERVAL"
done

if [[ "$last_miners" -lt 1 ]]; then
  echo "[hms-live] WARN: no seal_miners_connected — is ASIC on 192.168.0.110:3334?"
fi

if go build -trimpath -o "$ROOT/bin/hms_stratum_asic_sim" ./tools/hms_stratum_asic_sim 2>/dev/null; then
  echo "[hms-live] ASIC simulator 25s (submits → measured TH)"
  timeout 25 "$ROOT/bin/hms_stratum_asic_sim" -addr 127.0.0.1:3334 -worker live-verify-sim -max-sec 22 || true
  S2="$(stats)"
  th2="$(echo "$S2" | jq -r '.seal_hashrate_th // 0')"
  echo "[hms-live] after sim: miners=$(echo "$S2" | jq -r '.seal_miners_connected') th=$th2"
  awk -v t="$th2" 'BEGIN{exit !(t>0)}' && ok "measured seal_hashrate_th > 0 after sim" || echo "[hms-live] WARN: sim did not raise measured TH (need ≥3 submits / 60s window)"
fi

echo "[hms-live] dynamic difficulty fields"
echo "$J" | jq '{seal_target_mod_m, seal_target_leading_zero_bits, desired_seal_sec, seal_retarget_clamp}'
if [[ "$(echo "$J" | jq -r '.seal_difficulty_dynamic')" != "true" ]]; then
  fail "seal_difficulty_dynamic not true"
fi
ok "seal_target_mod_m=$MOD0 (retarget applies after successful epoch seal via RetargetSeal)"

echo "[hms-live] done"
