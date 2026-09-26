#!/usr/bin/env python3
"""Roll up reports/hunt-daily/YYYYMMDD/*/hunt-local.json into ROLLUP.{json,md}."""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path


def load_slots(day_dir: Path) -> list[dict]:
    rows = []
    if not day_dir.is_dir():
        return rows
    for slot in sorted(day_dir.iterdir()):
        if not slot.is_dir():
            continue
        hunt_p = slot / "hunt-local.json"
        meta_p = slot / "meta.json"
        meta = json.loads(meta_p.read_text()) if meta_p.is_file() else {}
        hunt = json.loads(hunt_p.read_text()) if hunt_p.is_file() else {}
        fam = hunt.get("finding_families") or {}
        name = slot.name  # HH00-target
        hour, _, target = name.partition("-")
        rows.append(
            {
                "slot": name,
                "hour_utc": hour[:2] if hour else meta.get("hour"),
                "target": hunt.get("target") or target or meta.get("target"),
                "verdict": hunt.get("verdict") or meta.get("verdict"),
                "iterations": hunt.get("iterations") or meta.get("iterations") or 0,
                "crashes": hunt.get("crashes") or meta.get("crashes") or 0,
                "exec_per_sec": hunt.get("exec_per_sec") or meta.get("exec_per_sec") or 0,
                "unique_signatures": hunt.get("unique_signatures") or meta.get("unique_signatures") or 0,
                "family_count": fam.get("family_count") or meta.get("family_count") or 0,
                "ok": meta.get("ok", hunt_p.is_file()),
                "error": hunt.get("error"),
                "sanitizer_signatures": hunt.get("sanitizer_signatures") or {},
            }
        )
    return rows


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", default=os.environ.get("HACKME_REPO_ROOT") or ".")
    ap.add_argument("--day", default="", help="YYYYMMDD UTC (default today)")
    args = ap.parse_args()
    root = Path(args.repo).resolve()
    day = args.day or __import__("datetime").datetime.utcnow().strftime("%Y%m%d")
    day_dir = root / "reports" / "hunt-daily" / day
    day_dir.mkdir(parents=True, exist_ok=True)
    rows = load_slots(day_dir)

    total_iters = sum(int(r.get("iterations") or 0) for r in rows)
    total_crashes = sum(int(r.get("crashes") or 0) for r in rows)
    total_families = sum(int(r.get("family_count") or 0) for r in rows)
    by_verdict: dict[str, int] = {}
    for r in rows:
        v = str(r.get("verdict") or "NONE")
        by_verdict[v] = by_verdict.get(v, 0) + 1
    # union of sanitizer subtype keys across slots
    sig_keys: set[str] = set()
    for r in rows:
        sig_keys.update((r.get("sanitizer_signatures") or {}).keys())

    doc = {
        "day": day,
        "honesty_version": "2.0",
        "slots_done": len(rows),
        "slots_planned": 24,
        "total_iterations": total_iters,
        "total_crash_artifacts": total_crashes,
        "total_finding_families": total_families,
        "unique_sanitizer_signatures_union": sorted(sig_keys),
        "by_verdict": by_verdict,
        "honesty_note": "Cite finding families / signatures, not raw crash artifact counts.",
        "rows": rows,
    }
    (day_dir / "ROLLUP.json").write_text(json.dumps(doc, indent=2) + "\n")

    lines = [
        f"# Hunt daily rollup — {day}",
        "",
        f"Slots **{doc['slots_done']}/24** · iters **{total_iters}** · crash artifacts **{total_crashes}** · "
        f"family sum **{total_families}** · signature union **{len(sig_keys)}**",
        "",
        "> Cite families / signatures, not raw crashes (honesty 2.0).",
        "",
        "| hour | target | verdict | iters | crashes | fam | eps |",
        "|------|--------|---------|------:|--------:|----:|----:|",
    ]
    for r in rows:
        lines.append(
            f"| {r.get('hour_utc')} | `{r.get('target')}` | {r.get('verdict')} | "
            f"{r.get('iterations')} | {r.get('crashes')} | {r.get('family_count')} | "
            f"{float(r.get('exec_per_sec') or 0):.1f} |"
        )
    if not rows:
        lines.append("| — | — | no slots yet | — | — | — | — |")
    lines += ["", f"Verdicts: `{json.dumps(by_verdict)}`", ""]
    (day_dir / "ROLLUP.md").write_text("\n".join(lines) + "\n")
    print(f"wrote {day_dir}/ROLLUP.md ({len(rows)} slots)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
