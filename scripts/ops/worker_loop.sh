#!/usr/bin/env bash
set -euo pipefail

# Resilient coordinator worker loop:
# - claim -> submit cycle
# - adaptive backoff on 429/409/network errors
# - periodic one-line status logs

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[worker] missing command: $1" >&2
    exit 1
  }
}

require_cmd curl
require_cmd jq

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

COORD_URL="${COORD_URL:-http://127.0.0.1:8081}"
WORKER_ID="${WORKER_ID:-worker-local-01}"
BATCH_SIZE="${BATCH_SIZE:-2000000}"
HASHRATE_GHS="${HASHRATE_GHS:-0.9}"
MIN_HASHRATE_GHS="${MIN_HASHRATE_GHS:-0.001}"
MAX_HASHRATE_GHS="${MAX_HASHRATE_GHS:-200.0}"
EMA_ALPHA="${EMA_ALPHA:-0.35}"
WORKER_NAME="${WORKER_NAME:-${WORKER_ID}}"
COORD_PUSH_WORK="${COORD_PUSH_WORK:-1}"
# Coordinator claim/submit needs HACKME_COORDINATOR_ADMIN_TOKEN from the coordinator process,
# not the command-node admin. Precedence: explicit coord env > on-disk coord secret > generic admin env.
TOKEN="${COORD_ADMIN_TOKEN:-${HACKME_COORDINATOR_ADMIN_TOKEN:-${COORD_TOKEN:-}}}"
if [[ -z "$TOKEN" && -f "$ROOT_DIR/.secrets/hackme_coordinator_worker_token" ]]; then
  TOKEN="$(head -n1 "$ROOT_DIR/.secrets/hackme_coordinator_worker_token" | tr -d '\r\n')"
fi
if [[ -z "$TOKEN" && -f "$ROOT_DIR/.secrets/hackme_coordinator_admin_token" ]]; then
  TOKEN="$(head -n1 "$ROOT_DIR/.secrets/hackme_coordinator_admin_token" | tr -d '\r\n')"
fi
if [[ -z "$TOKEN" ]]; then
  TOKEN="${ADMIN_TOKEN:-${HACKME_ADMIN_TOKEN:-}}"
fi

SIGN_SUBMITS="${HACKME_WORKER_SIGN_SUBMITS:-0}"
NONCE_FILE="${HACKME_MINER_NONCE_FILE:-${ROOT_DIR}/logs/miner_submit_nonce.seq}"
MINERSIGN_BIN="${MINERSIGN_BIN:-}"
mkdir -p "$(dirname "$NONCE_FILE")"

if [[ -z "$TOKEN" ]]; then
  echo "[worker] token required: set COORD_ADMIN_TOKEN, COORD_TOKEN, or HACKME_COORDINATOR_ADMIN_TOKEN, or create .secrets/hackme_coordinator_admin_token (VPS coordinator admin, one line), or ADMIN_TOKEN for same-token dev stacks" >&2
  exit 1
fi
if [[ "$TOKEN" == *"..."* || "$TOKEN" == *"ТУТ_ПОЛНЫЙ_ТОКЕН"* || "$TOKEN" == *"CHANGE_ME"* ]]; then
  echo "[worker] token looks like placeholder; set real secret token" >&2
  exit 1
fi

if [[ "$SIGN_SUBMITS" == "1" ]]; then
  if [[ -z "${HACKME_MINER_ED25519_SEED_HEX:-}" ]]; then
    echo "[worker] HACKME_WORKER_SIGN_SUBMITS=1 requires HACKME_MINER_ED25519_SEED_HEX (64 hex chars = 32-byte seed). Generate: go run ./cmd/minersign -gen-seed" >&2
    exit 1
  fi
  if [[ -z "$MINERSIGN_BIN" ]]; then
    if [[ -x "${ROOT_DIR}/minersign" ]]; then
      MINERSIGN_BIN="${ROOT_DIR}/minersign"
    elif command -v minersign >/dev/null 2>&1; then
      MINERSIGN_BIN="$(command -v minersign)"
    else
      echo "[worker] minersign binary not found; build: (cd ${ROOT_DIR} && go build -o minersign ./cmd/minersign) or set MINERSIGN_BIN=" >&2
      exit 1
    fi
  fi
fi

