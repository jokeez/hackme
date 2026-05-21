#!/usr/bin/env python3
"""Simulate N virtual pool workers against a coordinator (claim/submit loop).

Usage:
  COORD=http://127.0.0.1:8081 WORKERS=15 DURATION_SEC=45 bash scripts/tests/mock_miners_load.sh

Checks:
  - Parallel submissions do not stall (claims keep succeeding)
  - /api/work/stats reflects worker count and aggregate hashrate
  - Optional node dashboard proxy via NODE_BASE (pool strip / network stats)
"""
from __future__ import annotations

import argparse
import json
import os
import random
import string
import sys
import threading
import time
import urllib.error
import urllib.request
from collections import Counter


def post_json(url: str, body: dict, token: str, timeout: float) -> tuple[int, str, str]:
    raw = json.dumps(body).encode()
    req = urllib.request.Request(url, data=raw, method="POST")
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("X-Hackme-Admin-Token", token)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return int(resp.getcode()), resp.read().decode("utf-8", "replace"), ""
    except urllib.error.HTTPError as e:
        try:
            body_raw = e.read().decode("utf-8", "replace")
        except Exception:
            body_raw = ""
        return int(e.code), body_raw, ""
    except Exception as e:
        return 0, "", str(e)


def get_json(url: str, timeout: float, token: str = "") -> tuple[int, dict | None, str]:
    req = urllib.request.Request(url, method="GET")
    if token:
        req.add_header("X-Hackme-Admin-Token", token)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            data = json.loads(resp.read().decode("utf-8", "replace") or "{}")
            return int(resp.getcode()), data, ""
    except urllib.error.HTTPError as e:
        try:
            data = json.loads(e.read().decode("utf-8", "replace") or "{}")
        except Exception:
            data = None
        return int(e.code), data, ""
    except Exception as e:
        return 0, None, str(e)


def worker_loop(
    coord: str,
    token: str,
    worker_id: str,
    end_ts: float,
    batch: int,
    stats: Counter,
    lock: threading.Lock,
    timeout: float,
) -> None:
    claim_url = coord.rstrip("/") + "/api/work/claim"
    submit_url = coord.rstrip("/") + "/api/work/submit"
    local = Counter()
    nonce_hint = random.randint(0, 1_000_000)
    while time.time() < end_ts:
        code, body, err = post_json(
            claim_url,
            {"worker_id": worker_id, "batch_size": batch, "suggested_nonce": nonce_hint},
            token,
            timeout,
        )
        if 200 <= code < 300:
            local["claim_ok"] += 1
        else:
            local[f"claim_{code}"] += 1
        if err or not (200 <= code < 300):
            time.sleep(0.05)
            continue
        try:
            cj = json.loads(body)
        except Exception:
            time.sleep(0.05)
            continue
        base = int(cj.get("base_nonce") or 0)
        size = int(cj.get("batch_size") or batch)
        work_id = str(cj.get("work_id") or "")
        nonce_hint = base + size
        submit_body = {
            "worker_id": worker_id,
            "base_nonce": base,
            "batch_size": size,
            "work_id": work_id,
            "attempts": size,
            "found": False,
            "result_hash": "mock-" + worker_id + "-" + "".join(random.choices(string.hexdigits[:16], k=8)),
            "hashrate_gh_s": round(random.uniform(0.05, 2.5), 3),
        }
        sc, sb, serr = post_json(submit_url, submit_body, token, timeout)
        if 200 <= sc < 300:
            local["submit_ok"] += 1
        else:
            local[f"submit_{sc}"] += 1
        if serr:
            local["net_err"] += 1
        time.sleep(random.uniform(0.02, 0.12))
    with lock:
        stats.update(local)


def main() -> int:
    ap = argparse.ArgumentParser(description="Mock coordinator miners load test")
    ap.add_argument("--coord", default=os.environ.get("COORD", "http://127.0.0.1:8081"))
    ap.add_argument("--token", default=os.environ.get("COORD_ADMIN_TOKEN", os.environ.get("ADMIN_TOKEN", "")))
    ap.add_argument("--workers", type=int, default=int(os.environ.get("WORKERS", "15")))
    ap.add_argument("--duration", type=int, default=int(os.environ.get("DURATION_SEC", "40")))
    ap.add_argument("--batch", type=int, default=int(os.environ.get("BATCH_SIZE", "512")))
    ap.add_argument("--node", default=os.environ.get("NODE_BASE", ""))
    ap.add_argument("--timeout", type=float, default=float(os.environ.get("HTTP_TIMEOUT", "12")))
    args = ap.parse_args()

    if args.workers < 1:
        print("workers must be >= 1", file=sys.stderr)
        return 2

    stats: Counter = Counter()
    lock = threading.Lock()
    end_ts = time.time() + args.duration
    threads = []
    prefix = os.environ.get("WORKER_PREFIX", "mock-worker-")
    for i in range(args.workers):
        wid = f"{prefix}{i:02d}"
        th = threading.Thread(
            target=worker_loop,
            args=(args.coord, args.token, wid, end_ts, args.batch, stats, lock, args.timeout),
            daemon=True,
        )
        th.start()
        threads.append(th)
    for th in threads:
        th.join(timeout=args.duration + 30)

    code, work, err = get_json(args.coord.rstrip("/") + "/api/work/stats?details=1", args.timeout, args.token)
    if code != 200 or not work:
        print(f"FAIL: work/stats http={code} err={err}", file=sys.stderr)
        return 1

    workers_map = work.get("workers") or {}
    wc = int(work.get("workers_count") or len(workers_map) or 0)
    rigs = work.get("active_rigs") or []
    attempts = int(work.get("accepted_attempts") or 0)

    net_code, net, _ = get_json(args.coord.rstrip("/") + "/api/network/stats", args.timeout, args.token)
    global_th = 0.0
    total_miners = 0
    if net_code == 200 and net:
        global_th = float(net.get("global_hashrate_th_s") or 0)
        total_miners = int(net.get("total_miners") or 0)

    print(json.dumps({
        "workers_configured": args.workers,
        "workers_seen": wc,
        "accepted_attempts": attempts,
        "active_rigs": len(rigs),
        "global_hashrate_th_s": global_th,
        "total_miners": total_miners,
        "claim_ok": stats.get("claim_ok", 0),
        "submit_ok": stats.get("submit_ok", 0),
        "net_err": stats.get("net_err", 0),
    }, indent=2))

    if wc < max(1, args.workers // 2):
        print(f"FAIL: too few workers in stats ({wc} < {args.workers // 2})", file=sys.stderr)
        return 1
    if stats.get("claim_ok", 0) < args.workers and attempts < args.workers * 5:
        print(f"FAIL: low claim/submit activity claim_ok={stats.get('claim_ok', 0)} attempts={attempts}", file=sys.stderr)
        return 1
    if attempts < args.workers * 5:
        print(f"FAIL: accepted_attempts too low ({attempts})", file=sys.stderr)
        return 1

    if args.node:
        nc, node_st, _ = get_json(args.node.rstrip("/") + "/api/work/stats", args.timeout, args.token)
        if nc == 200 and node_st:
            print("node proxy work/stats ok", file=sys.stderr)
        else:
            print(f"WARN: node proxy stats http={nc}", file=sys.stderr)

    print("PASS: mock miners load", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
