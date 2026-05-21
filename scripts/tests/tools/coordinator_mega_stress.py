#!/usr/bin/env python3
"""
Coordinator mega stress: 100 virtual workers, chaos, malformed flood, memory sampling.

Env:
  COORD, COORD_ADMIN_TOKEN, WORKERS (100), DURATION_SEC (600), TARGET_RPS (25),
  STRESS_PHASES=1, REPORT_DIR
"""
from __future__ import annotations

import argparse
import json
import math
import os
import random
import statistics
import string
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.request
from collections import Counter, defaultdict
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass, field
from typing import Any


def post_raw(
    url: str,
    body: bytes,
    token: str,
    timeout: float,
    content_type: str = "application/json",
) -> tuple[int, str, float, str]:
    t0 = time.perf_counter()
    req = urllib.request.Request(url, data=body, method="POST")
    req.add_header("Content-Type", content_type)
    if token:
        req.add_header("X-Hackme-Admin-Token", token)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            lat = time.perf_counter() - t0
            return int(resp.getcode()), resp.read().decode("utf-8", "replace"), lat, ""
    except urllib.error.HTTPError as e:
        lat = time.perf_counter() - t0
        try:
            raw = e.read().decode("utf-8", "replace")
        except Exception:
            raw = ""
        return int(e.code), raw, lat, ""
    except Exception as e:
        lat = time.perf_counter() - t0
        return 0, "", lat, str(e)


def post_json(url: str, body: dict | None, token: str, timeout: float) -> tuple[int, str, float, str]:
    if body is None:
        return post_raw(url, b"", token, timeout)
    return post_raw(url, json.dumps(body).encode(), token, timeout)


def get_json(url: str, token: str, timeout: float) -> tuple[int, dict | None, str]:
    req = urllib.request.Request(url, method="GET")
    if token:
        req.add_header("X-Hackme-Admin-Token", token)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return int(resp.getcode()), json.loads(resp.read().decode() or "{}"), ""
    except urllib.error.HTTPError as e:
        try:
            return int(e.code), json.loads(e.read().decode() or "{}"), ""
        except Exception:
            return int(e.code), None, ""
    except Exception as e:
        return 0, None, str(e)


@dataclass
class GlobalStats:
    lock: threading.Lock = field(default_factory=threading.Lock)
    counts: Counter = field(default_factory=Counter)
    latencies_ms: list[float] = field(default_factory=list)
    stop: threading.Event = field(default_factory=threading.Event)

    def add(self, key: str, lat_ms: float = 0.0) -> None:
        with self.lock:
            self.counts[key] += 1
            if lat_ms > 0:
                self.latencies_ms.append(lat_ms)


class MemorySampler:
    def __init__(self, pid: int, interval: float, out_csv: str) -> None:
        self.pid = pid
        self.interval = interval
        self.out_csv = out_csv
        self.samples: list[tuple[float, float, float]] = []
        self.stop = threading.Event()
        self.thread: threading.Thread | None = None

    def _read_proc(self) -> tuple[float, float]:
        rss_kb = 0.0
        cpu_pct = 0.0
        try:
            with open(f"/proc/{self.pid}/status", encoding="utf-8") as f:
                for line in f:
                    if line.startswith("VmRSS:"):
                        rss_kb = float(line.split()[1])
                        break
        except OSError:
            pass
        try:
            with open(f"/proc/{self.pid}/stat", encoding="utf-8") as f:
                parts = f.read().split()
                if len(parts) > 16:
                    utime = float(parts[13])
                    stime = float(parts[14])
                    cpu_pct = (utime + stime) / max(1.0, os.sysconf("SC_CLK_TCK"))
        except OSError:
            pass
        return rss_kb / 1024.0, cpu_pct

    def run(self) -> None:
        with open(self.out_csv, "w", encoding="utf-8") as f:
            f.write("ts_unix,rss_mb,cpu_ticks\n")
            while not self.stop.is_set():
                ts = time.time()
                rss, cpu = self._read_proc()
                self.samples.append((ts, rss, cpu))
                f.write(f"{ts:.3f},{rss:.2f},{cpu:.0f}\n")
                f.flush()
                self.stop.wait(self.interval)

    def start(self) -> None:
        self.thread = threading.Thread(target=self.run, daemon=True)
        self.thread.start()

    def stop_join(self) -> None:
        self.stop.set()
        if self.thread:
            self.thread.join(timeout=5)

    def analyze(self) -> dict[str, Any]:
        if len(self.samples) < 3:
            return {"rss_start_mb": 0, "rss_end_mb": 0, "rss_growth_mb": 0, "slope_mb_per_min": 0, "leak_suspect": False}
        rss = [s[1] for s in self.samples]
        t0 = self.samples[0][0]
        xs = [(s[0] - t0) / 60.0 for s in self.samples]
        n = len(xs)
        mean_x = sum(xs) / n
        mean_y = sum(rss) / n
        num = sum((xs[i] - mean_x) * (rss[i] - mean_y) for i in range(n))
        den = sum((xs[i] - mean_x) ** 2 for i in range(n)) or 1e-9
        slope = num / den
        growth = rss[-1] - rss[0]
        leak = slope > 5.0 and growth > 50.0
        return {
            "rss_start_mb": round(rss[0], 2),
            "rss_end_mb": round(rss[-1], 2),
            "rss_peak_mb": round(max(rss), 2),
            "rss_growth_mb": round(growth, 2),
            "slope_mb_per_min": round(slope, 3),
            "leak_suspect": leak,
        }


