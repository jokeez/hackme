#!/usr/bin/env bash
# Deep stratum-mining/stratum Sv2 reference boundary fuzz via HackMe pool.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

STAMP="$(date +%Y%m%d-%H%M%S)"
PACK_ID="stratum-mining-fuzz-${STAMP}"
OUT="${OUT:-$ROOT/reports/stratum-mining-fuzz/${PACK_ID}}"
STRATUM="${STRATUM:-$ROOT/.cache/bounty-repos/stratum-mining}"

BASE="${BASE:-https://hackme.tech}"
COORD="${COORD:-${BASE}/pool/coordinator}"
ADMIN="$(tr -d '\r\n' < "${ADMIN_FILE:-$ROOT/.secrets/hackme_admin_token}")"
WORKER_TOK="$(tr -d '\r\n' < "${WORKER_TOKEN_FILE:-$ROOT/.secrets/hackme_coordinator_worker_token}")"

USE_POOL_DIST="${USE_POOL_DIST:-auto}"
AUTO_RUNNER="${AUTO_RUNNER:-auto}"

RUNS="${RUNS:-4096}"
BUDGET_HMC="${BUDGET_HMC:-3.0}"
WORKER_SEC="${WORKER_SEC:-120}"
MUT_ROUNDS="${MUTATION_ROUNDS:-128}"
export WORKERFUZZ_HTTP_TIMEOUT_SEC="${WORKERFUZZ_HTTP_TIMEOUT_SEC:-180}"

mkdir -p "$OUT"
log() { echo "[stratum-mining-fuzz] $*" | tee -a "$OUT/run.log"; }

[[ -n "$ADMIN" ]] || { echo "missing admin token" >&2; exit 1; }

probe_base() {
  local url="$1" code
  code="$(curl -sS -o /dev/null -w "%{http_code}" -X POST "$url/api/fuzz/campaigns" \
    -H "X-Hackme-Admin-Token: $ADMIN" -H "Content-Type: application/json" \
    -d "{\"id\":\"stratum-mining-probe-${STAMP}\",\"campaign_type\":\"property\",\"status\":\"draft\",\"title\":\"probe\",\"budget_runs\":1}" 2>/dev/null)" || code="000"
  echo "$code"
}

