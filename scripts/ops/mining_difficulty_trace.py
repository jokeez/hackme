#!/usr/bin/env python3
"""Extract difficulty / economics time series from overnight snapshots or live APIs."""
from __future__ import annotations

import json
import sys
from pathlib import Path


def g(obj, *path, default=None):
    cur = obj
    for p in path:
        if not isinstance(cur, dict):
            return default
        cur = cur.get(p)
    return default if cur is None else cur


def pick_difficulty(row: dict) -> dict:
    work = g(row, "local", "work", default={}) or {}
    coord = g(row, "coordinator", "work", default={}) or work
    metrics = g(row, "local", "metrics", default={}) or {}
    st = g(row, "local", "status_lite", default={}) or {}
    ts = g(row, "ts", default="") or g(row, "ts_utc", default="") or ""
    return {
        "ts": ts,
        "epoch": g(row, "epoch", default=0),
        "target_mod": int(g(coord, "target_mod", default=0) or 0),
        "target_mod_updated_unix": int(g(coord, "target_mod_updated_unix", default=0) or 0),
        "pool_retarget_enabled": bool(g(coord, "pool_retarget_enabled", default=False)),
        "reward_per_m": float(g(coord, "reward_per_m", default=0) or 0),
        "found_bonus_hmc": float(g(coord, "found_bonus_hmc", default=0) or 0),
        "scheduler_mode": str(g(coord, "scheduler_mode", default="") or ""),
        "orders_active": bool(g(coord, "orders_active", default=False)),
        "pool_target_mod_status": int(g(st, "pool_target_mod", default=0) or 0),
        "mining_target_mod_metrics": int(g(metrics, "mining_target_mod", default=0) or 0),
        "pool_global_hashrate_th_s": float(g(st, "pool_global_hashrate_th_s", default=0) or 0),
        "accepted_attempts": int(g(coord, "accepted_attempts", default=0) or 0),
        "total_payout_hmc": float(g(coord, "total_payout_hmc", default=0) or 0),
    }


def load_rows(jsonl: Path, baseline: Path | None) -> list[dict]:
    raw_rows: list[dict] = []

    def add_raw(obj: dict) -> None:
        if obj:
            raw_rows.append(obj)

    if baseline and baseline.exists():
        try:
            raw = json.loads(baseline.read_text())
            if raw.get("local") or raw.get("coordinator"):
                add_raw(raw)
            elif raw.get("ts_utc"):
                add_raw(
                    {
                        "ts": raw.get("ts_utc"),
                        "local": raw.get("local", {}),
                        "coordinator": raw.get("coordinator", {}),
                    }
                )
        except json.JSONDecodeError:
            pass
    snap_path = jsonl.parent / "baseline_snapshot.json"
    if snap_path.exists():
        try:
            add_raw(json.loads(snap_path.read_text()))
        except json.JSONDecodeError:
            pass
    if jsonl.exists():
        for line in jsonl.read_text().splitlines():
            line = line.strip()
            if not line:
                continue
            try:
                add_raw(json.loads(line))
            except json.JSONDecodeError:
                continue
    diff_jsonl = jsonl.parent / "difficulty.jsonl"
    if diff_jsonl.exists():
        for line in diff_jsonl.read_text().splitlines():
            line = line.strip()
            if not line:
                continue
            try:
                obj = json.loads(line)
                if "target_mod" in obj:
                    raw_rows.append({"ts": obj.get("ts"), **obj})
            except json.JSONDecodeError:
                continue
    return raw_rows


