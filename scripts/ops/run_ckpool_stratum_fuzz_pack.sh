#!/usr/bin/env bash
# Deep ckpool stratum parser/share-submit boundary fuzz via HackMe pool.
# Models mempool/ckpool (Con Kolivas) connector + stratifier paths.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

STAMP="$(date +%Y%m%d-%H%M%S)"
PACK_ID="ckpool-fuzz-${STAMP}"
OUT="${OUT:-$ROOT/reports/ckpool-fuzz/${PACK_ID}}"
CKPOOL="${CKPOOL:-$ROOT/.cache/bounty-repos/ckpool}"

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
log() { echo "[ckpool-fuzz] $*" | tee -a "$OUT/run.log"; }

[[ -n "$ADMIN" ]] || { echo "missing admin token" >&2; exit 1; }

probe_base() {
  local url="$1" code
  code="$(curl -sS -o /dev/null -w "%{http_code}" -X POST "$url/api/fuzz/campaigns" \
    -H "X-Hackme-Admin-Token: $ADMIN" -H "Content-Type: application/json" \
    -d "{\"id\":\"ckpool-probe-${STAMP}\",\"campaign_type\":\"property\",\"status\":\"draft\",\"title\":\"probe\",\"budget_runs\":1}" 2>/dev/null)" || code="000"
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
bash "$ROOT/scripts/build_ckpool_fuzz_pack.sh" 2>&1 | tee "$OUT/build.log"

log "ckpool native compile smoke (optional)"
if [[ -d "$CKPOOL" ]] && command -v make >/dev/null; then
  (
    cd "$CKPOOL"
    if [[ -x ./autogen.sh ]]; then ./autogen.sh >/dev/null 2>&1 || true; fi
    if [[ -f configure ]]; then ./configure --quiet 2>/dev/null || true; fi
    make -C src -j"$(nproc 2>/dev/null || echo 4)" ckpool 2>&1 | tail -30
  ) | tee "$OUT/ckpool_make.log" || log "WARN: ckpool make skipped/failed"
else
  log "skip ckpool native (no repo or make)"
fi

hdr_admin=(-H "X-Hackme-Admin-Token: $ADMIN" -H "Content-Type: application/json")
export COORD_URL="$COORD" COORD_TOKEN="$WORKER_TOK"

log "wallet"
curl -fsS "${hdr_admin[@]}" "$BASE/api/wallet" | tee "$OUT/wallet.json" | jq -c '{address,balance_hmc}'
export MINER_ADDRESS="$(jq -r '.address' "$OUT/wallet.json")"

GUARDS=(v1_line_frame submit_hex_fields version_mask submit_param_count ntime_window)
declare -a CIDS=()
declare -a REPORT_TOKENS=()
total_runs=0
total_findings=0
total_critical=0

for g in "${GUARDS[@]}"; do
  WASM="$ROOT/tasks/artifacts/security/ckpool/${g}.wasm"
  [[ -f "$WASM" ]] || { echo "missing $WASM" >&2; exit 1; }
  WASM_HEX="$(xxd -p "$WASM" | tr -d '\n')"
  CID="campaign-ckpool-${g}-${STAMP}"
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
      title: ("ckpool parser — " + $guard),
      description: ("Stratum boundary guard for ckpool/libckpool — " + $guard),
      owner_ref: "bounty:ckpool",
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
  for w in ckpool-a ckpool-b ckpool-c ckpool-d; do
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
python3 "$ROOT/scripts/ops/export_ckpool_fuzz_report_html.py" "$OUT" "$ROOT/web/site/reports/ckpool-fuzz"

log "done → web/site/reports/ckpool-fuzz/"
log "publish: https://hackme.tech/reports/ckpool-fuzz/"
log "pack=$PACK_ID runs=$total_runs findings=$total_findings critical=$total_critical"
