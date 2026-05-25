#!/usr/bin/env python3
"""Audit paid orders: escrow, blocks per miner, economics meta."""
import base64
import json
import sqlite3
import sys
from collections import defaultdict
from pathlib import Path

DB = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).resolve().parents[2] / "data" / "hackme.db"


def order_from_block(raw: str):
    try:
        j = json.loads(raw)
    except json.JSONDecodeError:
        return None, None
    task = j.get("task") or {}
    payload = task.get("payload")
    oid = None
    if isinstance(payload, str):
        for dec in (
            lambda x: json.loads(base64.b64decode(x)),
            lambda x: json.loads(x),
        ):
            try:
                p = dec(payload)
                oid = (p.get("order_task_id") or "").strip()
                if oid:
                    break
            except Exception:
                pass
    miner = j.get("miner_address", "?")
    return oid, miner


def main():
    db = sqlite3.connect(DB)
    by_order = defaultdict(lambda: {"n": 0, "miners": defaultdict(int)})
    miner_hmc = defaultdict(float)

    for _idx, raw in db.execute("SELECT block_index, json FROM blocks"):
        oid, miner = order_from_block(raw)
        if not oid:
            continue
        by_order[oid]["n"] += 1
        by_order[oid]["miners"][miner] += 1
        rw = db.execute("SELECT reward FROM tasks WHERE id=?", (oid,)).fetchone()
        if rw:
            miner_hmc[miner] += float(rw[0])

    print(f"DB: {DB}")
    print(f"Order blocks: {sum(v['n'] for v in by_order.values())}")
    print(f"Orders with blocks: {len(by_order)}")

    tasks = db.execute(
        "SELECT COUNT(*), SUM(prepaid_hmc), SUM(refunded_hmc) FROM tasks WHERE reward > 0"
    ).fetchone()
    print(f"Paid tasks: n={tasks[0]} prepaid={tasks[1]:.4f} refunded={tasks[2]:.4f}")

    for key in ("econ_order_escrow_units", "econ_total_minted_hmc", "econ_total_burned_hmc"):
        row = db.execute("SELECT value FROM meta WHERE key=?", (key,)).fetchone()
        print(f"  {key}: {row[0] if row else '?'}")

    want = sys.argv[2:] if len(sys.argv) > 2 else []
    if not want:
        want = sorted(by_order.keys(), key=lambda o: -by_order[o]["n"])[:10]

    for oid in want:
        if oid not in by_order:
            row = db.execute(
                "SELECT reward, progress_count, target_solves, status, prepaid_hmc FROM tasks WHERE id=?",
                (oid,),
            ).fetchone()
            print(f"\n{oid}: NO BLOCKS (task={row})")
            continue
        info = by_order[oid]
        row = db.execute(
            "SELECT reward, progress_count, target_solves, status, prepaid_hmc, refunded_hmc, payer_ref FROM tasks WHERE id=?",
            (oid,),
        ).fetchone()
        reward, prog, tgt, st, prepaid, refund, payer = row
        print(f"\n{oid}")
        print(f"  {st} {prog}/{tgt} reward={reward} prepaid={prepaid} refund={refund} payer={payer}")
        print(f"  blocks={info['n']} fee={prepaid*0.05:.6f} burn={prepaid*0.1:.6f}")
        for m, c in sorted(info["miners"].items(), key=lambda x: -x[1]):
            print(f"    {m}: {c} blocks → {c*reward:.4f} HMC")

    print("\nTop miners (order block rewards):")
    for m, h in sorted(miner_hmc.items(), key=lambda x: -x[1])[:12]:
        print(f"  {m}: {h:.6f} HMC")


if __name__ == "__main__":
    main()
