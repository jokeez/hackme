#!/usr/bin/env bash
# Full pool health: unit tests, coordinator API, worker processes, difficulty samples.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
COORD_TOKEN="${HACKME_POOL_COORDINATOR_TOKEN:-$(tr -d '\r\n' <"$ROOT/.secrets/hackme_coordinator_admin_token" 2>/dev/null || true)}"
COORD="${COORD:-https://hackme.tech/pool/coordinator}"
REPORT="${REPORT:-$ROOT/reports/pool-health-$(date -u +%Y%m%dT%H%M%SZ)}"
mkdir -p "$REPORT"
fail=0

log() { echo "$*" | tee -a "$REPORT/summary.log"; }
ok() { log "OK  $*"; }
bad() { log "FAIL $*"; fail=$((fail + 1)); }
wrn() { log "WARN $*"; }

log "=== pool health $(date -u +%Y-%m-%dT%H:%M:%SZ) ==="

log "--- go test (coordinator + gpupoh + gputune) ---"
if go test ./cmd/coordinator/... ./internal/gpupoh/... ./internal/gputune/... -count=1 -short >>"$REPORT/go-test.log" 2>&1; then
  ok "go test coordinator/gpupoh/gputune"
else
  bad "go test failed (see $REPORT/go-test.log)"
fi

log "--- coordinator API ---"
if curl -fsS --max-time 15 -H "X-Hackme-Admin-Token: $COORD_TOKEN" \
  "$COORD/api/work/stats?details=1" >"$REPORT/pool.json"; then
  ok "coordinator reachable"
  python3 - "$REPORT/pool.json" <<'PY' | tee -a "$REPORT/summary.log"
import json, sys, time
d = json.load(open(sys.argv[1]))
n = time.time()
print("  target_mod", d.get("target_mod"), "hint", d.get("target_mod_load_hint"))
print("  pool_gh_s", round(d.get("pool_hashrate_gh_s", 0) or 0, 4))
for k, v in sorted((d.get("workers") or {}).items()):
    age = int(n - (v.get("last_seen_unix") or n))
    print(f"  {k}: gh={v.get('hashrate_gh_s', 0):.4f} age={age}s")
PY
else
  bad "coordinator unreachable"
fi

log "--- local processes ---"
if pgrep -f 'workerpoh.*worker-kapa-pc' >/dev/null; then
  ok "worker-kapa-pc process: $(pgrep -af 'workerpoh.*worker-kapa-pc' | head -1)"
else
  wrn "worker-kapa-pc not running"
fi
if pgrep -f hackme-node-desktop >/dev/null; then
  ok "hackme-node-desktop running"
else
  wrn "local node not running"
fi

log "--- difficulty samples (3x25s) ---"
: >"$REPORT/difficulty.tsv"
echo -e "sample\ttarget_mod\tpool_gh" >>"$REPORT/difficulty.tsv"
for i in 1 2 3; do
  if curl -fsS --max-time 12 -H "X-Hackme-Admin-Token: $COORD_TOKEN" \
    "$COORD/api/work/stats" 2>/dev/null | python3 -c "import sys,json;d=json.load(sys.stdin);print(d.get('target_mod',0), round(d.get('pool_hashrate_gh_s',0),4))" \
    >>"$REPORT/difficulty.tsv" 2>/dev/null; then
    echo -e "\t$i" >>"$REPORT/difficulty.tsv"
  fi
  [[ "$i" -lt 3 ]] && sleep 25
done
ok "samples in $REPORT/difficulty.tsv"

log "--- done fail=$fail ---"
exit "$fail"
