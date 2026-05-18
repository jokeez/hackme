#!/usr/bin/env python3
import argparse
import json
import random
import string
import threading
import time
import urllib.error
import urllib.request
from collections import Counter, defaultdict


def now_utc() -> str:
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())


def rand_id(prefix: str) -> str:
    suffix = "".join(random.choices(string.ascii_lowercase + string.digits, k=10))
    return f"{prefix}-{int(time.time())}-{suffix}"


HTTP_TIMEOUT_SEC = 15


def post_json(url: str, body: dict, token: str = ""):
    raw = json.dumps(body).encode("utf-8")
    req = urllib.request.Request(url, data=raw, method="POST")
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("X-Hackme-Admin-Token", token)
    try:
        with urllib.request.urlopen(req, timeout=HTTP_TIMEOUT_SEC) as resp:
            code = int(resp.getcode())
            body_raw = resp.read().decode("utf-8", "replace")
            return code, body_raw, ""
    except urllib.error.HTTPError as e:
        try:
            body_raw = e.read().decode("utf-8", "replace")
        except Exception:
            body_raw = ""
        return int(e.code), body_raw, ""
    except Exception as e:
        return 0, "", str(e)


def get_json(url: str):
    req = urllib.request.Request(url, method="GET")
    try:
        with urllib.request.urlopen(req, timeout=HTTP_TIMEOUT_SEC) as resp:
            raw = resp.read().decode("utf-8", "replace")
            return int(resp.getcode()), raw, ""
    except urllib.error.HTTPError as e:
        try:
            body_raw = e.read().decode("utf-8", "replace")
        except Exception:
            body_raw = ""
        return int(e.code), body_raw, ""
    except Exception as e:
        return 0, "", str(e)


def classify(code: int) -> str:
    if code == 0:
        return "network_error"
    if 200 <= code < 300:
        return "2xx"
    if 300 <= code < 400:
        return "3xx"
    if 400 <= code < 500:
        return "4xx"
    if 500 <= code < 600:
        return "5xx"
    return "other"


def worker_loop(name, end_ts, fn, counters, lock, min_delay_ms: int):
    local_codes = Counter()
    local_classes = Counter()
    local_errors = 0
    local_reqs = 0
    while time.time() < end_ts:
        code, _resp, err = fn()
        local_reqs += 1
        local_codes[str(code)] += 1
        local_classes[classify(code)] += 1
        if err:
            local_errors += 1
        if min_delay_ms > 0:
            time.sleep(min_delay_ms / 1000.0)
    with lock:
        counters[name]["requests"] += local_reqs
        counters[name]["errors"] += local_errors
        counters[name]["codes"].update(local_codes)
        counters[name]["classes"].update(local_classes)


def make_tx_fn(base):
    url = base.rstrip("/") + "/api/tx/send"

    def _fn():
        # malformed by design to stress validation + rate limits
        return post_json(url, {})

    return _fn


def make_orders_fn(base, token):
    url = base.rstrip("/") + "/api/tasks"
    mode = "nospend"

    def set_mode(v: str):
        nonlocal mode
        mode = (v or "nospend").strip().lower()

    def _fn():
        diff = random.choice([1, 2, 4, 10])
        # Two modes:
        # - nospend (default): intentionally fails fairness guard, no escrow debit
        # - spend: valid paid orders, debits wallet escrow
        if mode == "spend":
            reward = random.choice([0.005, 0.01, 0.02])
        else:
            reward = 0.0001
        body = {
            "id": rand_id("stress-order"),
            "kind": "synthetic_poh_v1",
            "difficulty_score": diff,
            "reward_hmc": reward,
            "target_solves": random.choice([1, 2, 4]),
            "payer_ref": "stress:mega",
        }
        return post_json(url, body, token=token)

    return _fn, set_mode


def make_coord_fn(coord, token):
    url = coord.rstrip("/") + "/api/work/claim"

    def _fn():
        body = {
            "worker_id": rand_id("stress-worker"),
            "batch_size": random.choice([200000, 500000, 1000000]),
        }
        return post_json(url, body, token=token)

    return _fn


