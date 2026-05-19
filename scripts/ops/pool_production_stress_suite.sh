#!/usr/bin/env bash
# Production pool stress suite: geo latency, coordinator load, reconnect churn, coordinator failover.
#
#   bash scripts/ops/pool_production_stress_suite.sh
#
# Optional:
#   SKIP_FAILOVER=1     — skip coordinator restart on hackme-vps
#   STRESS_WORKERS=12   — synthetic stress worker count
#   STRESS_SEC=75       — load duration per phase

set -euo pipefail
OPS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$OPS_DIR/../.." && pwd)"
cd "$ROOT"
RUN_ID="${RUN_ID:-pool_stress_$(date -u +%Y%m%dT%H%M%SZ)}"
OUT="${OUT_DIR:-$ROOT/reports/$RUN_ID}"
mkdir -p "$OUT"

COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
BASE="${BASE:-https://hackme.tech}"
NODE_SSH="${NODE_SSH:-hackme-vps}"
MSK_SSH="${MSK_SSH:-root@82.146.53.7}"
STRESS_WORKERS="${STRESS_WORKERS:-10}"
STRESS_SEC="${STRESS_SEC:-75}"
SKIP_FAILOVER="${SKIP_FAILOVER:-0}"

TOKEN_FILE="${TOKEN_FILE:-$ROOT/.secrets/hackme_coordinator_admin_token}"
TOKEN="$(tr -d '\r\n' <"$TOKEN_FILE")"

log() { echo "[pool-stress] $*" | tee -a "$OUT/suite.log"; }
fail() { log "FAIL: $*"; exit 1; }

probe_latency() {
  local name="$1" url="$2" via="${3:-}"
  local meta code lat_ms
  if [[ -n "$via" ]]; then
    meta="$(ssh -o BatchMode=yes -o ConnectTimeout=12 "$via" \
      "curl -sS -o /dev/null -w '%{http_code} %{time_total}' --max-time 20 '$url' 2>/dev/null" || echo "000 9.9")"
  else
    meta="$(curl -sS -o /dev/null -w '%{http_code} %{time_total}' --max-time 20 "$url" 2>/dev/null || echo "000 9.9")"
  fi
  code="$(echo "$meta" | awk '{print $1}')"
  lat_ms="$(awk -v t="$(echo "$meta" | awk '{print $2}')" 'BEGIN{printf "%.0f", t*1000}')"
  log "  geo $name: HTTP $code ${lat_ms}ms $url"
  echo "$name,$code,$lat_ms" >>"$OUT/geo_latency.csv"
}

log "=== 1/5 Geo / ping (coordinator + public) ==="
: >"$OUT/geo_latency.csv"
echo "host,code,latency_ms" >>"$OUT/geo_latency.csv"
probe_latency "local_desktop" "$COORD_URL/api/pool/stats"
probe_latency "vps_hub" "$COORD_URL/api/pool/stats" "$NODE_SSH"
probe_latency "msk_remote" "$COORD_URL/api/pool/stats" "$MSK_SSH"
probe_latency "public_status" "$BASE/api/status"

log "=== 2/5 API stability soak (90s) ==="
BASE="$BASE" COORD_URL="$COORD_URL" DURATION_SEC=90 INTERVAL_SEC=10 \
  OUT_DIR="$OUT/soak" RUN_ID="${RUN_ID}_soak" \
  bash "$OPS_DIR/network_stability_soak.sh" 2>&1 | tee -a "$OUT/suite.log" || log "WARN: soak had issues"

log "=== 3/5 Coordinator load (loopback on $NODE_SSH, bypass nginx) ==="
rsync -az "$OPS_DIR/tools/pool_stress_runner.py" "$NODE_SSH:/tmp/pool_stress_runner.py"
ssh -o BatchMode=yes "$NODE_SSH" bash -s <<REMOTE | tee -a "$OUT/suite.log"
set -euo pipefail
COORD=\$(grep -m1 HACKME_COORDINATOR_ADMIN_TOKEN /opt/hackme/.env.coord | cut -d= -f2)
python3 /tmp/pool_stress_runner.py \
  --coord http://127.0.0.1:18081 --token "\$COORD" \
  --duration-sec ${STRESS_SEC} --workers ${STRESS_WORKERS} \
  --batch-size 262144 --claim-only --output /tmp/stress_load.json
cat /tmp/stress_load.json
REMOTE
scp -q "$NODE_SSH:/tmp/stress_load.json" "$OUT/stress_load.json" 2>/dev/null || true

