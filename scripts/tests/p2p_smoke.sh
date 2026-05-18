#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT_DIR/scripts/tests/common.sh"

require_cmd curl
require_cmd jq

BASE="${BASE:-http://127.0.0.1:8080}"
OUT_DIR="${OUT_DIR:-$ROOT_DIR/reports/tests}"
RID="${RUN_ID:-$(run_id)}"
OUT="$OUT_DIR/$RID/p2p"
ensure_reports_dir "$OUT"
results="$OUT/results.jsonl"
: >"$results"

P2P_TOKEN="${P2P_TOKEN:-${HACKME_P2P_TOKEN:-}}"
ADMIN_TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
P2P_MAX_UNSTABLE="${P2P_MAX_UNSTABLE:-1}"
P2P_MAX_BAD="${P2P_MAX_BAD:-1}"
P2P_REQUIRE_HEALTHY="${P2P_REQUIRE_HEALTHY:-0}"

hdr_args=()
if [[ -n "$P2P_TOKEN" ]]; then
  hdr_args+=(-H "X-Hackme-P2P-Token: $P2P_TOKEN")
fi

record() {
  local id="$1" expect="$2" got="$3" body="$4"
  local verdict="pass"
  if [[ "$expect" != "$got" ]]; then verdict="fail"; fi
  jq -nc --arg id "$id" --arg verdict "$verdict" --argjson expect_http "$expect" --argjson got_http "${got:-0}" --arg response "$body" \
    '{id:$id,verdict:$verdict,expect_http:$expect_http,got_http:$got_http,response:$response}' >>"$results"
}

