#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DB_PATH="${DB_PATH:-$ROOT_DIR/data/hackme.db}"
APPLY="${APPLY:-0}"              # 0 = dry-run, 1 = apply changes
TARGET_BALANCE_HMC="${TARGET_BALANCE_HMC:-}" # optional explicit wallet reset

if [[ ! -f "$DB_PATH" ]]; then
  echo "DB not found: $DB_PATH" >&2
  exit 1
fi

python3 - "$DB_PATH" "$APPLY" "$TARGET_BALANCE_HMC" <<'PY'
import sqlite3
import sys

db_path = sys.argv[1]
apply = sys.argv[2] == "1"
target_balance_raw = sys.argv[3].strip()

conn = sqlite3.connect(db_path)
conn.row_factory = sqlite3.Row
cur = conn.cursor()

patterns = [
    "stress-order-%",
    "order-valid-small-%",
    "order-insufficient-funds-%",
    "order-fairness-reject-%",
]

where_like = " OR ".join(["id LIKE ?"] * len(patterns))
params = patterns[:]

cur.execute(f"SELECT COUNT(*) AS c, COALESCE(SUM(prepaid_hmc),0) AS prepaid FROM tasks WHERE {where_like}", params)
row = cur.fetchone()
count = int(row["c"])
prepaid_sum = float(row["prepaid"])

cur.execute("SELECT address, balance_hmc FROM wallet WHERE id = 1")
w = cur.fetchone()
if w is None:
    print("Wallet row missing (id=1).")
    sys.exit(1)

addr = str(w["address"])
bal = float(w["balance_hmc"])

print(f"[INFO] test-like tasks matched: {count}")
print(f"[INFO] matched prepaid_hmc sum: {prepaid_sum:.6f}")
print(f"[INFO] current wallet: address={addr} balance_hmc={bal:.6f}")

if not apply:
    print("[DRY-RUN] No DB changes applied.")
    sys.exit(0)

cur.execute(f"DELETE FROM tasks WHERE {where_like}", params)
deleted = cur.rowcount if cur.rowcount is not None else 0
print(f"[APPLY] deleted tasks: {deleted}")

if target_balance_raw:
    target = float(target_balance_raw)
    if target < 0:
        raise ValueError("TARGET_BALANCE_HMC must be >= 0")
    cur.execute("UPDATE wallet SET balance_hmc = ?, balance_units = ? WHERE id = 1", (target, int(round(target * 100_000_000))))
    units = int(round(target * 100_000_000))
    cur.execute(
        """
        INSERT INTO accounts(address, balance_units, next_nonce, updated_at)
        VALUES(?, ?, 0, strftime('%s','now'))
        ON CONFLICT(address) DO UPDATE SET
          balance_units=excluded.balance_units,
          updated_at=excluded.updated_at
        """,
        (addr, units),
    )
    print(f"[APPLY] wallet reset to {target:.6f} HMC (accounts sync: {units} units)")
else:
    print("[APPLY] wallet unchanged (TARGET_BALANCE_HMC not set)")

conn.commit()
print("[DONE] DB cleanup committed.")
PY

