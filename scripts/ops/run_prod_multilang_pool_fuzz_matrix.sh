#!/usr/bin/env bash
# Heavy multi-guard pool fuzz matrix on production (operator localhost create + distributed workers).
#
#   NODE_SSH=hackme-vps bash scripts/ops/run_prod_multilang_pool_fuzz_matrix.sh
#   # or on VPS directly:
#   BASE=http://127.0.0.1:18080 COORD=http://127.0.0.1:18081 bash scripts/ops/run_prod_multilang_pool_fuzz_matrix.sh
#
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

if [[ -n "${NODE_SSH:-}" ]]; then
  rsync -az \
    "$ROOT/scripts/ops/run_prod_multilang_pool_fuzz_matrix.sh" \
    "$ROOT/tasks/artifacts/security/"*.wasm \
    "$NODE_SSH:/opt/hackme/scripts/ops/" 2>/dev/null || true
  rsync -az "$ROOT/tasks/artifacts/security/"*.wasm "$NODE_SSH:/opt/hackme/tasks/artifacts/security/" 2>/dev/null || true
  ssh -o BatchMode=yes "$NODE_SSH" \
    "cd /opt/hackme && NODE_SSH= BASE=http://127.0.0.1:18080 COORD=http://127.0.0.1:18081 \
    BUDGET_HMC=${BUDGET_HMC:-1.5} RUNS=${RUNS:-96} WORKER_SEC=${WORKER_SEC:-90} WORKERS=${WORKERS:-4} \
    ADMIN_FILE=/opt/hackme/.secrets/hackme_admin_token \
    WORKER_TOKEN_FILE=/opt/hackme/.secrets/hackme_coordinator_worker_token \
    bash scripts/ops/run_prod_multilang_pool_fuzz_matrix.sh"
  exit $?
fi

BASE="${BASE:-https://hackme.tech}"
COORD="${COORD:-${BASE}/pool/coordinator}"
ADMIN="$(tr -d '\r\n' < "${ADMIN_FILE:-$ROOT/.secrets/hackme_admin_token}")"
WORKER_TOK="$(tr -d '\r\n' < "${WORKER_TOKEN_FILE:-$ROOT/.secrets/hackme_coordinator_worker_token}")"
BUDGET_HMC="${BUDGET_HMC:-1.5}"
RUNS="${RUNS:-96}"
WORKER_SEC="${WORKER_SEC:-90}"
WORKERS="${WORKERS:-4}"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
LOG_DIR="${LOG_DIR:-$ROOT/logs/prod_multilang_matrix_${STAMP}}"
mkdir -p "$LOG_DIR"

hdr=(-H "X-Hackme-Admin-Token: $ADMIN" -H "Content-Type: application/json")

GUARDS=(
  "rust:rust_script_push_bounds_guard.wasm"
  "cpp:cpp_script_push_bounds_guard.wasm"
  "rust:rust_bounds_guard.wasm"
  "cpp:cpp_bounds_guard.wasm"
  "rust:rust_overflow_guard.wasm"
  "cpp:cpp_overflow_guard.wasm"
)

echo "[matrix] log_dir=$LOG_DIR budget=$BUDGET_HMC runs=$RUNS workers=$WORKERS x${WORKER_SEC}s"
curl -fsS "${hdr[@]}" "$BASE/api/wallet" | tee "$LOG_DIR/wallet_before.json" | jq -c '{address,balance_hmc,balance_on_chain_hmc}' || true

TREASURY_SEED_FILE="${TREASURY_SEED_FILE:-$ROOT/.secrets/hackme_treasury_ed25519_seed.hex}"
export COORD_URL="$COORD" COORD_TOKEN="$WORKER_TOK"
export WORKERFUZZ_HTTP_TIMEOUT_SEC="${WORKERFUZZ_HTTP_TIMEOUT_SEC:-120}"
if [[ -f "$TREASURY_SEED_FILE" ]]; then
  export HACKME_MINER_ED25519_SEED_HEX="$(tr -d '\r\n' <"$TREASURY_SEED_FILE")"
else
  export MINER_ADDRESS="$(jq -r '.address // empty' "$LOG_DIR/wallet_before.json")"
fi

curl -fsS "$COORD/api/fuzz/pool/stats" | tee "$LOG_DIR/pool_stats_start.json" | jq -c '{work_done,campaigns_running,work_pending}'

RESULTS="$LOG_DIR/results.jsonl"
: >"$RESULTS"

