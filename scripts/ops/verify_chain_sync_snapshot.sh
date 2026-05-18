#!/usr/bin/env bash
# Chain consistency snapshot: local tip vs canonical vs pool target_mod.
# No admin required. Run from anywhere:
#   LOCAL_BASE=http://127.0.0.1:8080 bash scripts/ops/verify_chain_sync_snapshot.sh
#
# Checks:
#   — Standalone (network_mode_active=false): tip_height in /api/status and /api/metrics should match.
#   — Network mode: canonical_tip_height may exceed tip_height (SQLite lag) — expected without P2P.
#   — If status exposes pool_target_mod and global metrics expose work.target_mod — they should match (single coordinator source).
set -euo pipefail
BASE="${LOCAL_BASE:-http://127.0.0.1:8080}"
BASE="${BASE%/}"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[sync-check] missing: $1" >&2
    exit 2
  }
}
require_cmd curl
require_cmd jq

fail=0
warn=0

echo "[sync-check] base=$BASE"

st_json="$(curl -sS -m 8 "$BASE/api/status" || true)"
if [[ -z "$st_json" ]] || ! echo "$st_json" | jq -e . >/dev/null 2>&1; then
  echo "[sync-check] FAIL: /api/status unreachable or not JSON (is the node up?)"
  exit 1
fi

mt_json="$(curl -sS -m 8 "$BASE/api/metrics" || true)"
if [[ -z "$mt_json" ]] || ! echo "$mt_json" | jq -e . >/dev/null 2>&1; then
  echo "[sync-check] FAIL: /api/metrics unreachable or not JSON"
  exit 1
fi

gl_json="$(curl -sS -m 8 "$BASE/api/global/metrics" 2>/dev/null || true)"
if [[ -z "$gl_json" ]] || ! echo "$gl_json" | jq -e . >/dev/null 2>&1; then
  gl_json="{}"
  echo "[sync-check] note: /api/global/metrics skipped (unreachable or not JSON)"
fi

tip="$(echo "$st_json" | jq -r '.tip_height // 0 | tonumber')"
canon="$(echo "$st_json" | jq -r '.canonical_tip_height // 0 | tonumber')"
net="$(echo "$st_json" | jq -r 'if .network_mode_active == true then true else false end')"
genesis="$(echo "$st_json" | jq -r 'if .has_genesis == true then true else false end')"
m_bh="$(echo "$mt_json" | jq -r '.block_height // 0 | tonumber')"
m_tm="$(echo "$mt_json" | jq -r '.mining_target_mod // 0 | tonumber')"
pool_tm="$(echo "$st_json" | jq -r '.pool_target_mod // 0 | tonumber')"
pool_src="$(echo "$st_json" | jq -r '.pool_target_mod_source // "—"')"
g_tm="$(echo "$gl_json" | jq -r '.work.target_mod // 0 | tonumber')"
g_wok="$(echo "$gl_json" | jq -r '.work.ok // false')"

echo "[sync-check] has_genesis=$genesis network_mode=$net"
echo "[sync-check] status tip_height=$tip canonical_tip_height=$canon"
echo "[sync-check] metrics block_height=$m_bh mining_target_mod=$m_tm"
echo "[sync-check] status pool_target_mod=$pool_tm source=$pool_src"
echo "[sync-check] global work.target_mod=$g_tm work.ok=$g_wok"

if [[ "$genesis" == "true" ]]; then
  if [[ "$net" == "false" ]]; then
    if [[ "$tip" != "$m_bh" ]]; then
      echo "[sync-check] FAIL: standalone — tip_height ($tip) != metrics.block_height ($m_bh)"
      fail=1
    else
      echo "[sync-check] OK: local height status == metrics"
    fi
  else
    if [[ "$canon" -gt 0 ]] && [[ "$tip" -lt "$canon" ]]; then
      echo "[sync-check] INFO: local tip ($tip) < canonical ($canon) — expected without P2P / disk sync"
    fi
    if [[ "$canon" -gt 0 ]] && [[ "$tip" == "$canon" ]]; then
      echo "[sync-check] OK: local tip matches canonical"
    fi
  fi
fi

if [[ "$pool_tm" -gt 0 ]] && [[ "$g_tm" -gt 0 ]] && [[ "$pool_tm" != "$g_tm" ]]; then
  echo "[sync-check] WARN: pool_target_mod ($pool_tm) != global work.target_mod ($g_tm) — possible cache / request race"
  warn=1
fi

if [[ "$pool_tm" -gt 0 ]] && [[ "$m_tm" -gt 0 ]] && [[ "$pool_tm" != "$m_tm" ]] && [[ "$net" == "true" ]]; then
  echo "[sync-check] INFO: pool_target_mod ($pool_tm) vs mining_target_mod ($m_tm) — mismatch allowed briefly on followers with canonical overlay"
fi

if [[ "$fail" -ne 0 ]]; then
  exit 1
fi
if [[ "$warn" -ne 0 ]]; then
  exit 0
fi
echo "[sync-check] done"
exit 0
