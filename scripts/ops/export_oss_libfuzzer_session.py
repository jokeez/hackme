#!/usr/bin/env python3
"""Summarize a libFuzzer session log + crash dir into SESSION.json (ROLLUP-compatible)."""
from __future__ import annotations

import json
import re
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path


def classify_crash(bin_path: Path, crash_file: Path) -> tuple[str, str]:
    try:
        r = subprocess.run(
            [str(bin_path), str(crash_file)],
            capture_output=True,
            text=True,
            timeout=10,
            env={
                "PATH": "/usr/bin:/bin",
                "ASAN_OPTIONS": "detect_leaks=0:halt_on_error=1:allocator_may_return_null=1",
                "UBSAN_OPTIONS": "halt_on_error=1",
                "HOME": "/tmp",
            },
        )
    except subprocess.TimeoutExpired:
        return "timeout", ""
    blob = (r.stdout or "") + (r.stderr or "")
    tail = blob[-800:].strip()
    for sig in (
        "heap-buffer-overflow",
        "stack-buffer-overflow",
        "use-after-free",
        "double-free",
        "SUMMARY: AddressSanitizer",
        "SEGV on unknown address",
    ):
        if sig in blob:
            return sig, tail
    if "SEGV on unknown address" in blob or "DEADLYSIGNAL" in blob:
        return "SEGV on unknown address", tail
    if "UndefinedBehaviorSanitizer" in blob or "runtime error:" in blob:
        return "UndefinedBehaviorSanitizer", tail
    if r.returncode != 0:
        return "signal", tail
    return "", tail


def parse_log(text: str) -> dict:
    stats = {
        "executions": 0,
        "exec_per_sec": 0.0,
        "corpus_count": 0,
        "corpus_bytes": 0,
        "coverage_edges": 0,
        "features": 0,
        "elapsed_sec": 0.0,
    }
    m_done = re.search(r"Done (\d+) runs in (\d+(?:\.\d+)?) second", text)
    if m_done:
        stats["executions"] = int(m_done.group(1))
        stats["elapsed_sec"] = float(m_done.group(2))
    m_units = re.search(r"stat::number_of_executed_units:\s*(\d+)", text)
    if m_units:
        stats["executions"] = max(stats["executions"], int(m_units.group(1)))
    m_eps = re.search(r"stat::average_exec_per_sec:\s*([\d.]+)", text)
    if m_eps:
        stats["exec_per_sec"] = float(m_eps.group(1))
    m_new = re.search(r"stat::new_units_added:\s*(\d+)", text)
    if m_new:
        stats["corpus_count"] = max(stats["corpus_count"], int(m_new.group(1)))
    for line in text.splitlines():
        m = re.search(
            r"#(\d+)\s+.*?cov:\s*(\d+)\s+ft:\s*(\d+)\s+corp:\s*(\d+)/(\d+)([KMG]?[bB]?)\s+.*?exec/s:\s*([\d.]+)",
            line,
        )
        if m:
            stats["executions"] = max(stats["executions"], int(m.group(1)))
            stats["coverage_edges"] = int(m.group(2))
            stats["features"] = int(m.group(3))
            stats["corpus_count"] = int(m.group(4))
            size = int(m.group(5))
            unit = m.group(6)
            unit_key = unit.rstrip("bB") or "b"
            mult = {"": 1, "b": 1, "K": 1024, "M": 1024**2, "G": 1024**3}.get(unit_key, 1)
            stats["corpus_bytes"] = size * mult
            stats["exec_per_sec"] = float(m.group(7))
    if stats["executions"] > 0 and stats["exec_per_sec"] > 0 and stats["elapsed_sec"] <= 0:
        stats["elapsed_sec"] = stats["executions"] / stats["exec_per_sec"]
    return stats


