#!/usr/bin/env bash
# Audit pool economics: target_mod drift, reward/M, payout vs attempts model.
#
#   COORD_URL=https://hackme.tech/pool/coordinator bash scripts/ops/pool_fairness_audit.sh
#   SAMPLES=3 INTERVAL_SEC=20 bash scripts/ops/pool_fairness_audit.sh
set -euo pipefail

require_cmd() { command -v "$1" >/dev/null || { echo "[fairness] missing: $1" >&2; exit 1; }; }
require_cmd curl
require_cmd jq

COORD_URL="${COORD_URL:-https://hackme.tech/pool/coordinator}"
SAMPLES="${SAMPLES:-3}"
INTERVAL_SEC="${INTERVAL_SEC:-20}"

snap() {
  curl -fsS --max-time 20 "${COORD_URL%/}/api/work/stats"
}

echo "[fairness] coordinator=$COORD_URL samples=$SAMPLES interval=${INTERVAL_SEC}s"
prev_m=0
for i in $(seq 1 "$SAMPLES"); do
  j="$(snap)"
  m="$(jq -r '.target_mod // 0' <<<"$j")"
  rpm="$(jq -r '.reward_per_m // 0' <<<"$j")"
  ghs="$(jq -r '.pool_hashrate_gh_s // 0' <<<"$j")"
  bonus="$(jq -r '.found_bonus_hmc // 0.01' <<<"$j")"
  hint="$(jq -r '.target_mod_load_hint // 0' <<<"$j")"
  dm=0
  if [[ "$prev_m" -gt 0 && "$m" -gt 0 ]]; then
    dm=$((m - prev_m))
  fi
  prev_m="$m"
  echo ""
  echo "=== sample $i === M=$(printf '%''d' "$m") (+${dm}) reward/M=$rpm pool_GH=$ghs hint=$hint"
  jq -r --argjson rpm "$rpm" --argjson bonus "$bonus" '
    .workers | to_entries[] |
    "  \(.key): GH=\(.value.hashrate_gh_s // 0) att=\(.value.accepted_attempts // 0) hits=\(.value.accepted_hits // 0) pay=\(.value.payout_hmc // 0) model=\((.value.accepted_attempts // 0) / 1000000 * $rpm + (.value.accepted_hits // 0) * $bonus)"
  ' <<<"$j"
  [[ "$i" -lt "$SAMPLES" ]] && sleep "$INTERVAL_SEC"
done
echo ""
echo "[fairness] done — payout % in dashboard is cumulative; hash % is instantaneous fleet share."
