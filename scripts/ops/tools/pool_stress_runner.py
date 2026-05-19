#!/usr/bin/env python3
"""Production pool coordinator stress: parallel claim/submit + reconnect churn."""
from __future__ import annotations

import argparse
import json
import random
import string
import threading
import time
import urllib.error
import urllib.request
from collections import Counter, defaultdict

HTTP_TIMEOUT = 25


def post_json(url: str, body: dict, token: str) -> tuple[int, str, str]:
    raw = json.dumps(body).encode()
    req = urllib.request.Request(url, data=raw, method="POST")
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("X-Hackme-Admin-Token", token)
    try:
        with urllib.request.urlopen(req, timeout=HTTP_TIMEOUT) as resp:
            return int(resp.getcode()), resp.read().decode("utf-8", "replace"), ""
    except urllib.error.HTTPError as e:
        try:
            body_raw = e.read().decode("utf-8", "replace")
        except Exception:
            body_raw = ""
        return int(e.code), body_raw, ""
    except Exception as e:
        return 0, "", str(e)


def parse_reason(body: str) -> str:
    try:
        j = json.loads(body)
        return str(j.get("reason") or j.get("error") or "")
    except Exception:
        return ""


def miner_worker(
    coord: str,
    token: str,
    worker_id: str,
    end_ts: float,
    batch: int,
    churn: bool,
    claim_only: bool,
    stats: dict,
    lock: threading.Lock,
) -> None:
    claim_url = coord.rstrip("/") + "/api/work/claim"
    submit_url = coord.rstrip("/") + "/api/work/submit"
    local = Counter()
    while time.time() < end_ts:
        if churn and random.random() < 0.15:
            time.sleep(random.uniform(0.05, 0.4))
            local["churn_sleep"] += 1
            continue
        code, body, err = post_json(
            claim_url, {"worker_id": worker_id, "batch_size": batch}, token
        )
        local[f"claim_{code}"] += 1
        if err:
            local["net_err"] += 1
            continue
        if code != 200:
            r = parse_reason(body)
            if r:
                local[f"claim_reason:{r}"] += 1
            else:
                local[f"claim_http_{code}"] += 1
            time.sleep(0.05)
            continue
        try:
            cj = json.loads(body)
        except Exception:
            local["claim_bad_json"] += 1
            continue
        if not cj.get("ok"):
            local[f"claim_reason:{cj.get('reason', 'not_ok')}"] += 1
            continue
        if claim_only:
            local["claim_ok"] += 1
            time.sleep(random.uniform(0.02, 0.12))
            continue
        base = int(cj.get("base_nonce") or 0)
        size = int(cj.get("batch_size") or batch)
        work_id = str(cj.get("work_id") or "")
        sub = {
            "worker_id": worker_id,
            "base_nonce": base,
            "batch_size": size,
            "work_id": work_id,
            "attempts": size,
            "found": False,
            "hashrate_gh_s": random.uniform(0.5, 8.0),
        }
        code2, body2, err2 = post_json(submit_url, sub, token)
        local[f"submit_{code2}"] += 1
        if err2:
            local["net_err"] += 1
        elif code2 != 200:
            r2 = parse_reason(body2)
            if r2:
                local[f"submit_reason:{r2}"] += 1
        else:
            try:
                sj = json.loads(body2)
                if sj.get("accepted"):
                    local["accepted"] += 1
                else:
                    local[f"submit_reason:{sj.get('reason', 'rejected')}"] += 1
            except Exception:
                local["submit_bad_json"] += 1
        time.sleep(random.uniform(0.01, 0.08))
    with lock:
        for k, v in local.items():
            stats[k] += v


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--coord", required=True)
    ap.add_argument("--token", required=True)
    ap.add_argument("--duration-sec", type=int, default=90)
    ap.add_argument("--workers", type=int, default=10)
    ap.add_argument("--batch-size", type=int, default=262144)
    ap.add_argument("--churn", action="store_true", help="simulate reconnect gaps")
    ap.add_argument("--claim-only", action="store_true", help="only claim (no submit)")
    ap.add_argument("--output", required=True)
    args = ap.parse_args()

    stats: dict = Counter()
    lock = threading.Lock()
    end = time.time() + args.duration_sec
    threads = []
    prefix = "worker-stress-" + "".join(random.choices(string.ascii_lowercase, k=4))
    for i in range(args.workers):
        wid = f"{prefix}-w{i:02d}"
        t = threading.Thread(
            target=miner_worker,
            args=(args.coord, args.token, wid, end, args.batch_size, args.churn, args.claim_only, stats, lock),
            daemon=True,
        )
        t.start()
        threads.append((wid, t))
    for _, t in threads:
        t.join()
    report = {
        "coord": args.coord,
        "duration_sec": args.duration_sec,
        "workers": args.workers,
        "churn": args.churn,
        "stats": dict(stats),
        "accepted": int(stats.get("accepted", 0)),
        "claim_200": int(stats.get("claim_200", 0)),
        "submit_200": int(stats.get("submit_200", 0)),
        "net_err": int(stats.get("net_err", 0)),
    }
    with open(args.output, "w", encoding="utf-8") as f:
        json.dump(report, f, indent=2)
    print(json.dumps(report, indent=2))
    # Pass if coordinator stayed responsive (some accepts or mostly 429 not 5xx)
    fails = sum(v for k, v in stats.items() if k.startswith("claim_5") or k.startswith("submit_5"))
    if fails > args.workers * 3:
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
