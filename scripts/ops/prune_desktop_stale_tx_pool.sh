#!/usr/bin/env bash
# Reject local-fork pending transfers whose nonce does not match canonical next_nonce.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DB="${HACKME_DATA_DIR:-$ROOT/data}/hackme.db"
WALLET="${WALLET:-HMC-91fe007e4036c602}"
CANON="${HACKME_CANONICAL_CHAIN_URL:-https://hackme.tech}"
NONCE="$(curl -fsS --max-time 15 "${CANON%/}/api/address/${WALLET}" | python3 -c "import sys,json; print(json.load(sys.stdin).get('next_nonce',0))")"
echo "[prune] canonical next_nonce=$NONCE for $WALLET"
sqlite3 "$DB" "UPDATE tx_pool SET status='rejected', reject_code='stale_local_fork' WHERE from_address='$WALLET' AND status='pending' AND nonce != $NONCE;"
echo "[prune] rejected rows: $(sqlite3 "$DB" "SELECT changes();")"
pending="$(sqlite3 "$DB" "SELECT COUNT(*) FROM tx_pool WHERE from_address='$WALLET' AND status='pending';")"
echo "[prune] pending left: $pending"