peers_body="$OUT/peers.json"
peers_http="$(curl -sS -o "$peers_body" -w '%{http_code}' "${hdr_args[@]}" "$BASE/api/p2p/peers" || true)"
record "p2p-peers-get" 200 "$peers_http" "$(cat "$peers_body" 2>/dev/null || true)"
if [[ "$peers_http" == "200" ]]; then
  quality_counts="$(jq -c '{
    total: ((.peers // []) | length),
    healthy: ((.peers // []) | map(select(.healthy == true)) | length),
    unstable: ((.peers // []) | map(select(.unstable == true)) | length),
    bad: ((.peers // []) | map(select((.quality // "unknown") == "bad")) | length)
  }' "$peers_body" 2>/dev/null || echo '{}')"
  total_peers="$(jq -r '.total // 0' <<<"$quality_counts" 2>/dev/null || echo "0")"
  healthy_peers="$(jq -r '.healthy // 0' <<<"$quality_counts" 2>/dev/null || echo "0")"
  unstable_peers="$(jq -r '.unstable // 0' <<<"$quality_counts" 2>/dev/null || echo "0")"
  bad_peers="$(jq -r '.bad // 0' <<<"$quality_counts" 2>/dev/null || echo "0")"

  all_down_ok="true"
  if [[ "$P2P_REQUIRE_HEALTHY" == "1" && "$total_peers" != "0" && "$healthy_peers" == "0" ]]; then
    all_down_ok="false"
  fi
  if [[ "$all_down_ok" == "true" ]]; then
    jq -nc --arg id "p2p-quality-all-down" --arg verdict "pass" --argjson total "$total_peers" --argjson healthy "$healthy_peers" \
      '{id:$id,verdict:$verdict,total:$total,healthy:$healthy}' >>"$results"
  else
    jq -nc --arg id "p2p-quality-all-down" --arg verdict "fail" --argjson total "$total_peers" --argjson healthy "$healthy_peers" \
      '{id:$id,verdict:$verdict,total:$total,healthy:$healthy}' >>"$results"
  fi

  unstable_ok="true"
  if (( unstable_peers > P2P_MAX_UNSTABLE )); then
    unstable_ok="false"
  fi
  if [[ "$unstable_ok" == "true" ]]; then
    jq -nc --arg id "p2p-quality-unstable-budget" --arg verdict "pass" --argjson unstable "$unstable_peers" --argjson max "$P2P_MAX_UNSTABLE" \
      '{id:$id,verdict:$verdict,unstable:$unstable,max:$max}' >>"$results"
  else
    jq -nc --arg id "p2p-quality-unstable-budget" --arg verdict "fail" --argjson unstable "$unstable_peers" --argjson max "$P2P_MAX_UNSTABLE" \
      '{id:$id,verdict:$verdict,unstable:$unstable,max:$max}' >>"$results"
  fi

  bad_ok="true"
  if (( bad_peers > P2P_MAX_BAD )); then
    bad_ok="false"
  fi
  if [[ "$bad_ok" == "true" ]]; then
    jq -nc --arg id "p2p-quality-bad-budget" --arg verdict "pass" --argjson bad "$bad_peers" --argjson max "$P2P_MAX_BAD" \
      '{id:$id,verdict:$verdict,bad:$bad,max:$max}' >>"$results"
  else
    jq -nc --arg id "p2p-quality-bad-budget" --arg verdict "fail" --argjson bad "$bad_peers" --argjson max "$P2P_MAX_BAD" \
      '{id:$id,verdict:$verdict,bad:$bad,max:$max}' >>"$results"
  fi
fi

local_policy_hash="$(curl -sS "$BASE/api/status" | jq -r '.economics.policy_hash // ""' 2>/dev/null || true)"
hs_payload="$(jq -nc --arg node_id "smoke-$RID" --arg ann "$BASE" --arg ph "$local_policy_hash" '{node_id:$node_id,height:0,tip_hash:"",seen_at:0,announce_url:$ann,policy_hash:$ph}')"
hs_body="$OUT/handshake.json"
hs_http="$(curl -sS -o "$hs_body" -w '%{http_code}' "${hdr_args[@]}" -X POST "$BASE/api/p2p/handshake" -H "Content-Type: application/json" -d "$hs_payload" || true)"
hs_verdict="fail"
if [[ "$hs_http" == "200" || "$hs_http" == "401" ]]; then
  hs_verdict="pass"
fi
jq -nc --arg id "p2p-handshake-post" --arg verdict "$hs_verdict" --argjson got_http "${hs_http:-0}" --arg response "$(cat "$hs_body" 2>/dev/null || true)" \
  '{id:$id,verdict:$verdict,got_http:$got_http,response:$response}' >>"$results"

bad_tx_body="$OUT/p2p_tx_bad.json"
bad_tx_http="$(curl -sS -o "$bad_tx_body" -w '%{http_code}' "${hdr_args[@]}" -X POST "$BASE/api/p2p/tx" -H "Content-Type: application/json" -d '{}' || true)"
bad_tx_http="$(printf '%s' "$bad_tx_http" | tr -d '\r\n[:space:]')"
bad_tx_accept="false"
if [[ "$bad_tx_http" == "400" || "$bad_tx_http" == "429" ]]; then
  bad_tx_accept="true"
elif [[ "$bad_tx_http" == "200" ]]; then
  if jq -e '(.ok == false) and (((.code // "") | tostring | length) > 0)' "$bad_tx_body" >/dev/null 2>&1; then
    bad_tx_accept="true"
  fi
elif [[ "$bad_tx_http" == "401" ]]; then
  bad_tx_accept="true"
fi
if [[ "$bad_tx_accept" == "true" ]]; then
  jq -nc --arg id "p2p-tx-invalid-rejected" --arg verdict "pass" --argjson got_http "${bad_tx_http:-0}" --arg response "$(cat "$bad_tx_body" 2>/dev/null || true)" \
    '{id:$id,verdict:$verdict,got_http:$got_http,response:$response}' >>"$results"
else
  jq -nc --arg id "p2p-tx-invalid-rejected" --arg verdict "fail" --argjson got_http "${bad_tx_http:-0}" --arg response "$(cat "$bad_tx_body" 2>/dev/null || true)" \
    '{id:$id,verdict:$verdict,got_http:$got_http,response:$response}' >>"$results"
fi

peers_public_body="$OUT/peers_public.json"
peers_public_http="$(curl -sS -o "$peers_public_body" -w '%{http_code}' "$BASE/api/p2p/peers" || true)"
if [[ "$peers_public_http" == "200" ]]; then
  jq -nc --arg id "p2p-peers-public-readable" --arg verdict "pass" --argjson got_http "${peers_public_http:-0}" --arg response "$(cat "$peers_public_body" 2>/dev/null || true)" \
    '{id:$id,verdict:$verdict,got_http:$got_http,response:$response}' >>"$results"
else
  jq -nc --arg id "p2p-peers-public-readable" --arg verdict "fail" --argjson got_http "${peers_public_http:-0}" --arg response "$(cat "$peers_public_body" 2>/dev/null || true)" \
    '{id:$id,verdict:$verdict,got_http:$got_http,response:$response}' >>"$results"
fi

bad_hs_body="$OUT/handshake_bad_payload.json"
bad_hs_http="$(curl -sS -o "$bad_hs_body" -w '%{http_code}' "${hdr_args[@]}" -X POST "$BASE/api/p2p/handshake" -H "Content-Type: application/json" -d '{"bad":"payload"}' || true)"
bad_hs_accept="false"
if [[ "$bad_hs_http" == "400" || "$bad_hs_http" == "403" || "$bad_hs_http" == "415" || "$bad_hs_http" == "422" || "$bad_hs_http" == "429" ]]; then
  bad_hs_accept="true"
elif [[ "$bad_hs_http" == "200" ]]; then
  if jq -e '(.ok == false) and (((.code // "") | tostring | length) > 0)' "$bad_hs_body" >/dev/null 2>&1; then
    bad_hs_accept="true"
  fi
elif [[ "$bad_hs_http" == "401" ]]; then
  bad_hs_accept="true"
fi
if [[ "$bad_hs_accept" == "true" ]]; then
  jq -nc --arg id "p2p-handshake-malformed-rejected" --arg verdict "pass" --argjson got_http "${bad_hs_http:-0}" --arg response "$(cat "$bad_hs_body" 2>/dev/null || true)" \
    '{id:$id,verdict:$verdict,got_http:$got_http,response:$response}' >>"$results"
else
  jq -nc --arg id "p2p-handshake-malformed-rejected" --arg verdict "fail" --argjson got_http "${bad_hs_http:-0}" --arg response "$(cat "$bad_hs_body" 2>/dev/null || true)" \
    '{id:$id,verdict:$verdict,got_http:$got_http,response:$response}' >>"$results"
fi

peers_retry_body="$OUT/peers_retry.json"
peers_retry_http="$(curl -sS -o "$peers_retry_body" -w '%{http_code}' "${hdr_args[@]}" "$BASE/api/p2p/peers" || true)"
record "p2p-peers-get-retry" 200 "$peers_retry_http" "$(cat "$peers_retry_body" 2>/dev/null || true)"

sync_body="$OUT/p2p_sync.json"
sync_http="$(curl -sS -o "$sync_body" -w '%{http_code}' "${hdr_args[@]}" "$BASE/api/p2p/sync" || true)"
record "p2p-sync-get" 200 "$sync_http" "$(cat "$sync_body" 2>/dev/null || true)"
if [[ "$sync_http" == "200" ]]; then
  if jq -e '(.enabled == false) or (has("sync_blocked") and has("sync_blocked_code") and has("sync_action"))' "$sync_body" >/dev/null 2>&1; then
    jq -nc --arg id "p2p-sync-blocked-fields" --arg verdict "pass" --arg response "$(cat "$sync_body" 2>/dev/null || true)" \
      '{id:$id,verdict:$verdict,response:$response}' >>"$results"
  else
    jq -nc --arg id "p2p-sync-blocked-fields" --arg verdict "fail" --arg response "$(cat "$sync_body" 2>/dev/null || true)" \
      '{id:$id,verdict:$verdict,response:$response}' >>"$results"
  fi
fi

if [[ -n "$ADMIN_TOKEN" ]]; then
  sync_run_body="$OUT/p2p_sync_run.json"
  sync_run_http="$(curl -sS -o "$sync_run_body" -w '%{http_code}' \
    -X POST -H "X-Hackme-Admin-Token: $ADMIN_TOKEN" \
    "$BASE/api/p2p/sync/run?depth_limit=8&max_apply=8" || true)"
  if [[ "$sync_run_http" == "200" ]]; then
    if jq -e '((.ok == false) and ((.code // "") == "sync_apply_disabled_no_state_replay_v1" or (.code // "") == "fork_detected_no_reorg_v1" or (.code // "") == "plan_not_ready" or (.code // "") == "p2p_disabled")) or ((.ok == true) and (((.apply.reason // "") == "ok") or ((.apply.reason // "") == "empty_stage")))' "$sync_run_body" >/dev/null 2>&1; then
      jq -nc --arg id "p2p-sync-run-contract" --arg verdict "pass" --arg response "$(cat "$sync_run_body" 2>/dev/null || true)" \
        '{id:$id,verdict:$verdict,response:$response}' >>"$results"
    else
      jq -nc --arg id "p2p-sync-run-contract" --arg verdict "fail" --arg response "$(cat "$sync_run_body" 2>/dev/null || true)" \
        '{id:$id,verdict:$verdict,response:$response}' >>"$results"
    fi
  else
    jq -nc --arg id "p2p-sync-run-contract" --arg verdict "fail" --argjson got_http "${sync_run_http:-0}" --arg response "$(cat "$sync_run_body" 2>/dev/null || true)" \
      '{id:$id,verdict:$verdict,got_http:$got_http,response:$response}' >>"$results"
  fi
fi

fails="$(jq -r 'select(.verdict=="fail") | .id' "$results" | wc -l | tr -d ' ')"
total="$(wc -l <"$results" | tr -d ' ')"
jq -nc --arg run_id "$RID" --arg base "$BASE" --arg captured_at "$(ts_utc)" --argjson total "$total" --argjson fails "$fails" \
  --argjson max_unstable "$P2P_MAX_UNSTABLE" --argjson max_bad "$P2P_MAX_BAD" \
  '{run_id:$run_id,base:$base,captured_at:$captured_at,total:$total,fails:$fails,max_unstable:$max_unstable,max_bad:$max_bad,status:(if $fails==0 then "PASS" else "FAIL" end)}' >"$OUT/summary.json"

if [[ "$fails" != "0" ]]; then
  fail "p2p smoke FAIL ($fails/$total). See $OUT"
fi
pass "p2p smoke PASS ($total checks). See $OUT"

