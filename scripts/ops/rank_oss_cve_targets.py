#!/usr/bin/env python3
"""Rank OSS CVE targets by likelihood of ASAN-class findings.

Scans reports/oss-cve/*/HUNT_REPORT.json + ROLLUP.json and upstream/oss_cve_targets.json.
Writes upstream/oss_cve_high_yield.json and a human-readable report.

  python3 scripts/ops/rank_oss_cve_targets.py
  python3 scripts/ops/rank_oss_cve_targets.py --top 12 --out reports/oss-cve/cve-rank-latest.md
"""
from __future__ import annotations

import argparse
import json
import pathlib
import re
from collections import defaultdict
from datetime import datetime, timezone

ROOT = pathlib.Path(__file__).resolve().parents[2]
MANIFEST = ROOT / "upstream" / "oss_cve_targets.json"
REPORTS = ROOT / "reports" / "oss-cve"
OUT_JSON = ROOT / "upstream" / "oss_cve_high_yield.json"

# Higher = more CVE surface historically (not already saturated in waves 10–13).
CATEGORY_BOOST = {
    "interpreter": 45,
    "compression": 40,
    "regex": 40,
    "xml": 35,
    "binary": 35,
    "msgpack": 35,
    "http": 30,
    "config": 10,
    "json": 0,
    "markdown": 5,
    "yaml": 5,
    "toml": 5,
    "ini": 0,
}

WAVE_SATURATED_CLEAN = {
    # wave12–13 CLEAN at ≥50k iters — do not re-queue in wave14+
    "expat", "yajl", "json-c", "cmp", "uriparser", "picohttpparser", "pcre2",
    "md4c", "mjson", "yyjson", "parson", "jansson", "sheredom", "cyaml",
    "tomlc17", "libyaml", "cmark",
    # wave23–24 verified CLEAN
    "miniz", "oniguruma", "zlib", "libxml2", "mpack", "cjson", "libcbor", "heatshrink",
}

# wave14 main run — CLEAN at ≥130k iters
WAVE14_CLEAN_SATURATED = {"miniz", "oniguruma", "zlib", "libxml2"}

# wave14 INFORMATIONAL — deprioritize in wave15 (asan-chase handles separately)
WAVE14_INFORMATIONAL = {"libucl", "cfgpack", "nghttp2", "duktape"}

# Always chase — prior CVE_CANDIDATE (UBSan) needs deep ASAN budget
BUILD_SKIP = {"msgpack-c"}  # needs cmake-generated pack_template.h — fix in build.go later

PIN_WAVE14 = ["libucl", "cfgpack", "nghttp2", "duktape"]

# wave15 — easy high-yield: never fuzzed / under-tested parsers & interpreters
PIN_WAVE15 = [
    "pcre2",
    "lua",
    "quickjs",
    "cmark",
    "json-c",
    "picohttpparser",
    "cmp",
    "uriparser",
]

# wave27 — obscure / low OSS-Fuzz coverage (see run_oss_cve_obscure.sh)
PIN_WAVE27 = [
    "frozen",
    "mpack",
    "libcbor",
    "kuba_zip",
    "cwalk",
    "jsonparser",
    "tinycbor",
    "microtar",
]

# wave30 — new GitHub targets (lz4, hiredis, wren, minimp3, stb)
PIN_WAVE30 = [
    "lz4",
    "hiredis",
    "wren",
    "minimp3",
    "stb_vorbis",
]

HOLD_DEEP = {"centijson"}  # disclosure hold — skip automated waves

DISCLOSURE_HOLD = {"centijson"}

# Harness/driver false positives — never score as CVE_CANDIDATE
DRIVER_FALSE_POSITIVE = {"heatshrink"}

