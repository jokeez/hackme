#!/usr/bin/env bash
# Snapshot pool + Day14 + jsmn until local 08:00 (TZ=Asia/Almaty or +05).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
LOG="$ROOT/logs/overnight-pool-watch-$(date +%Y%m%d).log"
END_LOCAL_HOUR=8
mkdir -p "$ROOT/logs"
ts() { date -Is; }
log() { echo "[$(ts)] $*" | tee -a "$LOG"; }

# end at next 08:00 local
python3 - <<'PY' > /tmp/overnight_end_epoch
from datetime import datetime, timedelta
now=datetime.now().astimezone()
end=now.replace(hour=8, minute=0, second=0, microsecond=0)
if now >= end:
  end += timedelta(days=1)
print(int(end.timestamp()))
print(end.isoformat())
PY
END_EPOCH=$(head -1 /tmp/overnight_end_epoch)
END_ISO=$(tail -1 /tmp/overnight_end_epoch)
log "overnight watch start → $END_ISO"

snap() {
  {
    echo "===== SNAP $(ts) ====="
    systemctl --user is-active hackme-cve-evening.service 2>/dev/null || true
    pgrep -c -f 'nghttp2-asan-fuzzer' || true
    curl -fsS --max-time 10 https://hackme.tech/api/status | python3 -c "import json,sys;d=json.load(sys.stdin);print('public',d.get('commit'),d.get('version'))" 2>/dev/null || echo public_fail
    ssh -o ConnectTimeout=12 -o BatchMode=yes hackme-vps 'python3 - <<"PY"
import json,urllib.request,subprocess
def get(u):
  with urllib.request.urlopen(u,timeout=10) as r: return json.load(r)
st=get("http://127.0.0.1:18080/api/status")
stats=get("http://127.0.0.1:18081/api/work/stats")
print("hub", st.get("commit"), "online", stats.get("workers_online"), "gh", round(float(stats.get("pool_hashrate_gh_s") or 0),1), "mode", stats.get("scheduler_mode"), "M", stats.get("target_mod"), "signed", stats.get("signed_submits_accepted"), "rej", stats.get("signed_submits_rejected"))
cid="campaign-bootstrap-jsmn-20260719t195248z"
try:
  f=get("http://127.0.0.1:18081/api/fuzz/pool/campaigns/progress?id="+cid)
  print("jsmn", f.get("runs_done"), "/", f.get("budget_runs"), f.get("status"))
except Exception as e:
  print("jsmn_err", e)
# load
u=subprocess.check_output(["uptime"]).decode().strip()
print("uptime", u)
print("workerfuzz", subprocess.check_output(["systemctl","is-active","hackme-workerfuzz"], text=True).strip())
opens=sum(1 for t in get("http://127.0.0.1:18080/api/tasks").get("tasks") or [] if t.get("status")=="open")
print("open_orders", opens)
PY' 2>&1 || echo hub_ssh_fail
  } | tee -a "$LOG"
}

snap
while [[ $(date +%s) -lt $END_EPOCH ]]; do
  sleep 1800  # every 30 min
  snap || log "snap failed"
done
log "overnight watch complete"
# final rich snap
snap
