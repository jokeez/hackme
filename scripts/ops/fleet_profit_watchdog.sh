#!/usr/bin/env bash
# Keep ALL named fleet units online. When dig/hunt work appears, boost top workers'
# dig/hunt knobs (more shards / tighter gap) without stopping anyone else.
#
#   bash scripts/ops/fleet_profit_watchdog.sh
# State: logs/test-named-fleet/dig-boost.state  (idle|boost)
set -euo pipefail
export PATH=/usr/bin:/bin:/usr/local/bin:${PATH:-}
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

LOG_DIR="${LOG_DIR:-$ROOT/logs/test-named-fleet}"
SEED_DIR="${SEED_DIR:-$LOG_DIR/seeds}"
COORD_URL="${COORD_URL:-http://132.243.112.100:18083}"
UNIT_PREFIX="${UNIT_PREFIX:-hackme-test-poh}"
STATE_FILE="${STATE_FILE:-$LOG_DIR/dig-boost.state}"
BOOST_LIST="${BOOST_LIST:-$LOG_DIR/dig-boost.workers}"
TOP_N="${TOP_N:-6}"
TOKEN="${POOL_TOKEN:-}"
MINERSIGN_BIN="${MINERSIGN_BIN:-$ROOT/bin/minersign}"
WATCH_LOG="${WATCH_LOG:-$LOG_DIR/profit-watchdog.jsonl}"

NAMES=(
  desktop-a4m2rx desktop-k7v1pd desktop-q9n4ls desktop-t2c8we desktop-z5h6mf
  shannon turing hopper knuth mccarthy
  lovelace tesla euclid noether faraday
  ui-west digga fuzzlane rack-neon byte-hop
  desktop-p3r9kx desktop-m2w8qn desktop-h7c4vj
  desktop-y1d8qt desktop-q8n2kp desktop-x4m7vt
  anechka FroZenn mira-lynx
)

mkdir -p "$LOG_DIR"
[[ -f "$STATE_FILE" ]] || echo idle >"$STATE_FILE"
[[ -f "$BOOST_LIST" ]] || : >"$BOOST_LIST"
if [[ -z "$TOKEN" && -f "$ROOT/.secrets/hackme_coordinator_worker_token" ]]; then
  TOKEN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_coordinator_worker_token")"
fi
[[ -n "$TOKEN" ]] || { echo "[watchdog] missing pool token" >&2; exit 1; }
[[ -x "$MINERSIGN_BIN" ]] || MINERSIGN_BIN="$ROOT/minersign"

# Pick probe worker = highest GH online from hub stats
curl -fsS --max-time 12 -A 'Mozilla/5.0' \
  'https://hackme.tech/pool/coordinator/api/work/stats' -o /tmp/wd-stats.json

# Public /api/work/stats often keys workers by pubkey hash (no worker-* names).
# Prefer matching named ids when present; else fall back to locally active units.
mapfile -t TOP < <(python3 - "$TOP_N" "$UNIT_PREFIX" "${NAMES[@]}" <<'PY'
import json, os, subprocess, sys
top_n=int(sys.argv[1]); prefix=sys.argv[2]; names=sys.argv[3:]
w=json.load(open("/tmp/wd-stats.json")).get("workers") or {}
rows=[]
for n in names:
  wid="worker-"+n
  x=w.get(wid) or {}
  if not x.get("online"):
    continue
  rows.append((float(x.get("hashrate_gh_s") or 0), n))
rows.sort(reverse=True)
if rows:
  for _,n in rows[:top_n]:
    print(n)
  raise SystemExit(0)
# Local fallback: systemd user units for this fleet are the source of truth.
active=set()
try:
  out=subprocess.check_output(
    ["systemctl","--user","list-units",f"{prefix}-*","--state=running","--no-legend","--plain"],
    text=True, stderr=subprocess.DEVNULL)
except Exception:
  out=""
for line in out.splitlines():
  unit=(line.split() or [""])[0]
  if unit.startswith(prefix+"-") and unit.endswith(".service"):
    active.add(unit[len(prefix)+1:-len(".service")])
local=[n for n in names if n in active]
for n in local[:top_n]:
  print(n)
PY
)

