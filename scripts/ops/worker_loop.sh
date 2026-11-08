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
# Optional: pin reported hashrate_gh_s (test/display fleets). Skips EMA drift from wall-time.
# Under Blake3 (poh_formula_version>=3) FORCE is ignored — algebraic GH/s is not real PoW throughput.
FORCE_HASHRATE_GHS="${FORCE_HASHRATE_GHS:-}"
FORCE_HASHRATE_GHS_REQUESTED="$FORCE_HASHRATE_GHS"
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
if [[ "$TOKEN" == *"..."* || "$TOKEN" == *"PUT_FULL_TOKEN_HERE"* || "$TOKEN" == *"CHANGE_ME"* ]]; then
  echo "[worker] token looks like placeholder; set real secret token" >&2
  exit 1
fi

MINER_PUBKEY_HEX=""
if [[ "$SIGN_SUBMITS" == "1" ]]; then
  if [[ -z "${HACKME_MINER_ED25519_SEED_HEX:-}" ]]; then
    echo "[worker] HACKME_WORKER_SIGN_SUBMITS=1 requires HACKME_MINER_ED25519_SEED_HEX (64 hex chars = 32-byte seed). Generate: go run ./cmd/minersign -gen-seed" >&2
    exit 1
  fi
  if [[ -z "$MINERSIGN_BIN" ]]; then
    if [[ -x "${ROOT_DIR}/bin/minersign" ]]; then
      MINERSIGN_BIN="${ROOT_DIR}/bin/minersign"
    elif [[ -x "${ROOT_DIR}/minersign" ]]; then
      MINERSIGN_BIN="${ROOT_DIR}/minersign"
    elif command -v minersign >/dev/null 2>&1; then
      MINERSIGN_BIN="$(command -v minersign)"
    else
      echo "[worker] minersign binary not found; build: (cd ${ROOT_DIR} && go build -o bin/minersign ./cmd/minersign) or set MINERSIGN_BIN=" >&2
      exit 1
    fi
  fi
  # Coordinator hybrid claims require miner_pubkey (claim_pubkey_required).
  MINER_PUBKEY_HEX="$(
    jq -nc '{worker_id:"pubkey-probe",base_nonce:0,batch_size:1,work_id:"pubkey-probe",attempts:1,found:false,found_nonce:0,result_hash:"",proof_hash:""}' \
      | env HACKME_MINER_ED25519_SEED_HEX="$HACKME_MINER_ED25519_SEED_HEX" "$MINERSIGN_BIN" --nonce-file "${NONCE_FILE}.pubkey-probe" \
      | jq -r '.miner_pubkey_ed25519 // empty'
  )"
  if [[ ${#MINER_PUBKEY_HEX} -ne 64 ]]; then
    echo "[worker] failed to derive miner_pubkey_ed25519 from seed (got len=${#MINER_PUBKEY_HEX})" >&2
    exit 1
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
# Optional pace between successful claim→submit rounds (ms). Named hybrid fleets
# share one per-worker claim bucket with dig/hunt — without this, PoH burns the cap.
CLAIM_COOLDOWN_MS="${HACKME_WORKER_CLAIM_COOLDOWN_MS:-0}"
CLAIM_COOLDOWN_JITTER_MS="${HACKME_WORKER_CLAIM_COOLDOWN_JITTER_MS:-0}"
BATCH_JITTER_PCT="${HACKME_WORKER_BATCH_JITTER_PCT:-0}"
GH_JITTER_PCT="${HACKME_WORKER_GH_JITTER_PCT:-0}"
BATCH_SIZE_BASE="$BATCH_SIZE"
ok_claims=0
ok_submits=0
accepted_hits=0
rejected=0
last_reason=""
ema_hashrate_ghs="$HASHRATE_GHS"
if [[ -n "$FORCE_HASHRATE_GHS" ]]; then
  ema_hashrate_ghs="$FORCE_HASHRATE_GHS"
fi

live_jitter_int() {
  local max="${1:-0}"
  if ! [[ "$max" =~ ^[0-9]+$ ]] || (( max <= 0 )); then
    echo 0
    return
  fi
  echo $(( RANDOM % (max + 1) ))
}

pick_live_batch() {
  local base="$BATCH_SIZE_BASE"
  local pct="${BATCH_JITTER_PCT:-0}"
  # Cap at coordinator Blake3 max_claim_batch (64M) so wall GH can reach mid-class GPUs.
  local hi_cap="${HACKME_WORKER_BATCH_MAX:-67108864}"
  if ! [[ "$pct" =~ ^[0-9]+$ ]] || (( pct <= 0 )); then
    echo "$base"
    return
  fi
  python3 -c "import random; b=int('${base}'); p=int('${pct}'); hi_cap=int('${hi_cap}');
lo=max(262144, int(b*(100-p)/100)); hi=min(hi_cap, int(b*(100+p)/100));
print(random.randint(lo, max(lo, hi)))"
}

pick_live_gh() {
  # Prefer sticky Blake3 target (FORCE cleared after fv≥3) so jitter still works.
  local base="${FORCE_HASHRATE_GHS:-${BLAKE3_TARGET_GHS:-$HASHRATE_GHS}}"
  local pct="${GH_JITTER_PCT:-0}"
  if [[ -z "$base" ]] || ! [[ "$pct" =~ ^[0-9]+$ ]] || (( pct <= 0 )); then
    echo "$base"
    return
  fi
  python3 -c "import random; b=float('${base}'); p=int('${pct}');
print(round(b*(1.0+random.uniform(-p,p)/100.0), 4))"
}

# Sleep so claim→submit wall ≈ batch/(target_ghs·1e9). Cosmetic fleets stay Blake3-honest
# without advertising pre-cliff FORCE numbers (coord clamps those anyway).
pace_blake3_wall() {
  local batch="$1"
  local target="$2"
  local start_ns="$3"
  python3 -c "
batch=float('${batch}'); target=float('${target}'); start_ns=int('${start_ns}')
import time
if batch<=0 or target<=0:
  raise SystemExit(0)
want=batch/(target*1e9)
if want<0.05: want=0.05
if want>45.0: want=45.0
elapsed=(time.time_ns()-start_ns)/1e9
remain=want-elapsed
if remain>0.01:
  time.sleep(remain)
"
}

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
  echo "[worker] hybrid signer: claims+submits carry miner_pubkey_ed25519; submits also miner_sig_ed25519 + submit_nonce (nonce file=${NONCE_FILE})"
fi
echo "[worker] note: HASHRATE_GHS env seeds EMA only; sustained GH/s ≈ batch_size / full claim+submit seconds (Blake3 class often ~0.02–0.4 GH/s/GPU)."
if [[ -n "$FORCE_HASHRATE_GHS_REQUESTED" ]]; then
  echo "[worker] FORCE_HASHRATE_GHS=${FORCE_HASHRATE_GHS_REQUESTED} → Blake3 wall-pace target when claim poh_formula_version>=3"
fi
force_ghs_blake3_logged=0
BLAKE3_TARGET_GHS="${BLAKE3_TARGET_GHS:-$FORCE_HASHRATE_GHS_REQUESTED}"
# Blake3 SHARE_STRICT fleets: claim only while holding a harvest CPU slot so the
# lease wall is real work (not flock queue) → higher share-implied GH/s + fewer expired leases.
EXPECT_SHARE_STRICT="${HACKME_EXPECT_SHARE_STRICT:-1}"
if [[ -z "${SHAREHARVEST_BIN:-}" ]]; then
  if [[ -x "${ROOT_DIR}/bin/shareharvest" ]]; then
    SHAREHARVEST_BIN="${ROOT_DIR}/bin/shareharvest"
  elif command -v shareharvest >/dev/null 2>&1; then
    SHAREHARVEST_BIN="$(command -v shareharvest)"
  fi
fi
HARVEST_LOCK_FD=""
acquire_harvest_slot() {
  local parallel="${HACKME_SHAREHARVEST_PARALLEL:-4}"
  local lock_dir="${HACKME_SHAREHARVEST_LOCK_DIR:-$ROOT_DIR/logs/test-named-fleet}"
  local wait_sec="${HACKME_SHAREHARVEST_LOCK_WAIT_SEC:-90}"
  local s f fd
  mkdir -p "$lock_dir"
  release_harvest_slot
  for ((s=0; s<parallel; s++)); do
    f="$lock_dir/shareharvest.${s}.lock"
    eval "exec {fd}<>\"$f\""
    if flock -n "$fd" 2>/dev/null; then
      HARVEST_LOCK_FD=$fd
      return 0
    fi
    eval "exec ${fd}>&-"
  done
  f="$lock_dir/shareharvest.0.lock"
  eval "exec {fd}<>\"$f\""
  if flock -w "$wait_sec" "$fd" 2>/dev/null; then
    HARVEST_LOCK_FD=$fd
    return 0
  fi
  eval "exec ${fd}>&-"
  return 1
}
release_harvest_slot() {
  if [[ -n "${HARVEST_LOCK_FD:-}" ]]; then
    eval "exec ${HARVEST_LOCK_FD}>&-" 2>/dev/null || true
    HARVEST_LOCK_FD=""
  fi
}

while true; do
  if [[ "$EXPECT_SHARE_STRICT" == "1" || "$EXPECT_SHARE_STRICT" == "true" ]]; then
    if ! acquire_harvest_slot; then
      echo "[worker] harvest slot busy — retry"
      sleep 2
      continue
    fi
  fi
  # Wall clock starts after CPU slot — shareImpliedGHS uses claim→submit elapsed.
  t_cycle_start="$(date +%s%N)"
  BATCH_SIZE="$(pick_live_batch)"
  if [[ -n "$FORCE_HASHRATE_GHS" || -n "$BLAKE3_TARGET_GHS" ]]; then
    ema_hashrate_ghs="$(pick_live_gh)"
  fi
  if [[ "$SIGN_SUBMITS" == "1" && -n "$MINER_PUBKEY_HEX" ]]; then
    claim_body="$(jq -nc \
      --arg wid "$WORKER_ID" \
      --argjson batch "$BATCH_SIZE" \
      --arg pub "$MINER_PUBKEY_HEX" \
      '{worker_id:$wid,batch_size:$batch,miner_pubkey:$pub,miner_pubkey_ed25519:$pub}')"
  else
    claim_body="$(jq -nc --arg wid "$WORKER_ID" --argjson batch "$BATCH_SIZE" '{worker_id:$wid,batch_size:$batch}')"
  fi
  claim="$(api_post "/api/work/claim" "$claim_body" 2>/dev/null || true)"
  ok="$(printf '%s' "$claim" | jq -r '.ok // false' 2>/dev/null || echo "false")"
  if [[ "$ok" != "true" ]]; then
    reason="$(printf '%s' "$claim" | jq -r '.reason // "claim_failed"' 2>/dev/null || echo "claim_failed")"
    last_reason="$reason"
    echo "[worker] claim fail reason=${reason} backoff=${backoff_sec}s"
    release_harvest_slot
    sleep_backoff
    continue
  fi

	base="$(printf '%s' "$claim" | jq -r '.base_nonce // empty' 2>/dev/null || echo "")"
	size="$(printf '%s' "$claim" | jq -r '.batch_size // empty' 2>/dev/null || echo "")"
	work_id="$(printf '%s' "$claim" | jq -r '.work_id // empty' 2>/dev/null || echo "")"
  claim_fv="$(printf '%s' "$claim" | jq -r '.poh_formula_version // .formula_version // 0' 2>/dev/null || echo "0")"
  share_mod="$(printf '%s' "$claim" | jq -r '.share_mod // 0' 2>/dev/null || echo "0")"
  share_strict="$(printf '%s' "$claim" | jq -r '.share_strict // false' 2>/dev/null || echo "false")"
  claim_prev="$(printf '%s' "$claim" | jq -r '.prev_hash // empty' 2>/dev/null || echo "")"
  claim_addr="$(printf '%s' "$claim" | jq -r '.miner_address // empty' 2>/dev/null || echo "")"
  if [[ -z "$base" || -z "$size" || -z "$work_id" ]]; then
    last_reason="claim_parse_error"
    echo "[worker] claim parse error backoff=${backoff_sec}s"
    release_harvest_slot
    sleep_backoff
    continue
  fi
  # SHARE_STRICT: verified share_nonces. More shares / shorter wall ⇒ higher shareImpliedGHS.
  share_nonces_json='[]'
  share_count=0
  if [[ "$share_strict" == "true" || "$share_strict" == "1" ]] && [[ "$share_mod" =~ ^[1-9][0-9]*$ ]]; then
    if [[ -z "${SHAREHARVEST_BIN:-}" ]]; then
      if [[ "${SHAREHARVEST_WARNED:-0}" == "0" ]]; then
        echo "[worker] WARN: share_strict but shareharvest missing (go build -o bin/shareharvest ./cmd/shareharvest)" >&2
        SHAREHARVEST_WARNED=1
      fi
      release_harvest_slot
      sleep_backoff
      continue
    fi
    share_max="${HACKME_SHAREHARVEST_MAX:-2}"
    share_workers="${HACKME_SHAREHARVEST_WORKERS:-4}"
    sh_out="$("$SHAREHARVEST_BIN" -base "$base" -batch "$size" -mod "$share_mod" \
      -fv "${claim_fv:-3}" -prev "$claim_prev" -addr "$claim_addr" \
      -max "$share_max" -workers "$share_workers" 2>/dev/null || true)"
    share_nonces_json="$(printf '%s' "$sh_out" | jq -c '.share_nonces // []' 2>/dev/null || echo '[]')"
    share_count="$(printf '%s' "$share_nonces_json" | jq 'length' 2>/dev/null || echo 0)"
    if [[ "${share_count:-0}" == "0" ]]; then
      echo "[worker] shareharvest empty mod=${share_mod} scanned=$(printf '%s' "$sh_out" | jq -r '.scanned // 0') — retry"
      release_harvest_slot
      sleep_backoff
      continue
    fi
    if [[ "$force_ghs_blake3_logged" == "0" ]]; then
      echo "[worker] Blake3 fv=${claim_fv} SHARE_STRICT: harvested ${share_count} shares (slot held through claim)"
      force_ghs_blake3_logged=1
    fi
    # Keep a paced board target when shares proved work. Coordinator only honors
    # hashrate_gh_s in [0.1, 2] alongside valid shares (see pacedBoardGHS).
    if [[ -n "$FORCE_HASHRATE_GHS_REQUESTED" || -n "$BLAKE3_TARGET_GHS" ]]; then
      ema_hashrate_ghs="$(pick_live_gh)"
      if [[ -z "$ema_hashrate_ghs" || "$ema_hashrate_ghs" == "0" ]]; then
        ema_hashrate_ghs="${BLAKE3_TARGET_GHS:-$FORCE_HASHRATE_GHS_REQUESTED}"
      fi
    else
      ema_hashrate_ghs="$MIN_HASHRATE_GHS"
    fi
    FORCE_HASHRATE_GHS=""
  elif [[ "$claim_fv" =~ ^[0-9]+$ ]] && (( claim_fv >= 3 )); then
    # Soft Blake3 path: wall-pace to target (no shares required).
    FORCE_HASHRATE_GHS=""
    if [[ -z "$BLAKE3_TARGET_GHS" && -n "$FORCE_HASHRATE_GHS_REQUESTED" ]]; then
      BLAKE3_TARGET_GHS="$FORCE_HASHRATE_GHS_REQUESTED"
    fi
    if [[ -n "$FORCE_HASHRATE_GHS_REQUESTED" || -n "$BLAKE3_TARGET_GHS" ]]; then
      if [[ "$force_ghs_blake3_logged" == "0" ]]; then
        echo "[worker] Blake3 fv=${claim_fv}: wall-pace target_ghs=${BLAKE3_TARGET_GHS:-$MIN_HASHRATE_GHS}"
        force_ghs_blake3_logged=1
      fi
      target_ghs="$(pick_live_gh)"
      if [[ -z "$target_ghs" || "$target_ghs" == "0" ]]; then
        target_ghs="$MIN_HASHRATE_GHS"
      fi
      pace_blake3_wall "$size" "$target_ghs" "$t_cycle_start"
      t_paced="$(date +%s%N)"
      ema_hashrate_ghs="$(awk -v attempts="${size}" -v ns="$((t_paced - t_cycle_start))" -v mn="${MIN_HASHRATE_GHS}" -v mx="${MAX_HASHRATE_GHS}" 'BEGIN{
        if (ns < 1) ns = 1;
        v = (attempts * 1.0) / ns;
        if (v < mn) v = mn;
        if (v > mx) v = mx;
        printf "%.6f", v
      }')"
    fi
  fi
  ok_claims=$((ok_claims + 1))
  reset_backoff

  if [[ -n "$FORCE_HASHRATE_GHS" && ( -z "${GH_JITTER_PCT:-}" || "${GH_JITTER_PCT}" == "0" ) ]]; then
    ema_hashrate_ghs="$FORCE_HASHRATE_GHS"
  fi

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
			release_harvest_slot
			sleep_backoff
			continue
		}
		extras="$(printf '%s\n' "$partial" | env HACKME_MINER_ED25519_SEED_HEX="$HACKME_MINER_ED25519_SEED_HEX" "$MINERSIGN_BIN" --nonce-file "$NONCE_FILE")" || {
			echo "[worker] minersign failed backoff=${backoff_sec}s"
			release_harvest_slot
			sleep_backoff
			continue
		}
		submit_body="$(jq -n --argjson p "$partial" --argjson x "$extras" --argjson gh "$ema_hashrate_ghs" --argjson shares "$share_nonces_json" \
		  '$p + $x + {hashrate_gh_s:$gh, share_nonces:$shares}' 2>/dev/null)" || {
			echo "[worker] jq submit merge failed (check minersign JSON) backoff=${backoff_sec}s"
			release_harvest_slot
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
      --argjson shares "$share_nonces_json" \
      '{worker_id:$wid, base_nonce:$base, batch_size:$size, work_id:$work_id, attempts:$size, found:false, hashrate_gh_s:$gh, share_nonces:$shares}' 2>/dev/null)" || {
      echo "[worker] jq unsigned submit failed backoff=${backoff_sec}s"
      release_harvest_slot
      sleep_backoff
      continue
    }
  fi
  submit="$(api_post "/api/work/submit" "$submit_body" 2>/dev/null || true)"
  # Release CPU slot before cooldown sleep so other units can harvest.
  release_harvest_slot
  submit_ok="$(printf '%s' "$submit" | jq -r '.ok // false' 2>/dev/null || echo "false")"
  accepted="$(printf '%s' "$submit" | jq -r '.accepted // false' 2>/dev/null || echo "false")"
  reason="$(printf '%s' "$submit" | jq -r '.reason // ""' 2>/dev/null || echo "")"
  payout="$(printf '%s' "$submit" | jq -r '.payout_hmc // 0' 2>/dev/null || echo "0")"
  resp_ghs="$(printf '%s' "$submit" | jq -r '.hashrate_gh_s // empty' 2>/dev/null || echo "")"

  if [[ "$submit_ok" == "true" || "$accepted" == "true" ]]; then
    if [[ -n "$resp_ghs" && "$resp_ghs" != "null" ]]; then
      ema_hashrate_ghs="$resp_ghs"
    elif [[ -n "$FORCE_HASHRATE_GHS" ]]; then
      if [[ -z "${GH_JITTER_PCT:-}" || "${GH_JITTER_PCT}" == "0" ]]; then
        ema_hashrate_ghs="$FORCE_HASHRATE_GHS"
      fi
    else
      t_cycle_end="$(date +%s%N)"
      elapsed_ns=$((t_cycle_end - t_cycle_start))
      if (( elapsed_ns < 1 )); then
        elapsed_ns=1
      fi
      # Prefer share-implied style estimate when we have shares: shares*mod/wall.
      if [[ "${share_count:-0}" -gt 0 && "$share_mod" =~ ^[1-9][0-9]*$ ]]; then
        inst_hashrate="$(awk -v ns="${elapsed_ns}" -v sh="${share_count}" -v mod="${share_mod}" 'BEGIN{
          sec=ns/1e9; if(sec<0.5)sec=0.5;
          v=(sh*mod)/(sec*1e9); if(v<0)v=0; print v
        }')"
      else
        inst_hashrate="$(awk -v attempts="${size}" -v ns="${elapsed_ns}" 'BEGIN{v=(attempts*1.0)/(ns*1.0); if(v<0)v=0; print v}')"
      fi
      ema_hashrate_ghs="$(awk -v prev="${ema_hashrate_ghs}" -v inst="${inst_hashrate}" -v a="${EMA_ALPHA}" -v mn="${MIN_HASHRATE_GHS}" -v mx="${MAX_HASHRATE_GHS}" 'BEGIN{
        if (a < 0.01) a = 0.01;
        if (a > 1.0) a = 1.0;
        v = prev*(1.0-a) + inst*a;
        if (v < mn) v = mn;
        if (v > mx) v = mx;
        print v
      }')"
    fi
    ok_submits=$((ok_submits + 1))
    if [[ "$accepted" == "true" ]]; then
      accepted_hits=$((accepted_hits + 1))
    fi
    push_work_snapshot "${ema_hashrate_ghs}" "true" "${ok_submits}"
    last_reason=""
    reset_backoff
    echo "[worker] submit ok claims=${ok_claims} submits=${ok_submits} accepted=${accepted_hits} payout=${payout} shares=${share_count:-0} hashrate_gh_s=${ema_hashrate_ghs}"
    if [[ "${CLAIM_COOLDOWN_MS}" =~ ^[0-9]+$ ]] && (( CLAIM_COOLDOWN_MS > 0 )); then
      jitter_ms="$(live_jitter_int "${CLAIM_COOLDOWN_JITTER_MS:-0}")"
      sleep "$(awk -v ms="${CLAIM_COOLDOWN_MS}" -v j="${jitter_ms}" 'BEGIN{printf "%.3f", (ms+j)/1000.0}')"
    fi
    continue
  fi

  rejected=$((rejected + 1))
  push_work_snapshot "${ema_hashrate_ghs}" "false" "${ok_submits}"
  if [[ -z "$reason" ]]; then
    reason="submit_failed"
  fi
  last_reason="$reason"
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