if [[ "$BASE" == https://hackme.tech* ]]; then
  code="$(probe_base "$BASE")"
  if [[ "$code" == "403" || "$code" == "000" ]]; then
    log "prod POST blocked (HTTP $code) — switching to local node"
    BASE="http://127.0.0.1:8080"
    COORD="${COORD:-https://hackme.tech/pool/coordinator}"
    USE_POOL_DIST="0"
    AUTO_RUNNER="1"
  fi
fi

if [[ "$USE_POOL_DIST" == "auto" ]]; then USE_POOL_DIST="1"; fi
if [[ "$AUTO_RUNNER" == "auto" ]]; then AUTO_RUNNER="0"; fi
if [[ "$USE_POOL_DIST" == "0" ]]; then AUTO_RUNNER="1"; fi
[[ -n "$WORKER_TOK" ]] || USE_POOL_DIST="0"

log "base=$BASE pool_distributed=$USE_POOL_DIST auto_runner=$AUTO_RUNNER pack=$PACK_ID"

log "build WASM guards"
bash "$ROOT/scripts/build_stratum_mining_fuzz_pack.sh" 2>&1 | tee "$OUT/build.log"

log "native cargo test smoke (optional)"
if [[ -d "$STRATUM" ]] && command -v cargo >/dev/null; then
  (
    cd "$STRATUM"
    cargo test -p buffer-sv2 -p channels-sv2 --no-fail-fast 2>&1
  ) | tee "$OUT/native_cargo_test.log" | tail -40 || log "WARN: cargo test skipped/failed"
else
  log "skip native cargo (no repo or cargo)"
fi

hdr_admin=(-H "X-Hackme-Admin-Token: $ADMIN" -H "Content-Type: application/json")
export COORD_URL="$COORD" COORD_TOKEN="$WORKER_TOK"

log "wallet"
curl -fsS "${hdr_admin[@]}" "$BASE/api/wallet" | tee "$OUT/wallet.json" | jq -c '{address,balance_hmc}'
export MINER_ADDRESS="$(jq -r '.address' "$OUT/wallet.json")"

GUARDS=(sv2_frame_bounds extranonce_len user_identity_len open_channel_target)
declare -a CIDS=()
declare -a REPORT_TOKENS=()
total_runs=0
total_findings=0
total_critical=0

for g in "${GUARDS[@]}"; do
  WASM="$ROOT/tasks/artifacts/security/stratum_mining/${g}.wasm"
  [[ -f "$WASM" ]] || { echo "missing $WASM" >&2; exit 1; }
  WASM_HEX="$(xxd -p "$WASM" | tr -d '\n')"
  CID="campaign-stratum-mining-${g}-${STAMP}"
  CIDS+=("$CID")

  log "create campaign $CID guard=$g runs=$RUNS budget_hmc=$BUDGET_HMC"
  create_body="$(jq -nc \
    --arg id "$CID" \
    --arg wasm "$WASM_HEX" \
    --arg guard "$g" \
    --argjson runs "$RUNS" \
    --argjson budget "$BUDGET_HMC" \
    --argjson mut "$MUT_ROUNDS" \
    --argjson pool_dist "$USE_POOL_DIST" \
    --arg auto_run "$AUTO_RUNNER" \
    '{
      id: $id,
      campaign_type: "property",
      status: "running",
      title: ("stratum-mining parser — " + $guard),
      description: ("Sv2 boundary guard for stratum-mining/stratum — " + $guard),
      owner_ref: "bounty:stratum-mining",
      budget_runs: $runs,
      budget_seconds: 7200,
      budget_hmc: $budget,
      config: {
        pool_distributed: ($pool_dist != 0),
        auto_runner: $auto_run,
        check_semantics: "detector",
        wasm_check_hex: $wasm,
        mutation_rounds: $mut,
        queue_depth: 256,
        worker_batch: 64,
        seed_corpus: [42, 1024, 512, 7000, 268435456]
      }
    }')"

  curl -fsS -X POST "$BASE/api/fuzz/campaigns" "${hdr_admin[@]}" -d "$create_body" \
    | tee "$OUT/create_${g}.json" | jq -c '{id:.campaign.id,status:.campaign.status,escrow:.escrow.status}'
  REPORT_TOKENS+=("$(jq -r '.customer_report_token // empty' "$OUT/create_${g}.json")")
done

log "pool stats before workers"
if [[ "$USE_POOL_DIST" != "0" ]]; then
  curl -fsS "$COORD/api/fuzz/pool/stats" | tee "$OUT/pool_stats_before.json" | jq -c '{work_done,workers}' || true
  log "run workerfuzz ${WORKER_SEC}s x4 on coordinator pool"
  for w in stratum-mining-a stratum-mining-b stratum-mining-c stratum-mining-d; do
    WORKER_ID="$w" timeout "${WORKER_SEC}s" go run ./cmd/workerfuzz -worker "$w" -timeout-ms 1200 \
      >"$OUT/worker_${w}.log" 2>&1 || true
  done
  curl -fsS "$COORD/api/fuzz/pool/stats" | tee "$OUT/pool_stats_after.json" | jq -c '{work_done,workers}' || true
else
  log "local auto_runner mode — skip coordinator workers"
fi

log "poll campaigns (max 600s each)"
for i in "${!GUARDS[@]}"; do
  g="${GUARDS[$i]}"
  CID="${CIDS[$i]}"
  RT="${REPORT_TOKENS[$i]}"
  for tick in $(seq 1 300); do
    curl -fsS "${hdr_admin[@]}" "$BASE/api/fuzz/campaigns/$CID" >"$OUT/campaign_${g}.json"
    st="$(jq -r '.campaign.status // "?"' "$OUT/campaign_${g}.json")"
    done_n="$(jq -r '.campaign.summary.runs_done // 0' "$OUT/campaign_${g}.json")"
    bud="$(jq -r '.campaign.budget_runs // 0' "$OUT/campaign_${g}.json")"
    log "  $g tick $tick status=$st runs=$done_n/$bud"
    [[ "$st" == "completed" || "$st" == "failed" ]] && break
    sleep 2
  done

  report_hdr=(-H "X-Hackme-Admin-Token: $ADMIN")
  [[ -n "$RT" ]] && report_hdr+=(-H "X-Hackme-Report-Token: $RT")

  curl -fsS "${report_hdr[@]}" "$BASE/api/fuzz/campaigns/$CID/report?format=json&limit=200" \
    | jq . | tee "$OUT/report_${g}.json"
  curl -fsS "${report_hdr[@]}" "$BASE/api/fuzz/campaigns/$CID/report.html?limit=200" \
    -o "$OUT/report_${g}.html"

  r="$(jq -r '.campaign.summary.runs_done // 0' "$OUT/report_${g}.json" 2>/dev/null || echo 0)"
  f="$(jq -r '.totals.findings_total // .security_summary.vulnerabilities_found // 0' "$OUT/report_${g}.json" 2>/dev/null || echo 0)"
  c="$(jq -r '.security_summary.critical_count // 0' "$OUT/report_${g}.json" 2>/dev/null || echo 0)"
  total_runs=$((total_runs + r))
  total_findings=$((total_findings + f))
  total_critical=$((total_critical + c))
done

jq -nc \
  --arg pack "$PACK_ID" \
  --argjson runs "$total_runs" \
  --argjson findings "$total_findings" \
  --argjson critical "$total_critical" \
  --argjson guards "${#GUARDS[@]}" \
  '{pack_id:$pack,total_runs:$runs,total_findings:$findings,total_critical:$critical,guards:$guards}' \
  | tee "$OUT/PACK_SUMMARY.json"

log "export fuzz_report_v2 HTML"
python3 "$ROOT/scripts/ops/export_stratum_mining_fuzz_report_html.py" "$OUT" "$ROOT/web/site/reports/stratum-mining-fuzz"

log "done → web/site/reports/stratum-mining-fuzz/"
log "publish: https://hackme.tech/reports/stratum-mining-fuzz/"
log "pack=$PACK_ID runs=$total_runs findings=$total_findings critical=$total_critical"
