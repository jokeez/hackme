#!/usr/bin/env bash
# MiningPoolStats + public launch probe for hackme.tech (run from operator machine).
#
#   PUBLIC_BASE=https://hackme.tech bash scripts/ops/mps_listing_readiness.sh
#   NODE_SSH=hackme-vps bash scripts/ops/mps_listing_readiness.sh --vps

set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PUBLIC_BASE="${PUBLIC_BASE:-https://hackme.tech}"
PUBLIC_BASE="${PUBLIC_BASE%/}"
VPS_CHECK=0
[[ "${1:-}" == "--vps" ]] && VPS_CHECK=1

pass=0
fail=0
warn=0

ok() { pass=$((pass + 1)); echo "[MPS-OK] $*"; }
bad() { fail=$((fail + 1)); echo "[MPS-FAIL] $*" >&2; }
note() { warn=$((warn + 1)); echo "[MPS-WARN] $*"; }

probe() {
  local name="$1" url="$2" expect="${3:-200}"
  local code body
  body="$(mktemp)"
  code="$(curl -sS -o "$body" -w '%{http_code}' --max-time 20 "$url" 2>/dev/null || echo 000)"
  if [[ "$code" == "$expect" ]] || [[ ",${expect}," == *",${code},"* ]]; then
    ok "$name HTTP $code $url"
    if [[ "$code" == "200" ]] && command -v jq >/dev/null 2>&1; then
      jq -e . "$body" >/dev/null 2>&1 || note "$name body is not JSON"
    fi
  else
    bad "$name HTTP $code (want $expect) $url"
  fi
  rm -f "$body"
}

echo "=== MiningPoolStats / public listing readiness ==="
echo "base=$PUBLIC_BASE"

probe "status" "$PUBLIC_BASE/api/status"
probe "global-metrics" "$PUBLIC_BASE/api/global/metrics"
probe "pool-stats-minimal" "$PUBLIC_BASE/pool/coordinator/api/pool/stats"
probe "pool-stats-via-node" "$PUBLIC_BASE/pool/api/work/stats"
probe "explorer" "$PUBLIC_BASE/pool/explorer" "200,301,302"
probe "downloads" "$PUBLIC_BASE/downloads.html"

# Minimal pool stats shape
tmp="$(mktemp)"
if curl -fsS --max-time 20 "$PUBLIC_BASE/pool/coordinator/api/pool/stats" -o "$tmp" 2>/dev/null; then
  if command -v jq >/dev/null 2>&1; then
    hr="$(jq -r '.hashrate // 0' "$tmp")"
    wc="$(jq -r '.workers // 0' "$tmp")"
    st="$(jq -r '.status // ""' "$tmp")"
    if [[ "$st" == "ok" ]]; then ok "pool/stats status=ok"; else bad "pool/stats status=$st"; fi
    if python3 - "$hr" <<'PY' 2>/dev/null; then
import sys
h=float(sys.argv[1])
sys.exit(0 if h>1e6 else 1)
PY
      ok "pool/stats hashrate=$hr"
    else
      note "pool/stats hashrate low or zero ($hr) — start workers before moderation"
    fi
    ok "pool/stats workers=$wc"
  fi
fi
rm -f "$tmp"

if [[ "$VPS_CHECK" == "1" ]]; then
  NODE_SSH="${NODE_SSH:-hackme-vps}"
  echo "=== VPS settlement (SSH $NODE_SSH) ==="
  if ssh -o BatchMode=yes -o ConnectTimeout=12 "$NODE_SSH" \
    'bash /opt/hackme/scripts/ops/settlement_healthcheck.sh' 2>&1; then
    ok "settlement_healthcheck on VPS"
  else
    bad "settlement_healthcheck on VPS"
  fi
  if ssh -o BatchMode=yes -o ConnectTimeout=12 "$NODE_SSH" \
    'systemctl is-active hackme-worker-settlement.timer hackme-coordinator hackme-node' 2>&1 | grep -q active; then
    ok "systemd units active"
  else
    bad "systemd units not all active"
  fi
fi

echo "=== Summary: pass=$pass fail=$fail warn=$warn ==="
exit $(( fail > 0 ? 1 : 0 ))
