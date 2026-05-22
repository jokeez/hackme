#!/usr/bin/env python3
"""
Memory & leak spec: churn virtual workers (connect/disconnect + batches), sample runtime.ReadMemStats
via GET /api/work/admin/memstats and POST /api/work/admin/gc.

Env:
  LEAK_SPEC_QUICK=1  → 3 min / 80 workers (CI)
  default            → 2 h / 500 workers
"""
from __future__ import annotations

import argparse
import csv
import json
import os
import random
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.request
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass, field
from typing import Any


def post_json(url: str, body: dict | None, token: str, timeout: float) -> tuple[int, str]:
    data = b"" if body is None else json.dumps(body).encode()
    req = urllib.request.Request(url, data=data, method="POST")
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("X-Hackme-Admin-Token", token)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return int(resp.getcode()), resp.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return int(e.code), e.read().decode("utf-8", "replace")


def get_json(url: str, token: str, timeout: float) -> tuple[int, dict | None]:
    req = urllib.request.Request(url, method="GET")
    if token:
        req.add_header("X-Hackme-Admin-Token", token)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return int(resp.getcode()), json.loads(resp.read().decode() or "{}")
    except urllib.error.HTTPError as e:
        try:
            return int(e.code), json.loads(e.read().decode() or "{}")
        except Exception:
            return int(e.code), None
    except Exception:
        return 0, None


@dataclass
class ChurnWorker:
    worker_id: str
    coord: str
    token: str
    batch: int
    timeout: float
    active: threading.Event = field(default_factory=threading.Event)
    pending: dict[str, Any] | None = None
    submits: int = 0

    def claim(self) -> bool:
        code, raw = post_json(
            f"{self.coord}/api/work/claim",
            {"worker_id": self.worker_id, "batch_size": self.batch},
            self.token,
            self.timeout,
        )[:2]
        if code != 200:
            return False
        try:
            cj = json.loads(raw)
        except Exception:
            return False
        if not cj.get("ok"):
            return False
        self.pending = cj
        return True

    def submit_batch(self) -> bool:
        if not self.pending:
            return False
        cj = self.pending
        body = {
            "worker_id": self.worker_id,
            "base_nonce": int(cj.get("base_nonce") or 0),
            "batch_size": int(cj.get("batch_size") or self.batch),
            "work_id": str(cj.get("work_id") or ""),
            "attempts": int(cj.get("batch_size") or self.batch),
            "found": False,
            "result_hash": f"leak-{self.worker_id}-{random.randint(0, 2**31)}",
            "hashrate_gh_s": round(random.uniform(5.0, 90.0), 3),
        }
        code, raw = post_json(f"{self.coord}/api/work/submit", body, self.token, self.timeout)[:2]
        self.pending = None
        if code == 200:
            try:
                if json.loads(raw).get("ok"):
                    self.submits += 1
                    return True
            except Exception:
                pass
        return False

    def disconnect(self) -> None:
        self.active.clear()
        self.pending = None
        post_json(
            f"{self.coord}/api/push_work",
            {"worker_id": self.worker_id, "status": "offline", "hashrate_gh_s": 0},
            self.token,
            self.timeout,
        )


def memstats(coord: str, token: str, timeout: float) -> dict[str, Any]:
    code, data = get_json(f"{coord}/api/work/admin/memstats", token, timeout)
    if code != 200 or not data:
        return {}
    return data


def trigger_gc(coord: str, token: str, timeout: float) -> dict[str, Any]:
    code, raw = post_json(f"{coord}/api/work/admin/gc", None, token, timeout)
    if code != 200:
        return {}
    try:
        return json.loads(raw)
    except Exception:
        return {}


