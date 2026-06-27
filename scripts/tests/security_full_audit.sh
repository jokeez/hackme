#!/usr/bin/env bash
# Full cybersecurity audit: unit packs + live red-team + secret scan + optional public surface.
#
# Usage:
#   bash scripts/tests/security_full_audit.sh
#   PUBLIC_BASE=https://hackme.tech/pool bash scripts/tests/security_full_audit.sh
#
# Env:
#   SKIP_LIVE=1           — unit tests only (no ephemeral node)
#   SKIP_PUBLIC=1         — skip public red-team probe
#   SKIP_SECRET_SCAN=1    — skip git-tracked secret grep
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

require_cmd go
require_cmd jq
require_cmd curl
require_cmd python3

OUT_DIR="${OUT_DIR:-$ROOT/reports/tests}"
RID="${RUN_ID:-security_full_$(run_id)}"
OUT="$OUT_DIR/$RID/security_full_audit"
ensure_reports_dir "$OUT"
LOG="$OUT/audit.log"
RESULTS="$OUT/results.jsonl"
: >"$LOG"
: >"$RESULTS"

SKIP_LIVE="${SKIP_LIVE:-0}"
SKIP_PUBLIC="${SKIP_PUBLIC:-0}"
SKIP_SECRET_SCAN="${SKIP_SECRET_SCAN:-0}"
PUBLIC_BASE="${PUBLIC_BASE:-https://hackme.tech/pool}"

record() {
  local id="$1" verdict="$2" detail="$3"
  jq -nc --arg id "$id" --arg verdict "$verdict" --arg detail "$detail" \
    '{id:$id,verdict:$verdict,detail:$detail}' >>"$RESULTS"
  echo "[$verdict] $id — $detail" | tee -a "$LOG"
}

run_case() {
  local id="$1" detail="$2"
  shift 2
  local logf="$OUT/${id}.log"
  if "$@" >"$logf" 2>&1; then
    record "$id" "pass" "$detail"
  else
    record "$id" "fail" "$detail (see $logf)"
  fi
}

echo "=== security full audit RUN_ID=$RID ===" | tee -a "$LOG"

run_case "critical_security_pack" "WASM sandbox, tamper, hybrid signer" \
  env RUN_ID="${RID}_crit" "$ROOT/scripts/tests/critical_security_pack.sh"

run_case "nightly_chaos_guard" "ledger crypto + init-worker + critical pack" \
  env RUN_ID="${RID}_chaos" "$ROOT/scripts/tests/nightly_chaos_guard.sh"

run_case "gputune_security_matrix" "rig profiles + chaos unit tests" \
  go test ./internal/gputune -run 'Chaos|Classify|WASMSandbox|RigProfile' -count=1 -timeout=120s

if [[ "$SKIP_SECRET_SCAN" != "1" ]]; then
  section_log="secret_scan"
  scan_out="$OUT/secret_scan.txt"
  : >"$scan_out"
  if git -C "$ROOT" ls-files | while read -r f; do
    grep -nE 'ghp_[A-Za-z0-9]{20,}|sk_live_|AKIA[0-9A-Z]{16}|BEGIN (RSA |OPENSSH )PRIVATE KEY' "$ROOT/$f" 2>/dev/null && echo "MATCH:$f"
  done | tee -a "$scan_out" | grep -q '^MATCH:'; then
    record "secret_scan" "fail" "possible secrets in tracked files — see $scan_out"
  else
    record "secret_scan" "pass" "no obvious API keys/private keys in git-tracked files"
  fi
fi

