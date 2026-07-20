#!/usr/bin/env bash
# One-shot pool + libheif snapshot for away monitoring (systemd timer on hub).
set -uo pipefail
ROOT="${HACKME_ROOT:-/opt/hackme}"
LOG="$ROOT/logs/pool-away-watch.log"
mkdir -p "$ROOT/logs"

ts() { date -u +%Y-%m-%dT%H:%M:%SZ; }

{
  set +e
  echo "===== $(ts) ====="
  systemctl is-active hackme-node hackme-coordinator hackme-workerfuzz hackme-libheif-24h 2>/dev/null
  curl -fsS --max-time 12 http://127.0.0.1:18081/api/work/stats 2>/dev/null | python3 -c "
import json,sys
d=json.load(sys.stdin)
ws=d.get('workers',{})
gh=sum(w.get('hashrate_gh_s',0) for w in ws.values())
on=sum(1 for w in ws.values() if w.get('online'))
print(f'pool workers={len(ws)} online={on} gh={gh:.1f} mode={d.get(\"scheduler_mode\")} leases={d.get(\"active_leases_count\")}')
" 2>/dev/null || echo "pool_stats_fail"
  pgrep -af 'file_fuzzer.*libheif' | head -1 || echo "libheif_fuzzer=down"
  [[ -f "$ROOT/reports/oss-cve-watch-libheif/cadence.json" ]] && cat "$ROOT/reports/oss-cve-watch-libheif/cadence.json"
} >>"$LOG" 2>&1

# keep log bounded
if [[ -f "$LOG" ]] && [[ $(wc -l <"$LOG") -gt 5000 ]]; then
  tail -3000 "$LOG" >"${LOG}.tmp" && mv "${LOG}.tmp" "$LOG"
fi
