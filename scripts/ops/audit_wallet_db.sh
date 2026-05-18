#!/usr/bin/env bash
# Offline audit: print wallet row + richest accounts + sample miners from blocks JSON (sqlite).
# Run on VPS or PC against ANY hackme.db (live or extracted from backups/*.tar.gz).
#
# Usage:
#   bash scripts/ops/audit_wallet_db.sh [path/to/hackme.db]
#   DB_PATH=/srv/hackme/data/hackme.db bash scripts/ops/audit_wallet_db.sh

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DB_PATH="${1:-${DB_PATH:-$ROOT_DIR/data/hackme.db}}"

if ! command -v sqlite3 >/dev/null 2>&1; then
  echo "install sqlite3 (e.g. apt install sqlite3)" >&2
  exit 1
fi
if [[ ! -f "$DB_PATH" ]]; then
  echo "DB not found: $DB_PATH" >&2
  exit 1
fi

echo "=== file ==="
echo "$DB_PATH"
ls -la "$DB_PATH"
echo

echo "=== wallet (UI primary row id=1) ==="
sqlite3 -header -column "$DB_PATH" "SELECT id, address, balance_units, balance_hmc FROM wallet;" || true
echo

echo "=== accounts TOP 25 by balance_units ==="
sqlite3 -header -column "$DB_PATH" \
  "SELECT address, balance_units, next_nonce FROM accounts ORDER BY balance_units DESC LIMIT 25;" || true
echo

echo "=== recent blocks: index + miner_address (from JSON) ==="
sqlite3 -header -column "$DB_PATH" \
  "SELECT block_index, json_extract(json, '\$.miner_address') AS miner FROM blocks ORDER BY block_index DESC LIMIT 15;" 2>/dev/null \
  || sqlite3 "$DB_PATH" "SELECT block_index, substr(json,1,120) FROM blocks ORDER BY block_index DESC LIMIT 5;"
echo

echo "=== alignment check (wallet vs tip miner on disk) ==="
WADDR="$(sqlite3 "$DB_PATH" "SELECT trim(address) FROM wallet WHERE id=1;" 2>/dev/null || true)"
TIP_MIN="$(sqlite3 "$DB_PATH" "SELECT json_extract(json,'$.miner_address') FROM blocks ORDER BY block_index DESC LIMIT 1;" 2>/dev/null || true)"
echo "wallet.address (id=1): ${WADDR:-<none>}"
echo "tip block miner_address: ${TIP_MIN:-<none>}"
if [[ -n "$WADDR" && -n "$TIP_MIN" && "$WADDR" != "$TIP_MIN" ]]; then
  echo "NOTE: mismatch is OK after signer change IF rebalance moved funds; UI balance uses accounts[wallet.address]."
  echo "      PoH credits (current code) go to accounts[wallet.address], block header still stores miner at solve time."
fi
echo

echo "=== hint ==="
echo "/api/wallet shows accounts[wallet.address] (primary wallet row)."
echo "transfer fees: burn vs DevFeeAddress — see /api/status economics."
echo "Stranded balance: accounts row with large balance_units whose address != wallet.address — investigate backups/rebind history."