TARGET_CATEGORY = {
    "lua": "interpreter",
    "quickjs": "interpreter",
    "duktape": "interpreter",
    "heatshrink": "compression",
    "miniz": "compression",
    "zlib": "compression",
    "lz4": "compression",
    "minimp3": "binary",
    "stb_vorbis": "binary",
    "hiredis": "http",
    "wren": "interpreter",
    "oniguruma": "regex",
    "pcre2": "regex",
    "libxml2": "xml",
    "expat": "xml",
    "mxml": "xml",
    "nghttp2": "http",
    "picohttpparser": "http",
    "cfgpack": "msgpack",
    "cmp": "msgpack",
    "mpack": "msgpack",
    "msgpack-c": "msgpack",
    "libcbor": "cbor",
    "tinycbor": "cbor",
    "frozen": "json",
    "jsonparser": "json",
    "kuba_zip": "archive",
    "cwalk": "path",
    "sqids": "codec",
    "microtar": "archive",
    "libucl": "config",
}


def load_manifest() -> dict:
    return json.loads(MANIFEST.read_text())


OSS_DRIVER_DIR = ROOT / "tasks" / "sources" / "fuzz" / "oss"


def target_has_driver(tid: str, manifest_by_id: dict[str, dict]) -> bool:
    """True when tasks/sources/fuzz/oss/<driver>.c exists for this manifest entry."""
    meta = manifest_by_id.get(tid) or {}
    driver = (meta.get("driver") or "").strip()
    if not driver:
        return False
    return (OSS_DRIVER_DIR / f"{driver}.c").is_file()


def filter_drivable(ids: list[str], manifest_by_id: dict[str, dict]) -> list[str]:
    return [i for i in ids if target_has_driver(i, manifest_by_id)]


def scan_reports() -> dict[str, dict]:
    """Latest stats per target_id from all hunt reports."""
    stats: dict[str, dict] = {}
    for path in sorted(REPORTS.glob("**/HUNT_REPORT.json")):
        try:
            r = json.loads(path.read_text())
        except Exception:
            continue
        tid = r.get("target_id") or path.parent.name
        prev = stats.get(tid, {})
        iters = int(r.get("iterations") or 0)
        crashes = r.get("crashes") or []
        asan = sum(
            1
            for c in crashes
            if "AddressSanitizer" in (c.get("sanitizer") or "")
            or "heap-buffer-overflow" in (c.get("sanitizer") or "")
            or "use-after-free" in (c.get("sanitizer") or "")
        )
        ubsan = len(crashes) - asan
        if iters >= int(prev.get("iterations") or 0):
            stats[tid] = {
                "verdict": r.get("verdict"),
                "iterations": iters,
                "crash_count": len(crashes),
                "asan_crashes": asan,
                "ubsan_crashes": ubsan,
                "report": str(path.relative_to(ROOT)),
                "stamp": path.parts[-3] if len(path.parts) >= 3 else "",
            }
    for tid in DRIVER_FALSE_POSITIVE:
        stats[tid] = {
            "verdict": "CLEAN",
            "iterations": max(int((stats.get(tid) or {}).get("iterations") or 0), 50_000),
            "crash_count": 0,
            "asan_crashes": 0,
            "ubsan_crashes": 0,
            "report": "driver_false_positive",
            "stamp": "heatshrink-verify50k",
        }
    return stats