def main() -> None:
    if len(sys.argv) < 4:
        print("usage: export_oss_libfuzzer_session.py TARGET SESSION_DIR FUZZER_BIN", file=sys.stderr)
        sys.exit(2)
    target, session_dir, bin_path = sys.argv[1], Path(sys.argv[2]), Path(sys.argv[3])
    log_path = session_dir / "fuzzer.log"
    if not log_path.is_file():
        sys.exit(f"missing {log_path}")
    log_text = log_path.read_text(errors="replace")
    stats = parse_log(log_text)
    crash_dir = session_dir / "crashes"
    crashes = []
    asan_n = ubsan_n = 0
    if crash_dir.is_dir():
        for cf in sorted(crash_dir.glob("crash-*"))[:200]:
            san, tail = classify_crash(bin_path, cf)
            if not san:
                continue
            entry = {
                "artifact": str(cf),
                "sanitizer": san,
                "input_len": cf.stat().st_size,
                "tail": tail[:500],
            }
            crashes.append(entry)
            if "AddressSanitizer" in san or "heap-buffer" in san or "use-after-free" in san:
                asan_n += 1
            elif "UndefinedBehavior" in san or "runtime error" in san:
                ubsan_n += 1
    if asan_n > 0:
        verdict = "CVE_CANDIDATE"
        summary = "HOLD — ASAN-class crash in libFuzzer session. Responsible disclosure before publish."
    elif ubsan_n > 0 or crashes:
        verdict = "INFORMATIONAL"
        summary = "INFORMATIONAL — UBSan-only or non-exploit signals in libFuzzer session."
    else:
        verdict = "CLEAN"
        summary = "CLEAN — no ASAN crash in libFuzzer session budget."
    started = ""
    if len(session_dir.name) >= 16 and session_dir.name[8] == "T":
        started = (
            f"{session_dir.name[0:4]}-{session_dir.name[4:6]}-{session_dir.name[6:8]}"
            f"T{session_dir.name[9:11]}:{session_dir.name[11:13]}:{session_dir.name[13:15]}Z"
        )
    out = {
        "engine": "libfuzzer",
        "target_id": target,
        "title": f"{target} · libFuzzer ASAN",
        "started_at": started or datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "finished_at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "verdict": verdict,
        "summary": summary,
        "iterations": stats["executions"],
        "elapsed_sec": stats["elapsed_sec"],
        "exec_per_sec": stats["exec_per_sec"],
        "corpus_count": stats["corpus_count"],
        "corpus_bytes": stats["corpus_bytes"],
        "coverage_edges": stats["coverage_edges"],
        "features": stats["features"],
        "asan_crashes": asan_n,
        "ubsan_crashes": ubsan_n,
        "crashes": crashes,
        "fuzzer_bin": str(bin_path),
        "session_dir": str(session_dir),
    }
    m0 = re.search(r"^(\d{4}-\d{2}-\d{2}T\S+)", log_text, re.M)
    if m0:
        out["started_at"] = m0.group(1)
    (session_dir / "SESSION.json").write_text(json.dumps(out, indent=2) + "\n")
    rollup = {
        "engine": "libfuzzer",
        "verdict": verdict,
        "summary": summary,
        "cve_candidates": [target] if verdict == "CVE_CANDIDATE" else [],
        "informational_targets": [target] if verdict == "INFORMATIONAL" else [],
        "clean_targets": [target] if verdict == "CLEAN" else [],
        "started_at": out["started_at"],
        "finished_at": out["finished_at"],
        "targets": [
            {
                "target_id": target,
                "verdict": verdict,
                "iterations": stats["executions"],
                "elapsed_sec": out["elapsed_sec"],
                "crash_count": len(crashes),
                "asan_crashes": asan_n,
                "crashes": crashes[:50],
            }
        ],
    }
    (session_dir / "ROLLUP.json").write_text(json.dumps(rollup, indent=2) + "\n")
    print(json.dumps({"verdict": verdict, "executions": stats["executions"], "asan": asan_n, "ubsan": ubsan_n}, indent=2))


if __name__ == "__main__":
    main()