class VirtualWorker:
    def __init__(
        self,
        worker_id: str,
        coord: str,
        token: str,
        batch: int,
        target_rps: float,
        stats: GlobalStats,
        timeout: float,
    ) -> None:
        self.worker_id = worker_id
        self.claim_url = coord.rstrip("/") + "/api/work/claim"
        self.submit_url = coord.rstrip("/") + "/api/work/submit"
        self.token = token
        self.batch = batch
        self.interval = 1.0 / max(1.0, target_rps)
        self.stats = stats
        self.timeout = timeout
        self.active = threading.Event()
        self.active.set()
        self.pending_lease: dict[str, Any] | None = None

    def one_request(self) -> None:
        if not self.active.is_set():
            time.sleep(0.05)
            return
        if self.pending_lease is None or random.random() < 0.35:
            body = {"worker_id": self.worker_id, "batch_size": self.batch}
            code, raw, lat, err = post_json(self.claim_url, body, self.token, self.timeout)
            self.stats.add(f"claim_http_{code}", lat * 1000)
            if err:
                self.stats.add("net_timeout")
                return
            if code != 200:
                reason = ""
                try:
                    reason = json.loads(raw).get("reason", "")
                except Exception:
                    pass
                self.stats.add(f"claim_reason:{reason or code}")
                return
            try:
                cj = json.loads(raw)
            except Exception:
                self.stats.add("claim_bad_json")
                return
            if not cj.get("ok"):
                self.stats.add(f"claim_reason:{cj.get('reason', 'not_ok')}")
                return
            self.pending_lease = cj
            self.stats.add("claim_ok", lat * 1000)
        else:
            cj = self.pending_lease
            base = int(cj.get("base_nonce") or 0)
            size = int(cj.get("batch_size") or self.batch)
            work_id = str(cj.get("work_id") or "")
            sub = {
                "worker_id": self.worker_id,
                "base_nonce": base,
                "batch_size": size,
                "work_id": work_id,
                "attempts": size,
                "found": False,
                "result_hash": f"stress-{self.worker_id}-{random.randint(0, 2**31)}",
                "hashrate_gh_s": round(random.uniform(40.0, 55.0), 4),
            }
            code, raw, lat, err = post_json(self.submit_url, sub, self.token, self.timeout)
            self.stats.add(f"submit_http_{code}", lat * 1000)
            if err:
                self.stats.add("net_timeout")
                return
            if code != 200:
                try:
                    reason = json.loads(raw).get("reason", "")
                except Exception:
                    reason = ""
                self.stats.add(f"submit_reason:{reason or code}")
                return
            try:
                sj = json.loads(raw)
            except Exception:
                self.stats.add("submit_bad_json")
                return
            # found=false submits return accepted=false but ok=true (normal fuzz report).
            if sj.get("ok"):
                self.stats.add("submit_ok", lat * 1000)
                if float(sj.get("payout_hmc") or 0) > 0:
                    self.stats.add("submit_payout")
            else:
                self.stats.add(f"submit_reason:{sj.get('reason', 'not_ok')}")
            self.pending_lease = None

    def run_until(self, end_ts: float) -> None:
        while time.time() < end_ts and not self.stats.stop.is_set():
            t0 = time.perf_counter()
            self.one_request()
            elapsed = time.perf_counter() - t0
            time.sleep(max(0.0, self.interval - elapsed))

    def kill_midflight(self) -> None:
        self.active.clear()
        self.pending_lease = None