def score_target(tid: str, meta: dict, rep: dict | None) -> tuple[int, list[str]]:
    reasons: list[str] = []
    score = 0
    cat = TARGET_CATEGORY.get(tid, "json")
    boost = CATEGORY_BOOST.get(cat, 0)
    if boost:
        score += boost
        reasons.append(f"category={cat}(+{boost})")

    if rep is None:
        score += 120
        reasons.append("never fuzzed(+120)")
    else:
        v = rep.get("verdict")
        it = int(rep.get("iterations") or 0)
        cc = int(rep.get("crash_count") or 0)
        asan = int(rep.get("asan_crashes") or 0)
        if v == "CVE_CANDIDATE":
            score += 90
            reasons.append("CVE_CANDIDATE(+90)")
        elif v == "INFORMATIONAL":
            score += 50 + min(30, cc // 100)
            reasons.append(f"INFORMATIONAL crashes={cc}(+{50 + min(30, cc // 100)})")
        elif v == "CLEAN":
            if it < 60_000:
                score += 25
                reasons.append(f"under-tested iters={it}(+25)")
            elif it >= 150_000:
                score -= 80
                reasons.append(f"saturated iters={it}(-80)")
            else:
                score -= 30
                reasons.append(f"clean mid-budget iters={it}(-30)")
        if asan > 0:
            score += 100
            reasons.append(f"asan_hits={asan}(+100)")

    pri = int(meta.get("priority") or 3)
    if pri == 1:
        score += 10
        reasons.append("priority=1(+10)")

    if tid in WAVE_SATURATED_CLEAN and (rep or {}).get("verdict") == "CLEAN":
        score -= 200
        reasons.append("wave12-13_clean_saturated(-200)")

    if tid in PIN_WAVE14:
        score += 75
        reasons.append("pinned_cve_chase(+75)")

    hold = DISCLOSURE_HOLD
    if tid in hold:
        score -= 40
        reasons.append("disclosure_hold(-40)")

    return score, reasons


def wave_exclude_base() -> set[str]:
    return set(BUILD_SKIP) | HOLD_DEEP | DISCLOSURE_HOLD


def build_wave_queue(
    wave_num: int,
    top: int,
    ranked: list[dict],
    manifest_by_id: dict[str, dict],
) -> tuple[list[str], dict]:
    """Return (target_ids, wave_meta) for a given wave number."""
    exclude = wave_exclude_base()

    if wave_num == 14:
        exclude |= WAVE_SATURATED_CLEAN
        hold_deep_active = {"centijson", "lua", "quickjs", "mxml"}
        exclude |= hold_deep_active
        slots = max(0, top - len(PIN_WAVE14))
        extra = [r["id"] for r in ranked if r["id"] not in exclude and r["id"] not in PIN_WAVE14][:slots]
        ids = [i for i in PIN_WAVE14 + extra if i not in exclude]
        return ids, {
            "targets": ids,
            "budget_iterations": 250000,
            "time_limit_sec": 7200,
            "skip_ids": sorted(exclude),
            "strategy": "high_yield_asan",
        }

    if wave_num == 15:
        exclude |= WAVE14_CLEAN_SATURATED | WAVE14_INFORMATIONAL
        # PIN targets bypass wave12-13 saturation — we want fresh deep runs on easy parsers
        pin = [i for i in PIN_WAVE15 if i not in wave_exclude_base()]
        slots = max(0, top - len(pin))
        extra_exclude = exclude | WAVE_SATURATED_CLEAN | set(pin)
        extra = [r["id"] for r in ranked if r["id"] not in extra_exclude][:slots]
        ids = pin + extra
        return ids, {
            "targets": ids,
            "budget_iterations": 180000,
            "time_limit_sec": 5400,
            "skip_ids": sorted(exclude),
            "strategy": "easy_never_fuzzed_parsers",
        }

    if wave_num == 27:
        ids = filter_drivable([i for i in PIN_WAVE27 if i in manifest_by_id and i not in exclude], manifest_by_id)
        return ids, {
            "targets": ids,
            "budget_iterations": 300000,
            "time_limit_sec": 7200,
            "skip_ids": sorted(exclude),
            "strategy": "obscure_low_coverage",
        }

    if wave_num == 30:
        ids = filter_drivable([i for i in PIN_WAVE30 if i in manifest_by_id and i not in exclude], manifest_by_id)
        return ids, {
            "targets": ids,
            "budget_iterations": 200000,
            "time_limit_sec": 5400,
            "skip_ids": sorted(exclude),
            "strategy": "new_github_targets",
        }

    if wave_num >= 16:
        exclude |= (
            WAVE_SATURATED_CLEAN
            | WAVE14_CLEAN_SATURATED
            | WAVE14_INFORMATIONAL
            | set(PIN_WAVE14)
            | set(PIN_WAVE15)
        )
        # Prefer never fuzzed, then under-tested INFORMATIONAL, then low-iter CLEAN
        never = filter_drivable(
            [r["id"] for r in ranked if r.get("last_verdict") is None and r["id"] not in exclude],
            manifest_by_id,
        )
        under = filter_drivable(
            [
                r["id"]
                for r in ranked
                if r.get("last_verdict") == "INFORMATIONAL" and r["id"] not in exclude
            ],
            manifest_by_id,
        )
        rest = filter_drivable(
            [
                r["id"]
                for r in ranked
                if r["id"] not in exclude and r["id"] not in never and r["id"] not in under
            ],
            manifest_by_id,
        )
        ids = (never + under + rest)[:top]
        return ids, {
            "targets": ids,
            "budget_iterations": 100000,
            "time_limit_sec": 3600,
            "skip_ids": sorted(exclude),
            "strategy": "sweep_remaining_easy",
        }

    raise ValueError(f"unsupported wave {wave_num}")


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--top", type=int, default=10)
    ap.add_argument("--wave", type=int, default=14, help="Wave number for queue (14, 15, 16, …)")
    ap.add_argument("--out", type=str, default="")
    args = ap.parse_args()

    manifest = load_manifest()
    report_stats = scan_reports()
    all_ids = {t["id"]: t for t in manifest.get("targets", [])}

    ranked = []
    for tid, meta in all_ids.items():
        if tid in BUILD_SKIP:
            continue
        rep = report_stats.get(tid)
        sc, reasons = score_target(tid, meta, rep)
        ranked.append(
            {
                "id": tid,
                "score": sc,
                "repo": meta.get("repo"),
                "title": meta.get("title"),
                "priority": meta.get("priority"),
                "category": TARGET_CATEGORY.get(tid, "json"),
                "last_verdict": (rep or {}).get("verdict"),
                "last_iterations": (rep or {}).get("iterations"),
                "crash_count": (rep or {}).get("crash_count"),
                "asan_crashes": (rep or {}).get("asan_crashes"),
                "reasons": reasons,
            }
        )
    ranked.sort(key=lambda x: (-x["score"], x["id"]))

    wave_ids, wave_meta = build_wave_queue(args.wave, args.top, ranked, all_ids)
    wave_key = f"wave{args.wave}"

    # Merge into existing JSON so wave14/15/16 queues coexist
    out_doc: dict = {}
    if OUT_JSON.is_file():
        try:
            out_doc = json.loads(OUT_JSON.read_text())
        except Exception:
            out_doc = {}
    out_doc.update(
        {
            "updated": datetime.now(timezone.utc).strftime("%Y-%m-%d"),
            "note": "Auto-ranked high-yield OSS CVE targets — ASAN-class hunt queue (wave14+)",
            "method": "rank_oss_cve_targets.py",
            "ranked": ranked,
        }
    )
    out_doc[wave_key] = wave_meta
    OUT_JSON.write_text(json.dumps(out_doc, indent=2) + "\n")

    never = [r["id"] for r in ranked if r.get("last_verdict") is None]
    lines = [
        "# OSS CVE target ranking",
        "",
        f"Generated: {out_doc['updated']}",
        "",
        f"## Wave{args.wave} queue ({wave_meta.get('strategy')}, n={len(wave_ids)})",
        "",
        f"Targets: `{','.join(wave_ids)}`",
        "",
        f"Never fuzzed ({len(never)}): {', '.join(never[:20])}{'…' if len(never) > 20 else ''}",
        "",
        "| Rank | Target | Score | Last | Iters | ASAN | Why |",
        "|------|--------|------:|------|------:|-----:|-----|",
    ]
    for i, r in enumerate(ranked[: args.top + 8], 1):
        mark = "**" if r["id"] in wave_ids else ""
        lines.append(
            f"| {i} | {mark}{r['id']}{mark} | {r['score']} | {r.get('last_verdict') or '—'} | "
            f"{r.get('last_iterations') or 0} | {r.get('asan_crashes') or 0} | "
            f"{'; '.join(r['reasons'][:3])} |"
        )

    md = "\n".join(lines) + "\n"
    report_path = pathlib.Path(args.out) if args.out else REPORTS / "cve-rank-latest.md"
    report_path.parent.mkdir(parents=True, exist_ok=True)
    report_path.write_text(md)

    print(f"wrote {OUT_JSON}")
    print(f"wrote {report_path}")
    print(f"{wave_key}:", ",".join(wave_ids))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
