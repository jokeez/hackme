#!/usr/bin/env bash
# v2_height_planner.sh — pick the Economics v2 activation height for a target UTC time.
#
#   bash scripts/ops/v2_height_planner.sh "2026-11-26 11:00"
#
# ASERT holds 30 s/block; its offset from the absolute schedule t_anchor + 30·(H − 495 399) IS the
# difficulty (M = M_anchor·2^(offset/1h)), so with steady hashrate the offset stays put and a
# block lands at now + 30·(H − tip). Error budget: ±1 h of arrival per 2× hashrate change before
# the height (the offset moves 1 h per doubling). Prints the height,
# its expected time, the lead in blocks (must be ≥ HACKME_ECONOMICS_V2_MIN_LEAD_BLOCKS, default
# 40 320 ≈ 14 days) and the env lines for the hub.
set -euo pipefail
BASE="${CHAIN_BASE:-https://hackme.tech/pool}"
TARGET="${1:?usage: $0 \"YYYY-MM-DD HH:MM\" (UTC)}"
MIN_LEAD="${HACKME_ECONOMICS_V2_MIN_LEAD_BLOCKS:-40320}"
command -v jq >/dev/null || { echo "jq required" >&2; exit 2; }

target_unix="$(date -u -d "$TARGET" +%s)"
now_unix="$(date -u +%s)"
m="$(curl -fsS --max-time 15 "$BASE/api/metrics")"
tip="$(jq -r '.tip_height' <<<"$m")"
obs="$(jq -r '.mining_observed_block_sec // 30' <<<"$m")"
[[ "$tip" =~ ^[0-9]+$ ]] || { echo "bad tip from $BASE/api/metrics" >&2; exit 1; }
(( target_unix > now_unix )) || { echo "target is in the past" >&2; exit 1; }

ANCHOR_IDX=495399
anchor_ts="$(curl -fsS --max-time 15 "$BASE/api/reports/blocks?q=$ANCHOR_IDX" | jq -r '.blocks[0].timestamp_unix')"
[[ "$anchor_ts" =~ ^[0-9]+$ ]] || { echo "cannot read anchor block $ANCHOR_IDX timestamp" >&2; exit 1; }
height=$(( tip + (target_unix - now_unix) / 30 ))
lead=$(( height - tip - 1 ))
eta="$(date -u -d "@$(( now_unix + (height - tip) * 30 ))" '+%Y-%m-%d %H:%M UTC (%a)')"
sched_tip=$(( ANCHOR_IDX + (now_unix - anchor_ts) / 30 ))
offset_min=$(( (tip - sched_tip) * 30 / 60 ))
dow="$(date -u -d "@$target_unix" +%u)"; hour="$(date -u -d "@$target_unix" +%H)"

echo "tip now:            $tip   (ASERT offset ${offset_min} min ahead of schedule = current difficulty; last-hour block ${obs}s)"
echo "activation height:  $height"
echo "expected at:        $eta   (±1 h per 2× hashrate change; recheck the day before)"
echo "lead:               $lead blocks (minimum $MIN_LEAD)"
(( lead >= MIN_LEAD )) || echo "WARNING: lead below minimum — producer guard will refuse to lock"
(( dow >= 2 && dow <= 4 )) || echo "NOTE: not Tue–Thu"
(( 10#$hour >= 10 && 10#$hour < 14 )) || echo "NOTE: outside 10:00–14:00 UTC"
(( 10#$hour != 0 )) || echo "WARNING: 00:xx UTC collides with pool day reset / checkpoint"
echo
echo "hub env (announce day):"
echo "  HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT=$height"
echo "  HACKME_ECONOMICS_V2_ACTIVATION_CONFIRM=$height"
echo "  HACKME_ECONOMICS_V2_ANNOUNCE_UNIX=<unix time of the public announce>"
echo "release constants (internal/chain):"
echo "  DefaultEconomicsV2ActivationHeight = $height"
echo "  DefaultAuthorityPubKey             = <hub /api/status node key or block miner_pubkey_ed25519>"