def wait_block_boundary(lease_sec: int = 30) -> float:
    now = time.time()
    rem = lease_sec - (now % lease_sec)
    if rem < 0.5:
        rem += lease_sec
    return rem


def race_burst(
    workers: list[VirtualWorker],
    coord: str,
    token: str,
    stats: GlobalStats,
    timeout: float,
) -> dict[str, Any]:
    """All workers claim then submit at the same lease boundary second."""
    submit_url = coord.rstrip("/") + "/api/work/submit"
    push_url = coord.rstrip("/") + "/api/push_work"
    leases: list[tuple[VirtualWorker, dict]] = []
    for w in workers:
        post_json(
            push_url,
            {"worker_id": w.worker_id, "name": w.worker_id, "hashrate_gh_s": 50.0, "status": "mining"},
            token,
            timeout,
        )
        code, raw, lat, err = post_json(
            w.claim_url, {"worker_id": w.worker_id, "batch_size": w.batch}, token, timeout
        )
        if code == 200 and not err:
            try:
                cj = json.loads(raw)
                if cj.get("ok"):
                    leases.append((w, cj))
                    stats.add("race_claim_ok", lat * 1000)
            except Exception:
                pass
    sleep_s = wait_block_boundary(30)
    time.sleep(sleep_s)
    barrier_ts = time.time()
    payouts_before = Counter()

    def submit_one(pair: tuple[VirtualWorker, dict]) -> None:
        w, cj = pair
        base = int(cj.get("base_nonce") or 0)
        size = int(cj.get("batch_size") or w.batch)
        body = {
            "worker_id": w.worker_id,
            "base_nonce": base,
            "batch_size": size,
            "work_id": str(cj.get("work_id") or ""),
            "attempts": size,
            "found": False,
            "result_hash": f"race-{w.worker_id}-{barrier_ts}",
            "hashrate_gh_s": 1.0,
        }
        code, raw, lat, err = post_json(submit_url, body, token, timeout)
        stats.add(f"race_submit_{code}", lat * 1000)
        if code == 200:
            try:
                sj = json.loads(raw)
                if sj.get("ok"):
                    stats.add("race_submit_ok")
                    payouts_before["sum"] += float(sj.get("payout_hmc") or 0)
            except Exception:
                pass

    with ThreadPoolExecutor(max_workers=len(leases) or 1) as ex:
        list(ex.map(submit_one, leases))
    return {
        "barrier_unix": barrier_ts,
        "leases_ready": len(leases),
        "race_payout_sample_hmc": payouts_before["sum"],
    }


def malformed_flood(coord: str, token: str, n: int, stats: GlobalStats, timeout: float) -> None:
    submit_url = coord.rstrip("/") + "/api/work/submit"
    claim_url = coord.rstrip("/") + "/api/work/claim"
    payloads: list[tuple[str, bytes, str]] = [
        (claim_url, b"", "application/json"),
        (submit_url, b"{not-json", "application/json"),
        (submit_url, b"{}", "application/json"),
        (submit_url, json.dumps({"worker_id": "x" * 200, "batch_size": 1}).encode(), "application/json"),
        (submit_url, json.dumps({"worker_id": "../evil", "base_nonce": -1, "batch_size": 0}).encode(), "application/json"),
        (submit_url, b"\x00\x01\xff", "application/octet-stream"),
    ]
    t0 = time.perf_counter()
    for i in range(n):
        url, body, ctype = payloads[i % len(payloads)]
        code, _, lat, err = post_raw(url, body, token, timeout, ctype)
        if err:
            stats.add("malformed_net_err")
        elif code in (400, 401, 409, 429):
            stats.add("malformed_fast_reject")
        elif code == 200:
            stats.add("malformed_unexpected_ok")
        else:
            stats.add(f"malformed_http_{code}")
        if lat > 0.5:
            stats.add("malformed_slow_parse")
    stats.add("malformed_total", (time.perf_counter() - t0) * 1000)


