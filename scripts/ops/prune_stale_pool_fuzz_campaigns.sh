#!/usr/bin/env bash
# Cancel stale pool fuzz campaigns (health-check gates, zero-budget zombies).
#
# Usage:
#   DRY_RUN=1 bash scripts/ops/prune_stale_pool_fuzz_campaigns.sh
#   DRY_RUN=0 bash scripts/ops/prune_stale_pool_fuzz_campaigns.sh
#
# Targets (status=running|planned):
#   - id prefix pool-sync-gate
#   - title matches pool sync gate (case-insensitive) AND budget_hmc=0
#   - budget_hmc=0 AND check_semantics=pow_gate AND runs_done=0
#
# Env:
#   BASE=http://127.0.0.1:8080          local node (optional)
#   COORD_URL=https://hackme.tech/pool/coordinator
#   ADMIN_TOKEN / COORD_ADMIN_TOKEN
#   MAX_AGE_DAYS=0 — if >0, also prune active campaigns older than N days with runs_done=0

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BASE="${BASE:-http://127.0.0.1:8080}"
COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
ADMIN_TOKEN="${ADMIN_TOKEN:-}"
COORD_ADMIN="${COORD_ADMIN_TOKEN:-${ADMIN_TOKEN:-}}"
DRY_RUN="${DRY_RUN:-1}"
MAX_AGE_DAYS="${MAX_AGE_DAYS:-0}"

if [[ -z "$ADMIN_TOKEN" && -f "$ROOT/.secrets/hackme_admin_token" ]]; then
  ADMIN_TOKEN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_admin_token")"
fi
if [[ -z "$COORD_ADMIN" && -f "$ROOT/.secrets/hackme_coordinator_admin_token" ]]; then
  COORD_ADMIN="$(tr -d '\r\n' <"$ROOT/.secrets/hackme_coordinator_admin_token")"
fi

hdr=(-H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" -H "Content-Type: application/json")

log() { echo "[prune-pool-fuzz] $*"; }

should_prune() {
  local json="$1"
  python3 - "$MAX_AGE_DAYS" <<'PY' <<<"$json"
import json, sys, time
c = json.loads(sys.stdin.read())
max_age = int(sys.argv[1])
cid = (c.get("id") or "").strip()
title = (c.get("title") or "").strip().lower()
status = (c.get("status") or "").strip().lower()
if status not in ("running", "planned"):
    sys.exit(1)
runs = int(c.get("runs_done") or 0)
budget = float(c.get("budget_hmc") or 0)
sem = (c.get("check_semantics") or "").strip().lower()
created = int(c.get("created_at") or 0)
age_days = (time.time() - created) / 86400.0 if created else 0
if cid.startswith("pool-sync-gate"):
    print("pool-sync-gate id"); sys.exit(0)
if "pool sync gate" in title and budget <= 0:
    print("pool sync gate title zero budget"); sys.exit(0)
if budget <= 0 and sem == "pow_gate" and runs == 0:
    print("zero-budget pow_gate no runs"); sys.exit(0)
if max_age > 0 and runs == 0 and age_days >= max_age and budget <= 0.01:
    print(f"stale {age_days:.0f}d zero progress"); sys.exit(0)
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
    "${hdr[@]}" -d '{"status":"cancelled"}' >/dev/null
  log "local cancelled $id"
}

cancel_coord() {
  local id="$1"
  if [[ -z "$COORD_ADMIN" ]]; then
    log "skip coord $id (no COORD_ADMIN)"
    return 0
  fi
  if [[ "$DRY_RUN" == "1" ]]; then
    log "DRY coord cancel $id"
    return 0
  fi
  code="$(curl -sS -o /tmp/prune_coord_resp.json -w '%{http_code}' -X POST \
    "${COORD_URL%/}/api/fuzz/pool/campaigns/status" \
    -H "X-Hackme-Admin-Token: ${COORD_ADMIN}" \
    -H "Content-Type: application/json" \
    -d "{\"id\":\"${id}\",\"status\":\"cancelled\"}")"
  if [[ "$code" == "200" ]]; then
    log "coord cancelled $id"
  else
    code="$(curl -sS -o /tmp/prune_coord_resp.json -w '%{http_code}' -X POST \
      "${COORD_URL%/}/api/fuzz/pool/campaigns" \
      -H "X-Hackme-Admin-Token: ${COORD_ADMIN}" \
      -H "Content-Type: application/json" \
      -d "{\"id\":\"${id}\",\"status\":\"cancelled\",\"campaign_type\":\"property\",\"title\":\"pool-sync-gate\",\"budget_runs\":8,\"config\":{\"pool_distributed\":true,\"internal_gate\":true}}")"
    if [[ "$code" == "200" ]]; then
      log "coord cancelled $id (upsert)"
    else
      log "coord API unavailable for $id HTTP $code — try SQL fallback (COORD_SQL_DB)"
      if [[ -n "${COORD_SQL_DB:-}" && "$DRY_RUN" != "1" ]]; then
        sqlite3 "$COORD_SQL_DB" "UPDATE fuzz_campaigns SET status='cancelled', completed_at=strftime('%s','now') WHERE id='$id' AND status IN ('running','planned'); UPDATE fuzz_work_items SET status='cancelled' WHERE campaign_id='$id' AND status IN ('pending','leased');" && log "sql cancelled $id"
      fi
    fi
  fi
}

prune_source() {
  local label="$1"
  local json_file="$2"
  local n=0
  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    id="$(echo "$line" | python3 -c 'import json,sys; print(json.loads(sys.stdin.read())["id"])')"
    reason="$(should_prune "$line" || true)"
    if [[ -z "$reason" ]]; then
      continue
    fi
    log "$label match $id ($reason)"
    n=$((n + 1))
    cancel_local "$id" || true
    cancel_coord "$id" || true
  done < <(python3 -c '
import json,sys
d=json.load(sys.stdin)
rows=d.get("campaigns",[])
for c in rows:
    print(json.dumps(c,separators=(",",":")))
' <"$json_file")
  echo "$n"
}

log "DRY_RUN=$DRY_RUN BASE=$BASE COORD=$COORD_URL"

local_n=0
coord_n=0

if curl -fsS --max-time 3 "${BASE%/}/api/status" >/dev/null 2>&1 && [[ -n "$ADMIN_TOKEN" ]]; then
  curl -fsS "${BASE%/}/api/fuzz/campaigns?limit=200" -H "X-Hackme-Admin-Token: ${ADMIN_TOKEN}" \
    > /tmp/prune_local_campaigns.json
  local_n="$(prune_source local /tmp/prune_local_campaigns.json)"
else
  log "skip local (node down or no ADMIN_TOKEN)"
fi

if [[ -n "$COORD_ADMIN" ]]; then
  curl -fsS "${COORD_URL%/}/api/fuzz/pool/campaigns/list?limit=500&all=1" \
    -H "X-Hackme-Admin-Token: ${COORD_ADMIN}" \
    > /tmp/prune_coord_campaigns.json 2>/dev/null || echo '{"campaigns":[]}' > /tmp/prune_coord_campaigns.json
  coord_n="$(prune_source coord /tmp/prune_coord_campaigns.json)"
else
  log "skip coord list (no COORD_ADMIN)"
fi

log "done local_matches=$local_n coord_pass=$coord_n"
if [[ "$DRY_RUN" == "1" ]]; then
  log "re-run with DRY_RUN=0 to apply"
fi