def analyze(series: list[dict]) -> dict:
    if not series:
        return {"samples": 0}
    mods = [s["target_mod"] for s in series if s.get("target_mod", 0) > 0]
    rpms = [s["reward_per_m"] for s in series if s.get("reward_per_m", 0) > 0]
    changes: list[dict] = []
    prev_m = None
    for s in series:
        m = s.get("target_mod") or 0
        if m <= 0:
            continue
        if prev_m is not None and m != prev_m:
            changes.append(
                {
                    "ts": s.get("ts"),
                    "from": prev_m,
                    "to": m,
                    "pct": round(100.0 * (m - prev_m) / prev_m, 2) if prev_m else None,
                }
            )
        prev_m = m
    return {
        "samples": len(series),
        "first_ts": series[0].get("ts"),
        "last_ts": series[-1].get("ts"),
        "target_mod": {
            "first": mods[0] if mods else None,
            "last": mods[-1] if mods else None,
            "min": min(mods) if mods else None,
            "max": max(mods) if mods else None,
            "delta": (mods[-1] - mods[0]) if len(mods) >= 2 else None,
            "retarget_events": len(changes),
            "changes": changes,
        },
        "reward_per_m": {
            "first": rpms[0] if rpms else None,
            "last": rpms[-1] if rpms else None,
            "min": min(rpms) if rpms else None,
            "max": max(rpms) if rpms else None,
        },
        "series": series,
    }


def write_report(out_dir: Path, report: dict) -> None:
    out_dir.mkdir(parents=True, exist_ok=True)
    series = report.pop("series", [])
    (out_dir / "difficulty_timeseries.json").write_text(json.dumps(series, indent=2))
    (out_dir / "difficulty_report.json").write_text(json.dumps(report, indent=2))

    tm = report.get("target_mod") or {}
    rpm = report.get("reward_per_m") or {}
    lines = [
        f"# Difficulty / economics trace",
        "",
        f"- **samples:** {report.get('samples', 0)}",
        f"- **from:** {report.get('first_ts', '—')} → **to:** {report.get('last_ts', '—')}",
        "",
        "## Pool target_mod (M)",
        f"- start: **{tm.get('first', '—'):,}**" if isinstance(tm.get("first"), int) else "- start: —",
        f"- end: **{tm.get('last', '—'):,}**" if isinstance(tm.get("last"), int) else "- end: —",
        f"- min / max: {tm.get('min', '—'):,} / {tm.get('max', '—'):,}" if tm.get("min") else "- min / max: —",
        f"- delta: {tm.get('delta', '—'):+,}" if tm.get("delta") is not None else "",
        f"- **retarget events:** {tm.get('retarget_events', 0)}",
        "",
    ]
    for ch in tm.get("changes") or []:
        lines.append(f"  - {ch.get('ts')}: {ch.get('from'):,} → {ch.get('to'):,} ({ch.get('pct')}%)")
    lines += [
        "",
        "## reward/M",
        f"- start: {rpm.get('first', '—')}",
        f"- end: {rpm.get('last', '—')}",
        f"- min / max: {rpm.get('min', '—')} / {rpm.get('max', '—')}",
        "",
        "Full series: `difficulty_timeseries.json`",
    ]
    (out_dir / "DIFFICULTY.md").write_text("\n".join(lines) + "\n")


def main() -> None:
    if len(sys.argv) < 2:
        print("usage: mining_difficulty_trace.py OUT_DIR [snapshots.jsonl] [baseline.json]", file=sys.stderr)
        sys.exit(1)
    out_dir = Path(sys.argv[1])
    jsonl = Path(sys.argv[2]) if len(sys.argv) > 2 else out_dir / "snapshots.jsonl"
    baseline = Path(sys.argv[3]) if len(sys.argv) > 3 else out_dir / "baseline.json"
    rows = load_rows(jsonl, baseline)
    series = []
    seen_epoch: set[int] = set()
    for r in rows:
        if "target_mod" in r and "reward_per_m" in r and not r.get("local"):
            s = r
        else:
            s = pick_difficulty(r)
        ep = int(s.get("epoch") or 0)
        key = ep if ep > 0 else hash(s.get("ts") or "")
        if key in seen_epoch:
            continue
        seen_epoch.add(key)
        series.append(s)
    series.sort(key=lambda x: int(x.get("epoch") or 0))
    # dedupe consecutive identical samples (optional keep all for graph)
    report = analyze(series)
    write_report(out_dir, report)
    print(json.dumps({k: v for k, v in report.items() if k != "series"}, indent=2))


if __name__ == "__main__":
    main()
