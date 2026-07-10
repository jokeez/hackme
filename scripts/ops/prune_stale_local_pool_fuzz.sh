#!/usr/bin/env bash
# Cancel stale local pool fuzz campaigns that are terminal on the coordinator.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

require_cmd curl jq python3 sqlite3

BASE="${BASE:-http://127.0.0.1:8080}"
ADMIN="$(resolve_admin_token "$ROOT")"
COORD="${HACKME_POOL_COORDINATOR_URL:-https://hackme.tech/pool/coordinator}"
COORD="${COORD%/}"
DB="${HACKME_DATA_DIR:-$ROOT/data}/hackme.db"
[[ -f "$DB" ]] || DB="$ROOT/logs/desktop/data/hackme.db"

log() { echo "[prune-local-fuzz] $*"; }

mapfile -t IDS < <(sqlite3 "$DB" "SELECT id FROM fuzz_campaigns WHERE status IN ('planned','running') AND json_extract(config_json, '\$.pool_distributed') IN (1,'true','1');")

n=0
for id in "${IDS[@]}"; do
  prog="$(curl -fsS --max-time 8 "${COORD}/api/fuzz/pool/campaigns/progress?id=$(python3 -c "import urllib.parse,sys; print(urllib.parse.quote(sys.argv[1]))" "$id")" 2>/dev/null || true)"
  [[ -n "$prog" ]] || continue
  st="$(echo "$prog" | jq -r '.status // empty')"
  if [[ "$st" == "completed" || "$st" == "cancelled" ]]; then
    log "cancel local $id (coordinator=$st)"
    curl -fsS --max-time 15 -X POST "${BASE}/api/fuzz/campaigns/${id}/status" \
      -H "Content-Type: application/json" \
      -H "X-Hackme-Admin-Token: $ADMIN" \
      -d "{\"status\":\"$st\"}" >/dev/null || true
    n=$((n + 1))
  fi
done
log "pruned $n campaigns"