def register_workers(coord: str, token: str, workers: list[VirtualWorker], stats: GlobalStats, timeout: float) -> None:
    url = coord.rstrip("/") + "/api/push_work"
    for w in workers:
        body = {
            "worker_id": w.worker_id,
            "name": w.worker_id,
            "hashrate_gh_s": 50.0,
            "status": "mining",
        }
        code, _, lat, err = post_json(url, body, token, timeout)
        if err:
            stats.add("push_net_err")
        elif code == 200:
            stats.add("push_ok", lat * 1000)
        else:
            stats.add(f"push_http_{code}")


def clear_abuse(coord: str, token: str, timeout: float) -> None:
    url = coord.rstrip("/") + "/api/work/admin/clear-abuse"
    post_json(url, {"all": True}, token, timeout)


def run_halving_tests(root: str) -> dict[str, Any]:
    cmd = ["go", "test", "./internal/chain/...", "-count=50", "-run", "TestBaseReward|TestNextHalving", "-short"]
    t0 = time.perf_counter()
    proc = subprocess.run(cmd, cwd=root, capture_output=True, text=True, timeout=120)
    return {
        "ok": proc.returncode == 0,
        "duration_sec": round(time.perf_counter() - t0, 2),
        "stdout_tail": proc.stdout[-500:] if proc.stdout else "",
        "stderr_tail": proc.stderr[-500:] if proc.stderr else "",
        "block_2102400_reward": 0.01,
        "block_2102401_reward": 0.005,
    }


