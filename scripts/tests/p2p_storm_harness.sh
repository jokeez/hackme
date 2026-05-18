#!/usr/bin/env bash
set -euo pipefail

# Local noisy-client harness for P2P ingress storm tests.
#
# Usage:
#   BASE=http://127.0.0.1:8080 P2P_TOKEN=... bash scripts/tests/p2p_storm_harness.sh
#
# Optional:
#   CONCURRENCY=200 REQUESTS=4000 MODE=mixed|handshake|tx|sync

BASE="${BASE:-http://127.0.0.1:8080}"
P2P_TOKEN="${P2P_TOKEN:-${HACKME_P2P_TOKEN:-}}"
CONCURRENCY="${CONCURRENCY:-200}"
REQUESTS="${REQUESTS:-4000}"
MODE="${MODE:-mixed}"
RUN_ID="${RUN_ID:-p2p_storm_$(date -u +%Y%m%dT%H%M%SZ)}"

if [[ -z "$P2P_TOKEN" ]]; then
  echo "p2p_storm_harness: P2P_TOKEN (or HACKME_P2P_TOKEN) required" >&2
  exit 2
fi

out_dir="reports/tests/${RUN_ID}"
mkdir -p "$out_dir"

handshake_body='{"node_id":"storm","height":1,"tip_hash":"x","seen_at":1,"announce_url":"http://10.0.9.9:8080","policy_hash":"invalid"}'
tx_body='{"tx_type":"transfer_v1","from":"HMC-A","to":"HMC-B","amount_units":1000,"fee_units":1000,"nonce":1,"timestamp_unix":1,"pubkey_ed25519":"00","sig_ed25519":"00"}'

one_req() {
  local i="$1"
  local mode="$2"
  local path=""
  local method="GET"
  local body=""
  if [[ "$mode" == "mixed" ]]; then
    case $(( i % 3 )) in
      0) mode="handshake" ;;
      1) mode="tx" ;;
      2) mode="sync" ;;
    esac
  fi
  if [[ "$mode" == "handshake" ]]; then
    path="/api/p2p/handshake"
    method="POST"
    body="$handshake_body"
  elif [[ "$mode" == "tx" ]]; then
    path="/api/p2p/tx"
    method="POST"
    body="$tx_body"
  else
    path="/api/p2p/sync?depth_limit=128"
    method="GET"
  fi

  if [[ "$method" == "POST" ]]; then
    curl -x "" -sS -o /dev/null -w "%{http_code}\n" \
      -X POST \
      -H "X-Hackme-P2P-Token: ${P2P_TOKEN}" \
      -H "Content-Type: application/json" \
      -d "$body" \
      "${BASE}${path}" || echo "000"
  else
    curl -x "" -sS -o /dev/null -w "%{http_code}\n" \
      -H "X-Hackme-P2P-Token: ${P2P_TOKEN}" \
      "${BASE}${path}" || echo "000"
  fi
}

export BASE P2P_TOKEN handshake_body tx_body
export -f one_req

seq 1 "$REQUESTS" | xargs -P "$CONCURRENCY" -I{} bash -lc 'one_req "$@"' _ {} "$MODE" > "${out_dir}/codes.txt"

status_code="$(curl -x "" -sS -o /dev/null -w "%{http_code}" "${BASE}/api/status" || true)"
metrics_code="$(curl -x "" -sS -o /dev/null -w "%{http_code}" "${BASE}/api/metrics" || true)"
global_code="$(curl -x "" -sS -o /dev/null -w "%{http_code}" "${BASE}/api/global/metrics" || true)"

jq -n \
  --arg run_id "$RUN_ID" \
  --arg base "$BASE" \
  --arg mode "$MODE" \
  --argjson requests "$REQUESTS" \
  --argjson concurrency "$CONCURRENCY" \
  --argjson status_code "${status_code:-0}" \
  --argjson metrics_code "${metrics_code:-0}" \
  --argjson global_code "${global_code:-0}" \
  --argjson code_200 "$(jq -R 'select(length>0)' "${out_dir}/codes.txt" | jq -s '[.[]|select(.=="200")]|length')" \
  --argjson code_401 "$(jq -R 'select(length>0)' "${out_dir}/codes.txt" | jq -s '[.[]|select(.=="401")]|length')" \
  --argjson code_403 "$(jq -R 'select(length>0)' "${out_dir}/codes.txt" | jq -s '[.[]|select(.=="403")]|length')" \
  --argjson code_429 "$(jq -R 'select(length>0)' "${out_dir}/codes.txt" | jq -s '[.[]|select(.=="429")]|length')" \
  --argjson code_other "$(jq -R 'select(length>0)' "${out_dir}/codes.txt" | jq -s '[.[]|select(.!="200" and .!="401" and .!="403" and .!="429")]|length')" \
  '{
    test: "p2p_storm_harness_v1",
    run_id: $run_id,
    base: $base,
    mode: $mode,
    requests: $requests,
    concurrency: $concurrency,
    http_codes: {
      "200": $code_200,
      "401": $code_401,
      "403": $code_403,
      "429": $code_429,
      "other": $code_other
    },
    liveness: {
      status_code: $status_code,
      metrics_code: $metrics_code,
      global_metrics_code: $global_code,
      alive: (($status_code == 200) and ($metrics_code == 200) and ($global_code == 200))
    }
  }' > "${out_dir}/summary.json"

cat "${out_dir}/summary.json"
echo "p2p_storm_harness: summary ${out_dir}/summary.json"