log "=== 4/5 Reconnect churn (loopback, claim-only + gaps) ==="
ssh -o BatchMode=yes "$NODE_SSH" bash -s <<REMOTE | tee -a "$OUT/suite.log"
set -euo pipefail
COORD=\$(grep -m1 HACKME_COORDINATOR_ADMIN_TOKEN /opt/hackme/.env.coord | cut -d= -f2)
python3 /tmp/pool_stress_runner.py \
  --coord http://127.0.0.1:18081 --token "\$COORD" \
  --duration-sec 40 --workers $((STRESS_WORKERS / 2 + 1)) \
  --batch-size 131072 --churn --claim-only --output /tmp/stress_churn.json
cat /tmp/stress_churn.json
REMOTE
scp -q "$NODE_SSH:/tmp/stress_churn.json" "$OUT/stress_churn.json" 2>/dev/null || true

curl -fsS -H "X-Hackme-Admin-Token: $TOKEN" \
  "$COORD_URL/api/work/admin/clear-abuse" \
  -H "Content-Type: application/json" -d '{"all":true}' >/dev/null 2>&1 || true

log "=== Production workers snapshot (before failover) ==="
curl -fsS -H "X-Hackme-Admin-Token: $TOKEN" \
  "$COORD_URL/api/work/stats?details=1" >"$OUT/stats_before.json"
jq -r '.workers | to_entries[] | "\(.key) att=\(.value.accepted_attempts // 0) payout=\(.value.payout_hmc // 0)"' \
  "$OUT/stats_before.json" | tee -a "$OUT/suite.log"

if [[ "$SKIP_FAILOVER" != "1" ]]; then
  log "=== 5/5 Failover: restart coordinator on $NODE_SSH ==="
  t0="$(date +%s)"
  ssh -o BatchMode=yes "$NODE_SSH" 'sudo systemctl restart hackme-coordinator' 2>&1 | tee -a "$OUT/suite.log"
  sleep 2
  ok=0
  for i in $(seq 1 30); do
    if ssh -o BatchMode=yes "$NODE_SSH" \
      "COORD=\$(grep -m1 HACKME_COORDINATOR_ADMIN_TOKEN /opt/hackme/.env.coord|cut -d= -f2); \
       curl -fsS -H \"X-Hackme-Admin-Token: \$COORD\" http://127.0.0.1:18081/api/work/stats >/dev/null 2>&1"; then
      ok=1
      break
    fi
    sleep 1
  done
  t1="$(date +%s)"
  log "  coordinator back in $((t1 - t0))s ok=$ok"
  [[ "$ok" == "1" ]] || fail "coordinator did not recover"
  sleep 8
  curl -fsS -H "X-Hackme-Admin-Token: $TOKEN" \
    "$COORD_URL/api/work/stats?details=1" >"$OUT/stats_after.json"
  log "  workers after failover:"
  jq -r '.workers | to_entries[] | "\(.key) ghs=\(.value.hashrate_gh_s // 0)"' \
    "$OUT/stats_after.json" | tee -a "$OUT/suite.log"
else
  log "=== 5/5 Failover SKIPPED ==="
fi

log "=== Verdict ==="
python3 - "$OUT" <<'PY'
import json, sys
from pathlib import Path
out = Path(sys.argv[1])
load = json.loads((out / "stress_load.json").read_text()) if (out / "stress_load.json").exists() else {}
churn = json.loads((out / "stress_churn.json").read_text()) if (out / "stress_churn.json").exists() else {}
lines = []
def summarize(name, d):
    st = d.get("stats") or {}
    acc = d.get("accepted", 0)
    c200 = st.get("claim_200", 0)
    s200 = st.get("submit_200", 0)
    net = st.get("net_err", 0)
    reasons = {k: v for k, v in st.items() if "reason:" in k}
    lines.append(f"{name}: accepted={acc} claim_200={c200} submit_200={s200} net_err={net}")
    for k, v in sorted(reasons.items(), key=lambda x: -x[1])[:6]:
        lines.append(f"  {k}={v}")
print("\n".join(lines))
# stale/reject proxy: submit not accepted
rej = sum(v for k, v in (load.get("stats") or {}).items() if "submit_reason:" in k)
rej += sum(v for k, v in (churn.get("stats") or {}).items() if "submit_reason:" in k)
acc = load.get("accepted", 0) + churn.get("accepted", 0)
c_ok = int((load.get("stats") or {}).get("claim_ok", 0)) + int((churn.get("stats") or {}).get("claim_ok", 0))
c200 = int(load.get("claim_200", 0)) + int(churn.get("claim_200", 0))
lines.append(f"reject_reason_total={rej} accepted_total={acc} claim_ok={c_ok} claim_200={c200}")
if c_ok + c200 > 50:
    lines.append("PASS: coordinator handled burst claims (rate-limit/ban expected at extreme load)")
elif acc > 0:
    lines.append("PASS: submits accepted under load")
else:
    lines.append("WARN: check rate limits / nginx 403 on public URL stress")
print("\n".join(lines))
PY

log "Reports: $OUT"
log "DONE"