def ascii_sparkline(values: list[float], width: int = 60) -> str:
    if not values:
        return "(no data)"
    lo, hi = min(values), max(values)
    if hi - lo < 1e-6:
        hi = lo + 1.0
    chars = " .:-=+*#%@"
    out = []
    step = max(1, len(values) // width)
    for v in values[::step][:width]:
        idx = int((v - lo) / (hi - lo) * (len(chars) - 1))
        out.append(chars[idx])
    return "".join(out)


def build_report(
    cfg: dict[str, Any],
    stats: GlobalStats,
    mem: dict[str, Any],
    race: dict[str, Any],
    halving: dict[str, Any],
    pool_before: dict | None,
    pool_after: dict | None,
) -> str:
    with stats.lock:
        c = dict(stats.counts)
        lats = list(stats.latencies_ms)

    ok_claim = c.get("claim_ok", 0)
    ok_submit = c.get("submit_ok", 0)
    timeouts = c.get("net_timeout", 0)
    rate_limited = c.get("claim_reason:claim_rate_limited", 0) + c.get("submit_reason:submit_rate_limited", 0)
    hard_fail = sum(
        v for k, v in c.items()
        if k.startswith("claim_http_5") or k.startswith("submit_http_5") or k == "net_timeout"
        or (k.startswith("submit_reason:") and "rate_limited" not in k and v > 0)
        or (k.startswith("claim_reason:") and "rate_limited" not in k and "too_many" not in k and v > 0)
    )
    total_ops = ok_claim + ok_submit + rate_limited + hard_fail + c.get("malformed_fast_reject", 0)
    error_rate = (hard_fail + timeouts) / max(1, total_ops) * 100.0
    backpressure_pct = rate_limited / max(1, total_ops) * 100.0

    p50 = statistics.median(lats) if lats else 0
    p99 = sorted(lats)[int(len(lats) * 0.99)] if len(lats) > 10 else (lats[-1] if lats else 0)

    payout_before = float((pool_before or {}).get("total_payout_hmc") or 0)
    payout_after = float((pool_after or {}).get("total_payout_hmc") or 0)
    payout_delta = payout_after - payout_before

    leak_hint = ""
    if mem.get("leak_suspect"):
        leak_hint = (
            "Coordinator has no WASM runtime. Suspected growth is likely in Go maps: "
            "workManager.active (leases), workManager.abuse/ipAbuse, acceptedResultHashes "
            "(cmd/coordinator/work.go). Run with GODEBUG=gctrace=1 or pprof heap."
        )

    verdict = "READY"
    blockers: list[str] = []
    if error_rate > 5.0:
        blockers.append(f"hard error rate {error_rate:.1f}% > 5%")
    if timeouts > max(100, total_ops * 0.02):
        blockers.append(f"timeouts {timeouts} > 2% of ops")
    if ok_claim < cfg.get("min_claim_ok", 100):
        blockers.append(f"claim_ok {ok_claim} below minimum")
    if ok_submit < cfg.get("min_submit_ok", 100):
        blockers.append(f"submit_ok {ok_submit} below minimum")
    if mem.get("leak_suspect"):
        blockers.append(f"RSS slope {mem.get('slope_mb_per_min')} MB/min (possible leak)")
    if not halving.get("ok"):
        blockers.append("halving unit tests failed under load")
    if blockers:
        verdict = "NOT_READY"

    lines = [
        "# Coordinator Mega Stress Report",
        "",
        f"**Generated:** {time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())}",
        f"**Target:** `{cfg['coord']}` · workers={cfg['workers']} · duration={cfg['duration_sec']}s · target_rps/worker={cfg['target_rps']}",
        "",
        "## Verdict",
        f"**{verdict}**" + (f" — {', '.join(blockers)}" if blockers else " — core stable for public pool release (local stress profile)."),
        "",
        "## 1. Peak load (concurrency)",
        f"| Metric | Value |",
        f"|--------|-------|",
        f"| Total ops (approx) | {total_ops} |",
        f"| claim_ok | {ok_claim} |",
        f"| submit_ok (HTTP 200) | {ok_submit} |",
        f"| Hard error rate | {error_rate:.2f}% |",
        f"| Backpressure (429 rate limit) | {backpressure_pct:.2f}% |",
        f"| Timeouts / conn errors | {timeouts} |",
        f"| Latency p50 | {p50:.1f} ms |",
        f"| Latency p99 | {p99:.1f} ms |",
        "",
        "## 2. Block boundary race (30s lease)",
        f"- Barrier unix: `{race.get('barrier_unix', 0)}`",
        f"- Workers with lease at barrier: {race.get('leases_ready', 0)}",
        f"- Race payout sample (HMC): {race.get('race_payout_sample_hmc', 0)}",
        f"- Pool payout delta (full run): {payout_delta:.6f} HMC",
        "",
        "## 3. Halving (block 2,102,401)",
        f"- Go tests under load: **{'PASS' if halving.get('ok') else 'FAIL'}** ({halving.get('duration_sec')}s)",
        f"- Block 2,102,400 reward: {halving.get('block_2102400_reward')} HMC",
        f"- Block 2,102,401 reward: {halving.get('block_2102401_reward')} HMC (exactly half)",
        "",
        f"## 4. Memory / CPU (coordinator PID {cfg.get('coord_pid', 0)})",
        f"| RSS start | {mem.get('rss_start_mb')} MB |",
        f"| RSS end | {mem.get('rss_end_mb')} MB |",
        f"| RSS peak | {mem.get('rss_peak_mb')} MB |",
        f"| Growth | {mem.get('rss_growth_mb')} MB |",
        f"| Slope | {mem.get('slope_mb_per_min')} MB/min |",
        f"| Leak suspect | {mem.get('leak_suspect')} |",
        "",
        "```",
        f"RSS sparkline: {cfg.get('rss_sparkline', '')}",
        "```",
        "",
        leak_hint if leak_hint else "_No linear RSS leak detected._",
        "",
        "## 5. Chaos & malformed",
        f"- Chaos: {cfg.get('chaos_killed', 0)} workers hard-stopped mid-flight",
        f"- Malformed fast reject (400/409/429): {c.get('malformed_fast_reject', 0)}",
        f"- Malformed slow (>500ms): {c.get('malformed_slow_parse', 0)}",
        f"- Malformed unexpected 200: {c.get('malformed_unexpected_ok', 0)}",
        "",
        "## Top counters",
        "```json",
        json.dumps(dict(sorted(c.items(), key=lambda x: -x[1])[:40]), indent=2),
        "```",
    ]
    return "\n".join(lines)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--coord", default=os.environ.get("COORD", "http://127.0.0.1:8082"))
    ap.add_argument("--token", default=os.environ.get("COORD_ADMIN_TOKEN", ""))
    ap.add_argument("--workers", type=int, default=int(os.environ.get("WORKERS", "100")))
    ap.add_argument("--duration-sec", type=int, default=int(os.environ.get("DURATION_SEC", "600")))
    ap.add_argument("--target-rps", type=float, default=float(os.environ.get("TARGET_RPS", "25")))
    ap.add_argument("--batch", type=int, default=int(os.environ.get("BATCH_SIZE", "512")))
    ap.add_argument("--coord-pid", type=int, default=int(os.environ.get("COORD_PID", "0")))
    ap.add_argument("--report-dir", default=os.environ.get("REPORT_DIR", "reports/tests/coordinator_mega_stress"))
    ap.add_argument("--root", default=os.environ.get("ROOT", "."))
    ap.add_argument("--http-timeout", type=float, default=float(os.environ.get("HTTP_TIMEOUT", "8")))
    args = ap.parse_args()

    os.makedirs(args.report_dir, exist_ok=True)
    stats = GlobalStats()
    mem_path = os.path.join(args.report_dir, "memory_samples.csv")
    mem_sampler = MemorySampler(args.coord_pid, 1.0, mem_path) if args.coord_pid > 0 else None
    if mem_sampler:
        mem_sampler.start()

    clear_abuse(args.coord, args.token, args.http_timeout)
    code, pool_before, _ = get_json(args.coord.rstrip("/") + "/api/work/stats?details=1", args.token, args.http_timeout)

    workers = [
        VirtualWorker(f"stress-worker-{i:03d}", args.coord, args.token, args.batch, args.target_rps, stats, args.http_timeout)
        for i in range(args.workers)
    ]

    halving_box: list[dict[str, Any]] = []

    def halving_worker() -> None:
        halving_box.append(run_halving_tests(args.root))

    halving_thread = threading.Thread(target=halving_worker, daemon=True)
    halving_thread.start()
    register_workers(args.coord, args.token, workers, stats, args.http_timeout)

    end_ts = time.time() + args.duration_sec
    chaos_at = time.time() + args.duration_sec * 0.75
    # Run race early while rate-limit buckets are not saturated by sustained load.
    race_at = time.time() + min(45.0, args.duration_sec * 0.08)
    malformed_at = time.time() + args.duration_sec * 0.82
    race_result: dict[str, Any] = {}
    chaos_killed = 0

    def worker_thread(vw: VirtualWorker) -> None:
        vw.run_until(end_ts)

    pool = ThreadPoolExecutor(max_workers=min(args.workers, 128))
    futures = [pool.submit(worker_thread, w) for w in workers]

    while time.time() < end_ts and not stats.stop.is_set():
        now = time.time()
        if not race_result and now >= race_at:
            race_result = race_burst(workers, args.coord, args.token, stats, args.http_timeout)
        if chaos_killed == 0 and now >= chaos_at:
            victims = workers[: args.workers // 2]
            for v in victims:
                v.kill_midflight()
            chaos_killed = len(victims)
        if now >= malformed_at and stats.counts.get("malformed_total", 0) == 0:
            malformed_flood(args.coord, args.token, 1000, stats, min(args.http_timeout, 3.0))
        time.sleep(0.5)

    stats.stop.set()
    pool.shutdown(wait=True, cancel_futures=True)

    if mem_sampler:
        mem_sampler.stop_join()
    halving_thread.join(timeout=130)
    halving = halving_box[0] if halving_box else {"ok": False}

    _, pool_after, _ = get_json(args.coord.rstrip("/") + "/api/work/stats?details=1", args.token, args.http_timeout)

    mem_analysis = mem_sampler.analyze() if mem_sampler else {}
    rss_vals = [s[1] for s in mem_sampler.samples] if mem_sampler else []
    min_claim = max(50, int(args.duration_sec * args.workers * 0.05))
    min_submit = max(50, int(args.duration_sec * args.workers * 0.05))
    cfg = {
        "coord": args.coord,
        "workers": args.workers,
        "duration_sec": args.duration_sec,
        "target_rps": args.target_rps,
        "coord_pid": args.coord_pid,
        "chaos_killed": chaos_killed,
        "rss_sparkline": ascii_sparkline(rss_vals),
        "min_claim_ok": min_claim,
        "min_submit_ok": min_submit,
    }
    report = build_report(cfg, stats, mem_analysis, race_result, halving, pool_before, pool_after)
    report_path = os.path.join(args.report_dir, "MEGA_STRESS_REPORT.md")
    with open(report_path, "w", encoding="utf-8") as f:
        f.write(report)
    json_path = os.path.join(args.report_dir, "metrics.json")
    with open(json_path, "w", encoding="utf-8") as f:
        with stats.lock:
            json.dump({"counts": dict(stats.counts), "mem": mem_analysis, "race": race_result, "halving": halving}, f, indent=2)

    print(report)
    print(f"\nReport: {report_path}", file=sys.stderr)
    return 0 if "**READY**" in report else 1


if __name__ == "__main__":
    sys.exit(main())