if [[ "$SKIP_LIVE" != "1" ]]; then
  ADMIN_TOKEN="${ADMIN_TOKEN:-}"
  if [[ -z "$ADMIN_TOKEN" && -f "$ROOT/.secrets/hackme_admin_token" ]]; then
    ADMIN_TOKEN="$(head -n1 "$ROOT/.secrets/hackme_admin_token" | tr -d '\r\n')"
  fi
  if [[ -z "$ADMIN_TOKEN" ]]; then
    ADMIN_TOKEN="$(python3 -c 'import secrets;print(secrets.token_hex(24))')"
    record "live_stack" "pass" "ephemeral admin token (generated for audit only)"
  fi

  PORT="$(python3 -c "import socket;s=socket.socket();s.bind(('127.0.0.1',0));print(s.getsockname()[1]);s.close()")"
  DATA="$OUT/node_data"
  mkdir -p "$DATA"
  NODE_BIN="$OUT/hackme-audit"
  (cd "$ROOT" && go build -trimpath -o "$NODE_BIN" .)

  HACKME_DATA_DIR="$DATA" \
  HACKME_BIND_ADDR="127.0.0.1:${PORT}" \
  HACKME_ADMIN_TOKEN="$ADMIN_TOKEN" \
  HACKME_REQUIRE_ADMIN_TOKEN=1 \
  HACKME_CHAIN_LEADER_LOCAL_POH=0 \
  HACKME_FUZZ_AUTORUN=0 \
    "$NODE_BIN" >>"$OUT/node.log" 2>&1 &
  npid=$!
  cleanup_node() { kill -TERM "$npid" 2>/dev/null || true; wait "$npid" 2>/dev/null || true; }
  trap cleanup_node EXIT

  BASE="http://127.0.0.1:${PORT}"
  for _ in $(seq 1 50); do
    curl -fsS --max-time 2 "$BASE/api/status" >/dev/null 2>&1 && break
    sleep 0.4
  done

  curl -fsS --max-time 10 -X POST "$BASE/api/genesis" \
    -H "X-Hackme-Admin-Token: $ADMIN_TOKEN" \
    -H "Content-Type: application/json" -d '{}' >/dev/null || true

  curl -fsS --max-time 10 "$BASE/api/status" >"$OUT/live_status.json"
  if jq -e '.sandbox_policy.locked == true and .sandbox_policy.profile == "secure"' "$OUT/live_status.json" >/dev/null; then
    record "sandbox_locked_secure" "pass" "HACKME_SANDBOX_LOCKED forces secure profile"
  else
    record "sandbox_locked_secure" "fail" "sandbox not locked secure — see live_status.json"
  fi

  if jq -e '.admin_auth_enabled == true' "$OUT/live_status.json" >/dev/null; then
    record "admin_auth_enabled" "pass" "requireAdminAuth active"
  else
    record "admin_auth_enabled" "fail" "admin auth disabled on audit node"
  fi

  # Desktop local-auth must not leak token by default
  curl -fsS --max-time 5 "$BASE/api/desktop/local-auth" >"$OUT/local_auth.json" 2>/dev/null || echo '{}' >"$OUT/local_auth.json"
  if jq -e 'has("admin_token") | not' "$OUT/local_auth.json" >/dev/null 2>&1; then
    record "desktop_local_auth_no_leak" "pass" "admin_token not in /api/desktop/local-auth JSON"
  else
    record "desktop_local_auth_no_leak" "fail" "admin_token exposed in local-auth response"
  fi

  run_case "security_assertions" "economics invariants + tx rate limit" \
    env BASE="$BASE" ADMIN_TOKEN="$ADMIN_TOKEN" RUN_ID="${RID}_assert" "$ROOT/scripts/tests/security_assertions.sh"
  run_case "redteam_surface" "unauth mutating routes rejected" \
    env BASE="$BASE" RUN_ID="${RID}_red" "$ROOT/scripts/tests/redteam_surface_smoke.sh"
  run_case "adversarial_api" "malformed tx + tasks contract" \
    env BASE="$BASE" ADMIN_TOKEN="$ADMIN_TOKEN" P2P_TOKEN="$ADMIN_TOKEN" RUN_ID="${RID}_adv" \
    "$ROOT/scripts/tests/adversarial_api_matrix.sh"
  run_case "language_chaos" "from_code alias flood" \
    env BASE="$BASE" ADMIN_TOKEN="$ADMIN_TOKEN" RUN_ID="${RID}_lang" "$ROOT/scripts/tests/language_chaos_security.sh"
  run_case "language_break" "adversarial WASM/code payloads" \
    env BASE="$BASE" ADMIN_TOKEN="$ADMIN_TOKEN" RUN_ID="${RID}_break" "$ROOT/scripts/tests/language_break_attempts.sh"
  run_case "fuzz_runtime_gate" "fuzz campaign admin gate" \
    env BASE="$BASE" ADMIN_TOKEN="$ADMIN_TOKEN" RUN_ID="${RID}_fuzz" "$ROOT/scripts/tests/fuzz_runtime_gate.sh"

  cleanup_node
  trap - EXIT
fi

if [[ "$SKIP_PUBLIC" != "1" ]]; then
  run_case "public_redteam" "public mutating routes ($PUBLIC_BASE)" \
    env BASE="$PUBLIC_BASE" RUN_ID="${RID}_pub" "$ROOT/scripts/tests/redteam_surface_smoke.sh"

  pub_stats="$(curl -sS --max-time 20 -o /dev/null -w '%{http_code}' "${PUBLIC_BASE%/}/coordinator/api/work/stats?details=1" 2>/dev/null || echo 000)"
  if [[ "$pub_stats" == "401" || "$pub_stats" == "403" ]]; then
    record "public_stats_details_auth" "pass" "stats?details=1 requires auth (HTTP $pub_stats)"
  else
    record "public_stats_details_auth" "fail" "stats?details=1 returned HTTP $pub_stats (expected 401/403)"
  fi

  curl -fsS --max-time 25 "${PUBLIC_BASE}/api/status" >"$OUT/public_status.json" 2>/dev/null \
    || curl -fsS --max-time 15 "${PUBLIC_BASE}/api/status?lite=1" >"$OUT/public_status.json" 2>/dev/null \
    || echo '{}' >"$OUT/public_status.json"
  if jq -e '.sandbox_policy.locked == true and .admin_auth_enabled == true' "$OUT/public_status.json" >/dev/null 2>&1; then
    record "public_sandbox_admin" "pass" "public node reports locked sandbox + admin auth"
  else
    record "public_sandbox_admin" "fail" "public status missing sandbox/admin flags"
  fi
fi

fails="$(jq -r 'select(.verdict=="fail") | .id' "$RESULTS" | wc -l | tr -d ' ')"
total="$(wc -l <"$RESULTS" | tr -d ' ')"
status="PASS"
[[ "$fails" -eq 0 ]] || status="FAIL"

jq -nc \
  --arg run_id "$RID" \
  --arg captured_at "$(ts_utc)" \
  --arg status "$status" \
  --argjson total "$total" \
  --argjson fails "$fails" \
  '{run_id:$run_id,captured_at:$captured_at,suite:"security_full_audit",status:$status,total:$total,fails:$fails}' \
  >"$OUT/summary.json"

ln -sfn "$OUT" "$ROOT/reports/security-full-audit-LATEST"

echo "" | tee -a "$LOG"
echo "Audit $status: $((total - fails))/$total passed" | tee -a "$LOG"
echo "Report: $OUT/summary.json" | tee -a "$LOG"

if [[ "$status" != "PASS" ]]; then
  fail "security_full_audit FAIL ($fails/$total). See $OUT"
fi
pass "security_full_audit PASS ($total checks). See $OUT"
