#!/usr/bin/env bash
# Cancel internal pool fuzz gate/probe campaigns on local node and coordinator.
#
# Usage:
#   DRY_RUN=1 bash scripts/ops/cancel_pool_fuzz_gates.sh
#   DRY_RUN=0 bash scripts/ops/cancel_pool_fuzz_gates.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BASE="${BASE:-http://127.0.0.1:8080}"
COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
DRY_RUN="${DRY_RUN:-1}"
ADMIN_TOKEN="${ADMIN_TOKEN:-}"
COORD_ADMIN="${COORD_ADMIN_TOKEN:-${ADMIN_TOKEN:-}}"

if [[ -z "$ADMIN_TOKEN" && -f "$ROOT/.secrets/hackme_admin_token" ]]; then
  ADMIN_TOKEN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_admin_token")"
fi
if [[ -z "$COORD_ADMIN" && -f "$ROOT/.secrets/hackme_coordinator_admin_token" ]]; then
  COORD_ADMIN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_coordinator_admin_token")"
fi

log() { echo "[cancel-gates] $*"; }

is_gate() {
  python3 - <<'PY' <<<"$1"
import json, sys
c = json.loads(sys.stdin.read())
cid = (c.get("id") or "").lower()
title = (c.get("title") or "").lower()
owner = (c.get("owner_ref") or "").lower()
st = (c.get("status") or "").lower()
if st not in ("running", "planned"):
    sys.exit(1)
if cid.startswith("pool-sync-gate") or "pool-sync-gate" in cid or cid.startswith("pool-sync-node-"):
    sys.exit(0)
if cid.endswith("-probe") or "-probe-" in cid or cid.startswith("campaign-gate-"):
    sys.exit(0)
if title in ("pool-sync-gate", "probe", "gate-audit"):
    sys.exit(0)
if "pool sync" in title and "gate" in title:
    sys.exit(0)
if owner.startswith("gate:"):
    sys.exit(0)
sys.exit(1)
PY
}

cancel_local() {
  local id="$1"
  if [[ "$DRY_RUN" == "1" ]]; then
    log "DRY local cancel $id"
    return 0
  fi
  curl -fsS -X POST "${BASE%/}/api/fuzz/campaigns/${id}/status" \
    -H "Content-Type: application/json" \
    -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
    -d '{"status":"cancelled"}' >/dev/null
  log "local cancelled $id"
}

cancel_coord() {
  local id="$1" title="$2"
  if [[ -z "$COORD_ADMIN" ]]; then
    return 0
  fi
  if [[ "$DRY_RUN" == "1" ]]; then
    log "DRY coord cancel $id"
    return 0
  fi
  # Upsert cancelled (works on deployed coordinators without /status route).
  code="$(curl -sS -o /tmp/cancel_gate_resp.json -w '%{http_code}' -X POST \
    "${COORD_URL%/}/api/fuzz/pool/campaigns" \
    -H "X-Hackme-Admin-Token: ${COORD_ADMIN}" \
    -H "Content-Type: application/json" \
    -d "{\"id\":\"${id}\",\"status\":\"cancelled\",\"campaign_type\":\"property\",\"title\":\"${title}\",\"budget_runs\":8,\"config\":{\"pool_distributed\":true,\"internal_gate\":true}}")"
  if [[ "$code" == "200" ]]; then
    log "coord cancelled $id"
  else
    log "coord fail $id HTTP $code"
  fi
}

prune_file() {
  local label="$1" file="$2" n=0
  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    if ! is_gate "$line"; then
      continue
    fi
    id="$(python3 -c 'import json,sys; print(json.loads(sys.stdin.read())["id"])' <<<"$line")"
    title="$(python3 -c 'import json,sys; print(json.loads(sys.stdin.read()).get("title",""))' <<<"$line")"
    log "$label match $id"
    cancel_local "$id" || true
    cancel_coord "$id" "$title" || true
    n=$((n + 1))
  done < <(python3 -c '
import json,sys
d=json.load(sys.stdin)
for c in d.get("campaigns",[]):
    print(json.dumps(c,separators=(",",":")))
' <"$file")
  echo "$n"
}

log "DRY_RUN=$DRY_RUN"
n_local=0
n_coord=0

if curl -fsS --max-time 3 "${BASE%/}/api/status" >/dev/null 2>&1 && [[ -n "$ADMIN_TOKEN" ]]; then
  curl -fsS "${BASE%/}/api/fuzz/campaigns?limit=300" -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
    > /tmp/cancel_gates_local.json
  n_local="$(prune_file local /tmp/cancel_gates_local.json)"
else
  log "skip local"
fi

if [[ -n "$COORD_ADMIN" ]]; then
  curl -fsS "${COORD_URL%/}/api/fuzz/pool/campaigns/list?limit=500" \
    > /tmp/cancel_gates_coord.json 2>/dev/null || echo '{"campaigns":[]}' > /tmp/cancel_gates_coord.json
  n_coord="$(prune_file coord /tmp/cancel_gates_coord.json)"
else
  log "skip coord"
fi

log "done local=$n_local coord=$n_coord"
[[ "$DRY_RUN" == "1" ]] && log "re-run with DRY_RUN=0"