# Best-effort: if WORKER_NAME was not explicitly set, try to embed GPU model
# so public pool dashboards show a meaningful rig label (e.g. RX 580).
if [[ "$WORKER_NAME" == "$WORKER_ID" ]]; then
  if command -v lspci >/dev/null 2>&1; then
    # Take first VGA/3D controller line and extract device model text.
    gpu_line="$(lspci 2>/dev/null | grep -Ei 'VGA compatible controller|3D controller' | head -n 1 || true)"
    if [[ -n "$gpu_line" ]]; then
      gpu_model="$(printf '%s' "$gpu_line" | sed -E 's/^[0-9a-fA-F:.]+[[:space:]]+[^:]+:[[:space:]]+//')"
      gpu_model="$(printf '%s' "$gpu_model" | sed -E 's/\\[[^\\]]+\\]//g' | sed -E 's/[[:space:]]+/ /g' | sed -E 's/[[:space:]]+$//')"
      if [[ -n "$gpu_model" ]]; then
        WORKER_NAME="$gpu_model"
      fi
    fi
  fi
fi

backoff_sec=1
ok_claims=0
ok_submits=0
accepted_hits=0
rejected=0
last_reason=""
ema_hashrate_ghs="$HASHRATE_GHS"

api_post() {
  local path="$1"
  local body="$2"
  curl -sS --connect-timeout 15 --max-time "${HACKME_WORKER_HTTP_MAX_SEC:-120}" \
    -X POST "${COORD_URL}${path}" \
    -H "Content-Type: application/json" \
    -H "X-Hackme-Admin-Token: ${TOKEN}" \
    -d "$body"
}

sleep_backoff() {
  sleep "$backoff_sec"
  if (( backoff_sec < 20 )); then
    backoff_sec=$((backoff_sec * 2))
    if (( backoff_sec > 20 )); then
      backoff_sec=20
    fi
  fi
}

reset_backoff() {
  backoff_sec=1
}

push_work_snapshot() {
  local gh="$1"
  local accepted="$2"
  local shares="$3"
  if [[ "${COORD_PUSH_WORK}" != "1" ]]; then
    return 0
  fi
  local body
  body="{\"worker_id\":\"${WORKER_ID}\",\"name\":\"${WORKER_NAME}\",\"hashrate_gh_s\":${gh},\"share_accepted\":${accepted},\"shares_accepted\":${shares}}"
  api_post "/api/push_work" "$body" >/dev/null 2>&1 || true
}

echo "[worker] start id=${WORKER_ID} coord=${COORD_URL} batch=${BATCH_SIZE}"
if [[ "$SIGN_SUBMITS" == "1" ]]; then
  echo "[worker] hybrid signer: submits will carry miner_pubkey_ed25519 + miner_sig_ed25519 + submit_nonce (nonce file=${NONCE_FILE})"
fi
echo "[worker] note: HASHRATE_GHS env seeds EMA only; sustained GH/s ≈ batch_size / full claim+submit seconds (often ~0.002–0.02 GH/s), not GPU saturation."