if (( ${#TOP[@]} == 0 )); then
  echo "[watchdog] no online workers (hub map empty + no local ${UNIT_PREFIX}-* running)" >&2
  exit 1
fi

probe_name="${TOP[0]}"
probe_wid="worker-${probe_name}"
seed="$(tr -d '\r\n' <"$SEED_DIR/${probe_wid}.seed")"
pub="$(
  jq -nc '{worker_id:"pubkey-probe",base_nonce:0,batch_size:1,work_id:"pubkey-probe",attempts:1,found:false,found_nonce:0,result_hash:"",proof_hash:""}' \
    | env HACKME_MINER_ED25519_SEED_HEX="$seed" "$MINERSIGN_BIN" --nonce-file "/tmp/wd-pub-$$" \
    | jq -r '.miner_pubkey_ed25519 // empty'
)"
[[ ${#pub} -eq 64 ]] || { echo "[watchdog] bad pubkey" >&2; exit 1; }

claim_json="$(curl -sS --max-time 15 \
  -H "X-Hackme-Admin-Token: $TOKEN" -H 'Content-Type: application/json' \
  -d "$(jq -nc --arg wid "$probe_wid" --arg pub "$pub" '{worker_id:$wid,miner_pubkey:$pub,miner_pubkey_ed25519:$pub}')" \
  "${COORD_URL}/api/fuzz/work/claim" || true)"
reason="$(printf '%s' "$claim_json" | jq -r '.reason // empty' 2>/dev/null || true)"
ok="$(printf '%s' "$claim_json" | jq -r '.ok // false' 2>/dev/null || echo false)"

dig_live=0
if [[ "$ok" == "true" ]]; then
  dig_live=1
elif [[ -n "$reason" && "$reason" != "no_fuzz_work" && "$reason" != "ghs_priority_defer" && "$reason" != "claim_rate_limited" ]]; then
  # unexpected — treat as potential work path still contested
  dig_live=0
fi
# ghs_priority_defer means queue may exist but we're deferred — still "work present"
if [[ "$reason" == "ghs_priority_defer" ]]; then
  dig_live=1
fi

prev=idle
if [[ -f "$STATE_FILE" ]]; then
  prev="$(tr -d '\r\n' <"$STATE_FILE" || true)"
fi
prev="${prev:-idle}"
want="idle"
[[ "$dig_live" == "1" ]] && want="boost"

printf '%s\n' "${TOP[@]}" >"$BOOST_LIST" || true
printf '%s\n' "$want" >"$STATE_FILE"
python3 -c "import json,time; print(json.dumps({'ts':time.time(),'want':'$want','prev':'$prev','reason':'''$reason''','ok':'$ok','tops':'${TOP[*]}'}))" >>"$WATCH_LOG"

echo "[watchdog] dig_live=$dig_live reason=${reason:-none} want=$want tops=${TOP[*]}"

lookup_gh_batch() {
  # Prefer pinned files from start script, then live unit env, then sane mid-board default.
  local name="$1" unit="$2" gh batch
  gh=""; batch=""
  [[ -f "$LOG_DIR/worker-${name}.gh" ]] && gh="$(tr -d '\r\n' <"$LOG_DIR/worker-${name}.gh")"
  [[ -f "$LOG_DIR/worker-${name}.batch" ]] && batch="$(tr -d '\r\n' <"$LOG_DIR/worker-${name}.batch")"
  if [[ -z "$gh" ]]; then
    gh="$(systemctl --user show "$unit.service" -p Environment --value 2>/dev/null | tr ' ' '\n' | sed -n 's/^FORCE_HASHRATE_GHS=//p' | head -1 || true)"
  fi
  if [[ -z "$batch" ]]; then
    batch="$(systemctl --user show "$unit.service" -p Environment --value 2>/dev/null | tr ' ' '\n' | sed -n 's/^BATCH_SIZE=//p' | head -1 || true)"
  fi
  [[ -n "$gh" ]] || gh=50
  [[ -n "$batch" ]] || batch=8388608
  printf '%s %s\n' "$gh" "$batch"
}

restart_one() {
  local mode="$1" name="$2" unit seed_file seed gh batch hunt gap
  unit="${UNIT_PREFIX}-${name}"
  seed_file="$SEED_DIR/worker-${name}.seed"
  [[ -f "$seed_file" ]] || return 0
  seed="$(tr -d '\r\n' <"$seed_file")"
  read -r gh batch <<<"$(lookup_gh_batch "$name" "$unit")"
  systemctl --user stop "$unit.service" 2>/dev/null || true
  systemctl --user reset-failed "$unit.service" 2>/dev/null || true
  hunt=1; gap=7000
  if [[ "$mode" == "boost" ]]; then
    hunt=2
    gap=2500
  fi
  systemd-run --user \
    --unit="$unit" \
    --property=Restart=on-failure \
    --property=RestartSec=5 \
    --working-directory="$ROOT" \
    --setenv=COORD_URL="$COORD_URL" \
    --setenv=COORD_ADMIN_TOKEN="$TOKEN" \
    --setenv=COORD_TOKEN="$TOKEN" \
    --setenv=WORKER_ID="worker-${name}" \
    --setenv=WORKER_NAME="$name" \
    --setenv=BATCH_SIZE="$batch" \
    --setenv=FORCE_HASHRATE_GHS="$gh" \
    --setenv=HASHRATE_GHS="$gh" \
    --setenv=HACKME_MINER_ED25519_SEED_HEX="$seed" \
    --setenv=MINERSIGN_BIN="$MINERSIGN_BIN" \
    --setenv=LOG_DIR="$LOG_DIR" \
    --setenv=HACKME_NAMED_HYBRID_FUZZ=1 \
    --setenv=HACKME_REPO_ROOT="$ROOT" \
    --setenv=HACKME_COORDINATOR_URL="$COORD_URL" \
    --setenv=HACKME_POOL_COORDINATOR_URL="$COORD_URL" \
    --setenv=HACKME_WORKER_HUNT_SHARDS="$hunt" \
    --setenv=HACKME_WORKER_HUNT_TIMEOUT_MS=180000 \
    --setenv=WORKERFUZZ_HTTP_TIMEOUT_SEC=120 \
    --setenv=WORKERFUZZ_TIMEOUT_MS=2500 \
    --setenv=HACKME_WORKER_HYBRID_FUZZ_CLAIM_GAP_MS="$gap" \
    --setenv=HACKME_WORKER_HYBRID_FUZZ_CONCURRENCY=1 \
    --setenv=HACKME_WORKER_CLAIM_COOLDOWN_MS=4500 \
    --setenv=HACKME_WORKER_CLAIM_COOLDOWN_JITTER_MS=1500 \
    --setenv=HACKME_WORKER_BATCH_JITTER_PCT=18 \
    --setenv=HACKME_WORKER_GH_JITTER_PCT=6 \
    "$ROOT/scripts/ops/named_hybrid_unit.sh"
  echo "[watchdog] restarted worker-${name} mode=$mode hunt=$hunt gap=$gap gh=$gh"
}

restart_boosted() {
  local mode="$1" name
  for name in "${TOP[@]}"; do
    restart_one "$mode" "$name"
  done
}

if [[ "$want" != "$prev" ]]; then
  echo "[watchdog] transition $prev -> $want"
  restart_boosted "$want"
else
  echo "[watchdog] no transition (stay $want); all other workers untouched"
fi

# Revive only inactive tops (one-by-one) — never stop the rest of the fleet
for name in "${TOP[@]}"; do
  if ! systemctl --user is-active --quiet "${UNIT_PREFIX}-${name}.service" 2>/dev/null; then
    echo "[watchdog] WARN ${name} inactive — revive only this unit (mode=$want)"
    restart_one "$want" "$name"
  fi
done
