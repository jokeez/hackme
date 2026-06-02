#!/usr/bin/env bash
# Run full GPU test session on one Vast instance via SSH.
# Usage: bash scripts/vast/ssh_run_session.sh <instance-name>
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/vast/ssh_common.sh
source "$SCRIPT_DIR/ssh_common.sh"

NAME="${1:-}"
if [[ -z "$NAME" ]]; then
  echo "usage: $0 <instance-name>" >&2
  echo "instances:" >&2
  jq -r '.instances[].name' "$INSTANCES_JSON" 2>/dev/null || echo "(none — create .secrets/vast/instances.json)"
  exit 1
fi

require_instances
mkdir -p "$ROOT/$REMOTE_REPORTS"
LOG_FILE="$ROOT/$REMOTE_REPORTS/${NAME}-$(date -u +%Y%m%dT%H%M%SZ).log"
export LOG_FILE

host="$(instance_field "$NAME" host)"
port="$(instance_field "$NAME" port)"
user="$(instance_field "$NAME" user)"
key="$(instance_field "$NAME" ssh_key)"
wid="$(instance_field "$NAME" worker_id)"
sec="$(instance_field "$NAME" run_seconds)"
fleet="$(instance_field "$NAME" fleet)"
label="$(instance_field "$NAME" gpu_label)"
region="$(instance_field "$NAME" region)"
[[ -n "$region" && "$region" != "null" ]] && label="${region}-${label:-$NAME}"

[[ -n "$host" && "$host" != "null" ]] || { echo "bad host for $NAME"; exit 1; }
[[ -f "$key" ]] || { echo "missing ssh key: $key"; exit 1; }
port="${port:-22}"
user="${user:-root}"
sec="${sec:-2700}"
wid="${wid:-vast-$NAME}"

pack="$(resolve_pack)"
vast_log "session=$NAME label=${label:-?} worker=$wid fleet=$fleet host=$host:$port"

remote_upload_pack "$host" "$port" "$user" "$key" "$pack"

run_script="01_run_pool_worker.sh"
[[ "$fleet" == "true" ]] && run_script="01_run_fleet.sh"

remote_exec "$host" "$port" "$user" "$key" bash -s <<REMOTE
set -euo pipefail
cd ~/vast-gpu-matrix-active
sed -i "s/^WORKER_ID=.*/WORKER_ID=${wid}/" env.vast
export RUN_SECONDS=${sec}
export REPORT=reports/vast-session

bash scripts/00_inventory.sh
REGION_LABEL='${label:-$NAME}'
bash scripts/regional_latency_probe.sh "\${REGION_LABEL}" || true
bash scripts/${run_script}
bash scripts/03_ui_snapshot.sh
bash scripts/02_collect_report.sh

echo "=== remote done ==="
REMOTE

vast_log "fetching reports..."
mkdir -p "$ROOT/$REMOTE_REPORTS/$NAME"
scp -r "$(ssh_opts "$key")" -P "$port" \
  "${user}@${host}:~/vast-gpu-matrix-active/reports/vast-session/" \
  "$ROOT/$REMOTE_REPORTS/$NAME/" 2>/dev/null || vast_log "WARN: scp reports failed"

# Local UI snapshot (prod coordinator)
WORKER_ID="$wid" REPORT="$ROOT/$REMOTE_REPORTS/$NAME" \
  bash "$SCRIPT_DIR/03_ui_snapshot.sh" >>"$LOG_FILE" 2>&1 || true

# Local node APIs → same data as http://127.0.0.1:8080/#mining #hardware #chain
if curl -fsS --max-time 5 "http://127.0.0.1:8080/api/status?lite=1" >/dev/null 2>&1; then
  vast_log "local dashboard probe (127.0.0.1:8080)..."
  OUT="$ROOT/$REMOTE_REPORTS/$NAME" bash "$SCRIPT_DIR/local_dashboard_probe.sh" "$wid" >>"$LOG_FILE" 2>&1 || true
else
  vast_log "WARN: local node :8080 down — open mining UI after: bash scripts/ops/restart_linux_desktop_worker.sh"
fi

vast_log "done → $LOG_FILE and $ROOT/$REMOTE_REPORTS/$NAME/"