for entry in "${GUARDS[@]}"; do
  lang="${entry%%:*}"
  wasm_file="${entry#*:}"
  wasm_path="$ROOT/tasks/artifacts/security/$wasm_file"
  guard_id="${wasm_file%.wasm}"
  cid="matrix-${lang}-${guard_id}-${STAMP}"

  if [[ ! -f "$wasm_path" ]]; then
    echo "[matrix] SKIP missing $wasm_path" | tee -a "$LOG_DIR/skip.log"
    jq -nc --arg id "$cid" --arg lang "$lang" --arg guard "$guard_id" \
      '{id:$id,lang:$lang,guard:$guard,verdict:"skip",reason:"missing_wasm"}' >>"$RESULTS"
    continue
  fi

  wasm_hex="$(xxd -p "$wasm_path" | tr -d '\n')"
  echo "[matrix] === create $cid ($lang $guard_id) ==="

  create_body="$(jq -nc \
    --arg id "$cid" \
    --arg wasm "$wasm_hex" \
    --arg lang "$lang" \
    --arg guard "$guard_id" \
    --argjson runs "$RUNS" \
    --argjson budget "$BUDGET_HMC" \
    '{
      id: $id,
      campaign_type: "property",
      status: "running",
      title: ("Matrix " + $lang + " " + $guard),
      description: "Multi-language pool fuzz matrix — escrow 20/80",
      budget_runs: $runs,
      budget_seconds: 1200,
      budget_hmc: $budget,
      config: {
        pool_distributed: true,
        auto_runner: "0",
        check_semantics: "detector",
        wasm_check_hex: $wasm,
        seed_corpus: [133452, 999001, 3735928559, 1312],
        mutation_rounds: 4,
        queue_depth: 128
      }
    }')"

  if ! curl -fsS -X POST "$BASE/api/fuzz/campaigns" "${hdr[@]}" -d "$create_body" \
    | tee "$LOG_DIR/create_${cid}.json" | jq -e '.ok == true' >/dev/null; then
    echo "[matrix] FAIL create $cid" >&2
    jq -nc --arg id "$cid" --arg lang "$lang" --arg guard "$guard_id" \
      '{id:$id,lang:$lang,guard:$guard,verdict:"fail_create"}' >>"$RESULTS"
    continue
  fi

  pool_before="$(curl -fsS "$COORD/api/fuzz/pool/stats" | jq -r '.work_done // 0')"
  pids=()
  for i in $(seq 1 "$WORKERS"); do
    wid="matrix-${lang}-${i}"
    echo "[matrix] worker $wid ${WORKER_SEC}s (parallel)"
    WORKER_ID="$wid" timeout "${WORKER_SEC}s" go run ./cmd/workerfuzz -worker "$wid" -timeout-ms 800 \
      >"$LOG_DIR/worker_${cid}_${wid}.log" 2>&1 &
    pids+=($!)
  done
  for pid in "${pids[@]}"; do wait "$pid" || true; done
  pool_after="$(curl -fsS "$COORD/api/fuzz/pool/stats" | jq -r '.work_done // 0')"
  delta=$((pool_after - pool_before))

  curl -fsS "${hdr[@]}" "$BASE/api/fuzz/campaigns/$cid" \
    | tee "$LOG_DIR/campaign_${cid}.json" >/dev/null
  curl -fsS "${hdr[@]}" "$BASE/api/fuzz/campaigns/$cid/escrow" 2>/dev/null \
    | tee "$LOG_DIR/escrow_${cid}.json" >/dev/null || true

  runs_done="$(jq -r '.campaign.summary.runs_done // .campaign.runs_done // 0' "$LOG_DIR/campaign_${cid}.json")"
  findings="$(jq -r '.findings | length' "$LOG_DIR/campaign_${cid}.json")"
  runs_paid="$(jq -r '.escrow.runs_paid_hmc // 0' "$LOG_DIR/escrow_${cid}.json" 2>/dev/null || echo 0)"
  bounty_paid="$(jq -r '.escrow.bounty_paid_hmc // 0' "$LOG_DIR/escrow_${cid}.json" 2>/dev/null || echo 0)"
  verdict="pass"
  if [[ "$delta" -lt 4 ]]; then verdict="warn_low_work"; fi

  jq -nc \
    --arg id "$cid" --arg lang "$lang" --arg guard "$guard_id" --arg verdict "$verdict" \
    --argjson delta "$delta" --argjson runs_done "$runs_done" --argjson findings "$findings" \
    --argjson runs_paid "$runs_paid" --argjson bounty_paid "$bounty_paid" \
    '{id:$id,lang:$lang,guard:$guard,verdict:$verdict,work_done_delta:$delta,runs_done:$runs_done,findings:$findings,runs_paid_hmc:$runs_paid,bounty_paid_hmc:$bounty_paid}' >>"$RESULTS"

  echo "[matrix] $cid verdict=$verdict delta=$delta runs_done=$runs_done runs_paid=$runs_paid bounty_paid=$bounty_paid findings=$findings"
  sleep 3
done

curl -fsS "$COORD/api/fuzz/pool/stats" | tee "$LOG_DIR/pool_stats_end.json" | jq .

export LOG_DIR STAMP
python3 - <<'PY'
import json, os
from pathlib import Path
log = Path(os.environ["LOG_DIR"])
rows = [json.loads(l) for l in (log/"results.jsonl").read_text().splitlines() if l.strip()]
pass_n = sum(1 for r in rows if r.get("verdict")=="pass")
warn_n = sum(1 for r in rows if r.get("verdict")=="warn_low_work")
fail_n = sum(1 for r in rows if r.get("verdict","").startswith("fail"))
skip_n = sum(1 for r in rows if r.get("verdict")=="skip")
total_runs_paid = sum(float(r.get("runs_paid_hmc") or 0) for r in rows)
total_bounty = sum(float(r.get("bounty_paid_hmc") or 0) for r in rows)
st0 = json.loads((log/"pool_stats_start.json").read_text())
st1 = json.loads((log/"pool_stats_end.json").read_text())
md = log/"VERDICT.md"
md.write_text(f"""# Multi-language pool fuzz matrix — {os.environ.get('STAMP','')}

| Metric | Value |
|--------|-------|
| Guards tested | {len(rows)} |
| PASS | {pass_n} |
| WARN (low work) | {warn_n} |
| FAIL create | {fail_n} |
| SKIP | {skip_n} |
| Pool work_done | {st0.get('work_done')} → {st1.get('work_done')} |
| Total runs_paid_hmc | {total_runs_paid:.6f} |
| Total bounty_paid_hmc | {total_bounty:.6f} |

## Per guard

""" + "\n".join(
    f"- **{r['lang']}** `{r['guard']}` — {r['verdict']} · Δwork={r.get('work_done_delta')} · runs={r.get('runs_done')} · paid={r.get('runs_paid_hmc')} HMC · findings={r.get('findings')}"
    for r in rows
) + f"\n\nLog dir: `{log}`\n")
print(md.read_text())
PY

echo "[matrix] done $LOG_DIR/VERDICT.md"
