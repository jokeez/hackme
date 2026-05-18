#!/usr/bin/env bash
# Read-only audit on canonical VPS over SSH (passwordless Host e.g. hackme-vps).
# Uses Python sqlite3 (CLI sqlite3 optional).
#
# Usage:
#   VPS_SSH=hackme-vps bash scripts/ops/vps_wallet_audit_remote.sh
# With HTTP:
#   VPS_SSH=hackme-vps NODE_BASE=http://IP:18080 bash scripts/ops/vps_wallet_audit_remote.sh

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VPS_SSH="${VPS_SSH:-hackme-vps}"
NODE_DEPLOY_DIR="${NODE_DEPLOY_DIR:-/opt/hackme}"
DB_REMOTE="${DB_REMOTE:-$NODE_DEPLOY_DIR/data/hackme.db}"
NODE_BASE="${NODE_BASE:-}"

require_cmd() { command -v "$1" >/dev/null 2>&1 || { echo "[vps-wallet-audit] missing: $1" >&2; exit 1; }; }
require_cmd ssh
require_cmd bash

echo "[vps-wallet-audit] ssh $VPS_SSH (BatchMode)"
ssh -o BatchMode=yes -o ConnectTimeout=15 "$VPS_SSH" "hostname; ls -la \"$DB_REMOTE\" 2>/dev/null || { echo 'DB missing — check NODE_DEPLOY_DIR/data/hackme.db'; exit 1; }"

echo "[vps-wallet-audit] DB snapshot (remote python3 sqlite)"
ssh -o BatchMode=yes "$VPS_SSH" "PYTHONWARNINGS=ignore python3 -" "$DB_REMOTE" <<'PY'
import sqlite3, sys

def dump(title: str, q: str, con: sqlite3.Connection) -> None:
    print(title)
    cur = con.execute(q)
    cols = [d[0] for d in cur.description]
    print(" | ".join(cols))
    for row in cur.fetchall():
        print(" | ".join(str(x) for x in row))
    print()

db = sys.argv[1]
con = sqlite3.connect(db)
dump("=== wallet ===", "SELECT id, address, balance_units, balance_hmc FROM wallet", con)
dump(
    "=== accounts TOP 15 ===",
    "SELECT address, balance_units, next_nonce FROM accounts ORDER BY balance_units DESC LIMIT 15",
    con,
)
w = (con.execute("SELECT trim(address) FROM wallet WHERE id=1").fetchone() or [""])[0]
m = (
    con.execute(
        "SELECT json_extract(json, '$.miner_address') FROM blocks ORDER BY block_index DESC LIMIT 1"
    ).fetchone()
    or [""]
)[0]
print("=== tip miner vs wallet.address ===")
print("wallet.address:", w or "?")
print("tip.miner_address:", m or "?")
print()
dump(
    "=== recent miners (15 blocks) ===",
    """SELECT block_index,
              json_extract(json, '$.miner_address') AS miner
       FROM blocks ORDER BY block_index DESC LIMIT 15""",
    con,
)
con.close()
PY

if [[ -n "$NODE_BASE" ]]; then
  require_cmd curl
  require_cmd jq
  echo "[vps-wallet-audit] HTTP $NODE_BASE/api/status (subset)"
  curl -fsS --max-time 15 "${NODE_BASE%/}/api/status" | jq '{tip_height, mining, node_address, tip_hash}' || echo "[vps-wallet-audit] status HTTP failed"
  echo "[vps-wallet-audit] HTTP $NODE_BASE/api/wallet"
  curl -fsS --max-time 15 "${NODE_BASE%/}/api/wallet" | jq '.' || echo "[vps-wallet-audit] wallet HTTP failed"
  TIP="$(curl -fsS --max-time 15 "${NODE_BASE%/}/api/status" | jq -r '.tip_height // empty')"
  if [[ -n "$TIP" && "$TIP" != "null" ]]; then
    echo "[vps-wallet-audit] HTTP $NODE_BASE/api/reports/block?index=$TIP"
    curl -fsS --max-time 15 "${NODE_BASE%/}/api/reports/block?index=${TIP}" | jq '{index, miner_address, task_id}' || true
  fi
fi

echo "[vps-wallet-audit] done"
