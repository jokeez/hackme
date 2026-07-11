#!/usr/bin/env bash
# Customer smoke: deep pool order on VPS → 50+ pool runs → report + escrow snapshot.
#
#   NODE_SSH=hackme-vps bash scripts/ops/run_customer_pool_smoke.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

NODE_SSH="${NODE_SSH:-hackme-vps}"
RUNS="${RUNS:-64}"
BUDGET_HMC="${BUDGET_HMC:-2.0}"
WORKER_SEC="${WORKER_SEC:-90}"
STAMP="$(date -u +%Y%m%d-%H%M%S)"
CID="customer-smoke-${STAMP}"
LOG="${LOG:-$ROOT/logs/customer_smoke_${STAMP}}"
mkdir -p "$LOG"

log() { echo "[customer-smoke] $*" | tee -a "$LOG/summary.txt"; }

log "cleanup queue on VPS"
NODE_SSH="$NODE_SSH" bash "$ROOT/scripts/ops/coordinator_fuzz_queue_cleanup.sh" >>"$LOG/cleanup.log" 2>&1

log "create deep pool campaign $CID on VPS (runs=$RUNS budget=$BUDGET_HMC)"
ssh -o BatchMode=yes "$NODE_SSH" "cd /opt/hackme && \
  ADMIN=\$(tr -d '\r\n' < .secrets/hackme_admin_token) && \
  WASM_HEX=\$(xxd -p tasks/artifacts/security/rust_script_push_bounds_guard.wasm | tr -d '\n') && \
  curl -fsS -X POST http://127.0.0.1:18080/api/fuzz/campaigns \
    -H 'Content-Type: application/json' -H \"X-Hackme-Admin-Token: \$ADMIN\" \
    -d \"\$(jq -nc --arg id '$CID' --arg wasm \"\$WASM_HEX\" --argjson runs $RUNS --argjson budget $BUDGET_HMC \
      '{id:\$id,campaign_type:\"property\",status:\"running\",title:\"Customer deep pool audit\",owner_ref:\"HMC-customer-demo\",
        budget_runs:\$runs,budget_seconds:7200,budget_hmc:\$budget,
        config:{pool_distributed:true,check_semantics:\"detector\",wasm_check_hex:\$wasm,seed_corpus:[133452,999001],auto_runner:\"0\"}}')\" \
  | tee /tmp/smoke_create.json | jq -c '{ok,pool_sync,escrow:.escrow.status}'"

sleep 5
log "ensure coordinator registration"
ssh "$NODE_SSH" "cd /opt/hackme && CAMPAIGN_ID='$CID' bash scripts/ops/resync_pool_fuzz_campaigns.sh 2>/dev/null || \
  curl -fsS -X POST http://127.0.0.1:18081/api/fuzz/pool/campaigns \
    -H 'X-Hackme-Admin-Token: '\$(tr -d '\r\n' < .secrets/hackme_coordinator_admin_token) \
    -H 'Content-Type: application/json' \
    -d @<(sqlite3 /opt/hackme/data/hackme.db \"SELECT json_object('id',id,'campaign_type',campaign_type,'title',title,'status','running','budget_runs',budget_runs,'budget_seconds',budget_seconds,'config',json(config_json)) FROM fuzz_campaigns WHERE id='$CID'\")" 2>/dev/null | head -c 200 || true

log "run pool workers ${WORKER_SEC}s"
ssh "$NODE_SSH" "cd /opt/hackme && export COORD_URL=http://127.0.0.1:18081 COORD_TOKEN=\$(tr -d '\r\n' < .secrets/hackme_coordinator_worker_token) \
  HACKME_MINER_ED25519_SEED_HEX=\$(tr -d '\r\n' < .secrets/hackme_treasury_ed25519_seed.hex) && \
  for w in smoke-a smoke-b; do WORKER_ID=\$w timeout ${WORKER_SEC}s go run ./cmd/workerfuzz -worker \$w -timeout-ms 800 >>/tmp/smoke_wfuzz.log 2>&1 & done; wait; \
  curl -fsS http://127.0.0.1:18081/api/fuzz/pool/campaigns/progress?id=$CID" | tee "$LOG/progress.json" | jq .

RUNS_DONE="$(jq -r '.runs_done // 0' "$LOG/progress.json")"
if [[ "${RUNS_DONE:-0}" -lt 50 ]]; then
  log "WARN runs_done=$RUNS_DONE (<50), extending worker 45s"
  ssh "$NODE_SSH" "cd /opt/hackme && export COORD_URL=http://127.0.0.1:18081 COORD_TOKEN=\$(tr -d '\r\n' < .secrets/hackme_coordinator_worker_token) \
    HACKME_MINER_ED25519_SEED_HEX=\$(tr -d '\r\n' < .secrets/hackme_treasury_ed25519_seed.hex) && \
    WORKER_ID=smoke-c timeout 45s go run ./cmd/workerfuzz -worker smoke-c -timeout-ms 800 2>&1 | tail -3; \
    curl -fsS http://127.0.0.1:18081/api/fuzz/pool/campaigns/progress?id=$CID" | tee "$LOG/progress2.json" | jq .
  RUNS_DONE="$(jq -r '.runs_done // 0' "$LOG/progress2.json")"
fi

log "replay escrow settle + wait pull"
ssh "$NODE_SSH" "cd /opt/hackme && CAMPAIGN_ID='$CID' COORD_URL=http://127.0.0.1:18081 BASE=http://127.0.0.1:18080 \
  bash scripts/ops/replay_fuzz_escrow_settle.sh 2>&1 | tail -8"

sleep 20
log "escrow + report snapshot"
ssh "$NODE_SSH" "cd /opt/hackme && ADMIN=\$(tr -d '\r\n' < .secrets/hackme_admin_token) && \
  curl -fsS -H \"X-Hackme-Admin-Token: \$ADMIN\" http://127.0.0.1:18080/api/fuzz/campaigns/$CID/escrow | jq . && \
  curl -fsS -H \"X-Hackme-Admin-Token: \$ADMIN\" 'http://127.0.0.1:18080/api/fuzz/campaigns/$CID/report?format=json&limit=20' | jq -c '{ok,runs_done:.summary.runs_done,findings:(.findings|length),verdict:.verdict}'" \
  | tee "$LOG/escrow_report.json"

if [[ "${RUNS_DONE:-0}" -ge 50 ]]; then
  log "PASS customer smoke runs_done=$RUNS_DONE log=$LOG"
  exit 0
fi
log "FAIL customer smoke runs_done=$RUNS_DONE (need 50+) log=$LOG"
exit 1
