#!/usr/bin/env bash
# Morning report: compare overnight baseline vs final snapshot.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
RUN_ID="${1:-night_20260520}"
OUT="$ROOT/reports/overnight/$RUN_ID"
SUMMARY="$OUT/summary.json"
BASELINE="$OUT/baseline.json"

if [[ ! -f "$SUMMARY" ]]; then
  echo "[morning] summary not ready yet: $SUMMARY" >&2
  echo "[morning] monitor still running? pid=$(cat "$OUT/monitor.pid" 2>/dev/null || echo —)" >&2
  if [[ -f "$OUT/snapshots.jsonl" ]]; then
    echo "[morning] snapshots so far: $(wc -l <"$OUT/snapshots.jsonl")" >&2
  fi
  exit 1
fi

echo "=== Morning mining report ($RUN_ID) ==="
jq -r '
  "baseline: \(.baseline.ts // "see baseline.json")",
  "final:    \(.final.ts)",
  "snapshots: \(.snapshots) every \(.interval_sec)s",
  "",
  "── Balances ──",
  "canonical HMC: \(.baseline.canonical_balance_hmc) → \(.final.canonical_balance_hmc) (Δ \(.delta.canonical_balance_hmc // 0))",
  "wallet API:    \(.baseline.wallet_balance_hmc) → \(.final.wallet_balance_hmc)",
  "unpaid accrual Δ: \(.delta.unpaid_accrual_hmc // 0)",
  "",
  "── Coordinator ──",
  "total payout HMC: \(.baseline.coord_total_payout_hmc) → \(.final.coord_total_payout_hmc) (Δ \(.delta.coord_total_payout_hmc // 0))",
  "ranges: \(.baseline.coord_submitted_items) → \(.final.coord_submitted_items) (Δ \(.delta.coord_submitted_items // 0))",
  "",
  "── Desktop GPU ──",
  "running: \(.baseline.desktop_worker_running) → \(.final.desktop_worker_running)",
  "GH/s: \(.baseline.desktop_measured_gh_s) → \(.final.desktop_measured_gh_s)",
  "",
  "── Per worker payout Δ ──"
' "$SUMMARY"

for w in worker-kapa-pc worker-vps-msk-01 vps-canary-01 worker-vps-62-01; do
  jq -r --arg w "$w" '
    "\($w): payout Δ \(.delta[($w + "_payout_hmc")] // 0) HMC · ranges Δ \(.delta[($w + "_ranges")] // 0) · attempts Δ \(.delta[($w + "_attempts")] // 0)"
  ' "$SUMMARY"
done

if [[ -f "$OUT/difficulty_report.json" ]]; then
  echo ""
  echo "── Difficulty (overnight) ──"
  jq -r '
    .target_mod as $m | .reward_per_m as $r |
    "target_mod M: \($m.first) → \($m.last) (min \($m.min) max \($m.max), retargets \($m.retarget_events))",
    "reward/M: \($r.first) → \($r.last) (min \($r.min) max \($r.max))"
  ' "$OUT/difficulty_report.json"
  echo "Details: cat $OUT/DIFFICULTY.md"
fi

echo ""
echo "Full delta: jq .delta $SUMMARY"
echo "Human baseline: cat $OUT/README.md"