def metrics_sampler(base, end_ts, interval, out, lock):
    max_cpu = -1.0
    max_mem = -1.0
    max_disk_write = -1.0
    min_hashrate = None
    samples = 0
    errors = 0

    while time.time() < end_ts:
        code, raw, err = get_json(base.rstrip("/") + "/api/metrics")
        if code == 200 and raw:
            try:
                j = json.loads(raw)
                samples += 1
                cpu = float(j.get("cpu_pct", -1))
                mem = float(j.get("mem_pct", -1))
                wr = float(j.get("disk_write_mbps", -1))
                hr = float(j.get("hashrate_th_s", -1))
                if cpu > max_cpu:
                    max_cpu = cpu
                if mem > max_mem:
                    max_mem = mem
                if wr > max_disk_write:
                    max_disk_write = wr
                if hr >= 0:
                    min_hashrate = hr if min_hashrate is None else min(min_hashrate, hr)
            except Exception:
                errors += 1
        else:
            errors += 1
            if err:
                pass
        time.sleep(interval)

    with lock:
        out["metrics"] = {
            "samples": samples,
            "errors": errors,
            "max_cpu_pct": max_cpu,
            "max_mem_pct": max_mem,
            "max_disk_write_mbps": max_disk_write,
            "min_hashrate_th_s": min_hashrate if min_hashrate is not None else -1,
        }


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", required=True)
    ap.add_argument("--coord", required=True)
    ap.add_argument("--duration-sec", type=int, default=600)
    ap.add_argument("--tx-workers", type=int, default=24)
    ap.add_argument("--orders-workers", type=int, default=8)
    ap.add_argument("--coord-workers", type=int, default=12)
    ap.add_argument("--admin-token", default="")
    ap.add_argument("--coord-admin-token", default="")
    ap.add_argument("--sample-interval-sec", type=float, default=2.0)
    ap.add_argument("--orders-mode", choices=["nospend", "spend"], default="nospend")
    ap.add_argument("--profile", choices=["mixed", "tx-heavy", "orders-heavy", "coord-heavy"], default="mixed")
    ap.add_argument("--worker-delay-ms", type=int, default=10)
    ap.add_argument("--output", required=True)
    args = ap.parse_args()
    if args.profile == "tx-heavy":
        args.tx_workers = max(args.tx_workers, 64)
        args.orders_workers = min(args.orders_workers, 4)
        args.coord_workers = min(args.coord_workers, 8)
    elif args.profile == "orders-heavy":
        args.tx_workers = min(args.tx_workers, 12)
        args.orders_workers = max(args.orders_workers, 24)
        args.coord_workers = min(args.coord_workers, 8)
    elif args.profile == "coord-heavy":
        args.tx_workers = min(args.tx_workers, 12)
        args.orders_workers = min(args.orders_workers, 4)
        args.coord_workers = max(args.coord_workers, 32)

    end_ts = time.time() + max(1, args.duration_sec)
    lock = threading.Lock()

    counters = defaultdict(lambda: {
        "requests": 0,
        "errors": 0,
        "codes": Counter(),
        "classes": Counter(),
    })
    report = {
        "generated_at": now_utc(),
        "base": args.base,
        "coord": args.coord,
        "duration_sec": args.duration_sec,
        "profile": args.profile,
        "scenarios": {},
        "metrics": {},
    }

    threads = []
    for _ in range(max(0, args.tx_workers)):
        t = threading.Thread(
            target=worker_loop,
            args=("tx_burst", end_ts, make_tx_fn(args.base), counters, lock, args.worker_delay_ms),
            daemon=True,
        )
        threads.append(t)
        t.start()

    orders_fn, set_orders_mode = make_orders_fn(args.base, args.admin_token)
    set_orders_mode(args.orders_mode)
    for _ in range(max(0, args.orders_workers)):
        t = threading.Thread(
            target=worker_loop,
            args=("orders_burst", end_ts, orders_fn, counters, lock, args.worker_delay_ms),
            daemon=True,
        )
        threads.append(t)
        t.start()

    for _ in range(max(0, args.coord_workers)):
        t = threading.Thread(
            target=worker_loop,
            args=(
                "coordinator_claim_burst",
                end_ts,
                make_coord_fn(args.coord, args.coord_admin_token),
                counters,
                lock,
                args.worker_delay_ms,
            ),
            daemon=True,
        )
        threads.append(t)
        t.start()

    smp = threading.Thread(
        target=metrics_sampler,
        args=(args.base, end_ts, max(0.2, args.sample_interval_sec), report, lock),
        daemon=True,
    )
    smp.start()
    threads.append(smp)

    for t in threads:
        t.join()

    for name, agg in counters.items():
        req = int(agg["requests"])
        cls = dict(agg["classes"])
        codes = dict(agg["codes"])
        report["scenarios"][name] = {
            "requests": req,
            "errors": int(agg["errors"]),
            "codes": codes,
            "classes": cls,
            "ratio_5xx": (cls.get("5xx", 0) / req) if req else 0.0,
            "ratio_network_error": (cls.get("network_error", 0) / req) if req else 0.0,
        }

    with open(args.output, "w", encoding="utf-8") as f:
        json.dump(report, f, ensure_ascii=False, indent=2)


if __name__ == "__main__":
    main()