while true; do
  # Full claim→submit wall time matches credited batch throughput (pool GH/s / global TH/s).
  t_cycle_start="$(date +%s%N)"
  claim="$(api_post "/api/work/claim" "{\"worker_id\":\"${WORKER_ID}\",\"batch_size\":${BATCH_SIZE}}" 2>/dev/null || true)"
  ok="$(printf '%s' "$claim" | jq -r '.ok // false' 2>/dev/null || echo "false")"
  if [[ "$ok" != "true" ]]; then
    reason="$(printf '%s' "$claim" | jq -r '.reason // "claim_failed"' 2>/dev/null || echo "claim_failed")"
    last_reason="$reason"
    echo "[worker] claim fail reason=${reason} backoff=${backoff_sec}s"
    sleep_backoff
    continue
  fi

	base="$(printf '%s' "$claim" | jq -r '.base_nonce // empty' 2>/dev/null || echo "")"
	size="$(printf '%s' "$claim" | jq -r '.batch_size // empty' 2>/dev/null || echo "")"
	work_id="$(printf '%s' "$claim" | jq -r '.work_id // empty' 2>/dev/null || echo "")"
  if [[ -z "$base" || -z "$size" || -z "$work_id" ]]; then
    last_reason="claim_parse_error"
    echo "[worker] claim parse error backoff=${backoff_sec}s"
    sleep_backoff
    continue
  fi
  ok_claims=$((ok_claims + 1))
  reset_backoff

  if [[ "$SIGN_SUBMITS" == "1" ]]; then
		partial="$(jq -nc \
		  --arg wid "$WORKER_ID" \
		  --argjson base "${base}" \
		  --argjson size "${size}" \
		  --arg wid_work "${work_id}" \
		  --argjson attempts "${size}" \
		  --argjson found false \
		  --argjson found_nonce 0 \
		  --arg rh "" \
		  --arg ph "" \
		  '{worker_id:$wid,base_nonce:$base,batch_size:$size,work_id:$wid_work,attempts:$attempts,found:$found,found_nonce:$found_nonce,result_hash:$rh,proof_hash:$ph}' 2>/dev/null)" || {
			echo "[worker] jq partial build failed backoff=${backoff_sec}s"
			sleep_backoff
			continue
		}
		extras="$(printf '%s\n' "$partial" | env HACKME_MINER_ED25519_SEED_HEX="$HACKME_MINER_ED25519_SEED_HEX" "$MINERSIGN_BIN" --nonce-file "$NONCE_FILE")" || {
			echo "[worker] minersign failed backoff=${backoff_sec}s"
			sleep_backoff
			continue
		}
		submit_body="$(jq -n --argjson p "$partial" --argjson x "$extras" --argjson gh "$ema_hashrate_ghs" '$p + $x + {hashrate_gh_s:$gh}' 2>/dev/null)" || {
			echo "[worker] jq submit merge failed (check minersign JSON) backoff=${backoff_sec}s"
			sleep_backoff
			continue
		}
  else
    submit_body="$(jq -n \
      --arg wid "$WORKER_ID" \
      --arg work_id "$work_id" \
      --argjson base "$base" \
      --argjson size "$size" \
      --argjson gh "$ema_hashrate_ghs" \
      '{worker_id:$wid, base_nonce:$base, batch_size:$size, work_id:$work_id, attempts:$size, found:false, hashrate_gh_s:$gh}' 2>/dev/null)" || {
      echo "[worker] jq unsigned submit failed backoff=${backoff_sec}s"
      sleep_backoff
      continue
    }
  fi
  submit="$(api_post "/api/work/submit" "$submit_body" 2>/dev/null || true)"
  submit_ok="$(printf '%s' "$submit" | jq -r '.ok // false' 2>/dev/null || echo "false")"
  accepted="$(printf '%s' "$submit" | jq -r '.accepted // false' 2>/dev/null || echo "false")"
  reason="$(printf '%s' "$submit" | jq -r '.reason // ""' 2>/dev/null || echo "")"
  payout="$(printf '%s' "$submit" | jq -r '.payout_hmc // 0' 2>/dev/null || echo "0")"

  if [[ "$submit_ok" == "true" || "$accepted" == "true" ]]; then
    t_cycle_end="$(date +%s%N)"
    elapsed_ns=$((t_cycle_end - t_cycle_start))
    if (( elapsed_ns < 1 )); then
      elapsed_ns=1
    fi
    inst_hashrate="$(awk -v attempts="${size}" -v ns="${elapsed_ns}" 'BEGIN{v=(attempts*1.0)/(ns*1.0); if(v<0)v=0; print v}')"
    # Exponential moving average smooths jitter over successful claim→submit rounds only.
    ema_hashrate_ghs="$(awk -v prev="${ema_hashrate_ghs}" -v inst="${inst_hashrate}" -v a="${EMA_ALPHA}" -v mn="${MIN_HASHRATE_GHS}" -v mx="${MAX_HASHRATE_GHS}" 'BEGIN{
      if (a < 0.01) a = 0.01;
      if (a > 1.0) a = 1.0;
      v = prev*(1.0-a) + inst*a;
      if (v < mn) v = mn;
      if (v > mx) v = mx;
      print v
    }')"
    ok_submits=$((ok_submits + 1))
    if [[ "$accepted" == "true" ]]; then
      accepted_hits=$((accepted_hits + 1))
    fi
    push_work_snapshot "${ema_hashrate_ghs}" "true" "${ok_submits}"
    last_reason=""
    reset_backoff
    echo "[worker] submit ok claims=${ok_claims} submits=${ok_submits} accepted=${accepted_hits} payout=${payout} hashrate_gh_s=${ema_hashrate_ghs}"
    continue
  fi

  rejected=$((rejected + 1))
  push_work_snapshot "${ema_hashrate_ghs}" "false" "${ok_submits}"
  if [[ -z "$reason" ]]; then
    reason="submit_failed"
  fi
  last_reason="$reason"
  # Treat transient coordinator protections as recoverable.
  case "$reason" in
    claim_rate_limited|submit_rate_limited|worker_temporarily_banned|lease_expired|unknown_or_already_closed_range)
      echo "[worker] submit transient reason=${reason} rejected=${rejected} backoff=${backoff_sec}s"
      sleep_backoff
      ;;
    *)
      echo "[worker] submit rejected reason=${reason} rejected=${rejected} backoff=${backoff_sec}s"
      sleep_backoff
      ;;
  esac
done