def worker_loop(w: ChurnWorker, end_ts: float) -> None:
    while time.time() < end_ts and w.active.is_set():
        if w.pending is None:
            w.claim()
        else:
            w.submit_batch()
        time.sleep(random.uniform(0.02, 0.12))


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--coord", required=True)
    ap.add_argument("--token", required=True)
    ap.add_argument("--workers", type=int, default=500)
    ap.add_argument("--duration-sec", type=int, default=7200)
    ap.add_argument("--batch-size", type=int, default=4096)
    ap.add_argument("--report-dir", required=True)
    ap.add_argument("--baseline-mb", type=float, default=20.8)
    ap.add_argument("--margin-mb", type=float, default=12.0)
    ap.add_argument("--sample-sec", type=float, default=30.0)
    ap.add_argument("--churn-batch", type=int, default=50)
    args = ap.parse_args()

    os.makedirs(args.report_dir, exist_ok=True)
    csv_path = os.path.join(args.report_dir, "mem_samples.csv")
    report_path = os.path.join(args.report_dir, "MEMORY_LEAK_SPEC_REPORT.md")

    print(f"[leak-spec] warm-up 45s workers={args.workers} duration={args.duration_sec}s")
    time.sleep(5)
    baseline_samples: list[float] = []
    for _ in range(3):
        ms = memstats(args.coord, args.token, 15.0)
        if ms:
            baseline_samples.append(float(ms.get("heap_alloc_mb") or 0))
        time.sleep(15)

    baseline_mb = args.baseline_mb
    if baseline_samples:
        baseline_mb = sum(baseline_samples) / len(baseline_samples)
    print(f"[leak-spec] baseline_heap_mb={baseline_mb:.2f} (target ~{args.baseline_mb})")

    workers = [
        ChurnWorker(f"leak-worker-{i:04d}", args.coord, args.token, args.batch_size, 12.0)
        for i in range(args.workers)
    ]

    end_ts = time.time() + args.duration_sec
    stop_sampler = threading.Event()
    samples: list[dict[str, Any]] = []

    def sampler() -> None:
        with open(csv_path, "w", encoding="utf-8", newline="") as f:
            w = csv.writer(f)
            w.writerow(
                [
                    "ts_unix",
                    "heap_alloc_mb",
                    "abuse_workers",
                    "ip_abuse_entries",
                    "active_rigs",
                    "workers_tracked",
                    "active_leases",
                ]
            )
            while not stop_sampler.is_set():
                ts = time.time()
                ms = memstats(args.coord, args.token, 15.0)
                if ms:
                    row = {
                        "ts_unix": ts,
                        "heap_alloc_mb": ms.get("heap_alloc_mb"),
                        "abuse_workers": ms.get("abuse_workers"),
                        "ip_abuse_entries": ms.get("ip_abuse_entries"),
                        "active_rigs": ms.get("active_rigs"),
                        "workers_tracked": ms.get("workers_tracked"),
                        "active_leases": ms.get("active_leases"),
                    }
                    samples.append(row)
                    w.writerow(
                        [
                            f"{ts:.3f}",
                            row["heap_alloc_mb"],
                            row["abuse_workers"],
                            row["ip_abuse_entries"],
                            row["active_rigs"],
                            row["workers_tracked"],
                            row["active_leases"],
                        ]
                    )
                    f.flush()
                stop_sampler.wait(args.sample_sec)

    sampler_th = threading.Thread(target=sampler, daemon=True)
    sampler_th.start()

    cycle = 0
    while time.time() < end_ts:
        cycle += 1
        active_pool = random.sample(workers, min(args.churn_batch, len(workers)))
        for w in workers:
            w.active.clear()
        for w in active_pool:
            w.active.set()
        post_json(
            f"{args.coord}/api/work/admin/clear-abuse",
            {"all": True},
            args.token,
            15.0,
        )
        with ThreadPoolExecutor(max_workers=min(64, len(active_pool))) as ex:
            futs = [ex.submit(worker_loop, w, time.time() + 25) for w in active_pool]
            for fu in futs:
                fu.result()
        for w in active_pool:
            w.disconnect()
        if cycle % 10 == 0:
            gc_out = trigger_gc(args.coord, args.token, 20.0)
            after = (gc_out.get("after") or {}) if gc_out else {}
            print(
                f"[leak-spec] cycle={cycle} heap_after_gc={after.get('heap_alloc_mb')} "
                f"ip_abuse={after.get('ip_abuse_entries')} rigs={after.get('active_rigs')}"
            )
        time.sleep(2)

    stop_sampler.set()
    sampler_th.join(timeout=10)

    for w in workers:
        w.disconnect()
    post_json(f"{args.coord}/api/work/admin/clear-abuse", {"all": True}, args.token, 15.0)
    gc_final = trigger_gc(args.coord, args.token, 30.0)
    time.sleep(3)
    final_ms = memstats(args.coord, args.token, 15.0)

    final_heap = float(final_ms.get("heap_alloc_mb") or 0)
    peak_heap = max((float(s.get("heap_alloc_mb") or 0) for s in samples), default=final_heap)
    max_ip_abuse = max((int(s.get("ip_abuse_entries") or 0) for s in samples), default=0)
    max_abuse = max((int(s.get("abuse_workers") or 0) for s in samples), default=0)
    max_rigs = max((int(s.get("active_rigs") or 0) for s in samples), default=0)

    ceiling = baseline_mb + args.margin_mb
    leak_heap = final_heap > ceiling
    leak_maps = max_ip_abuse > args.workers + 50 or max_abuse > args.workers + 50

    verdict = "PASS" if not leak_heap and not leak_maps else "FAIL"
    with open(report_path, "w", encoding="utf-8") as f:
        f.write("# Memory & leak spec report\n\n")
        f.write(f"- **Verdict:** {verdict}\n")
        f.write(f"- **Duration:** {args.duration_sec}s · **Workers:** {args.workers}\n")
        f.write(f"- **Baseline heap (MB):** {baseline_mb:.2f}\n")
        f.write(f"- **Final heap after GC (MB):** {final_heap:.2f} (ceiling {ceiling:.2f})\n")
        f.write(f"- **Peak heap (MB):** {peak_heap:.2f}\n")
        f.write(f"- **Max ip_abuse_entries:** {max_ip_abuse}\n")
        f.write(f"- **Max abuse_workers:** {max_abuse}\n")
        f.write(f"- **Max active_rigs:** {max_rigs}\n\n")
        f.write("## GC final\n\n```json\n")
        f.write(json.dumps(gc_final, indent=2))
        f.write("\n```\n")

    print(f"[leak-spec] {verdict} final_heap={final_heap:.2f}MB ceiling={ceiling:.2f}MB")
    print(f"[leak-spec] report={report_path}")
    return 0 if verdict == "PASS" else 1


if __name__ == "__main__":
    raise SystemExit(main())
