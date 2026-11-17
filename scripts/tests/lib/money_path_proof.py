#!/usr/bin/env python3
"""Validate money-path E2E proof artifacts for stable_readiness_gate.

Expects a directory (or HACKME_MONEY_PATH_PROOF path) containing one or more
VERDICT.json files from rc20_*_e2e scripts, each with pickup=true and no soft
pickup. Soft pickup is refused here regardless of ALLOW_SOFT_PICKUP.

Default proof dir: reports/gates/money_path_e2e/
"""
from __future__ import annotations

import json
import os
import sys
from pathlib import Path


REQUIRED_SUITES = ("pool", "hunt", "demo")


def _proof_root(root: Path) -> Path:
    env = (os.environ.get("HACKME_MONEY_PATH_PROOF") or "").strip()
    if env:
        return Path(env).expanduser()
    return root / "reports" / "gates" / "money_path_e2e"


def _load_verdicts(proof: Path) -> dict[str, dict]:
    out: dict[str, dict] = {}
    if not proof.is_dir():
        return out
    for suite in REQUIRED_SUITES:
        for name in (f"{suite}/VERDICT.json", f"VERDICT_{suite}.json", f"{suite}.VERDICT.json"):
            p = proof / name
            if p.is_file():
                out[suite] = json.loads(p.read_text())
                break
        # also accept flat VERDICT.json only for pool if that is the sole artifact
    if "pool" not in out:
        flat = proof / "VERDICT.json"
        if flat.is_file():
            out["pool"] = json.loads(flat.read_text())
    return out


def validate(root: Path | None = None) -> tuple[bool, str]:
    root = root or Path(__file__).resolve().parents[3]
    proof = _proof_root(root)
    if not proof.is_dir():
        return False, f"missing proof dir {proof} (run hard E2E; set HACKME_MONEY_PATH_PROOF)"
    soft = (os.environ.get("ALLOW_SOFT_PICKUP") or "0").strip()
    if soft == "1":
        return False, "ALLOW_SOFT_PICKUP=1 refused for money-path stable proof"
    verdicts = _load_verdicts(proof)
    missing = [s for s in REQUIRED_SUITES if s not in verdicts]
    if missing:
        return False, f"missing VERDICT for suites: {', '.join(missing)} under {proof}"
    for suite, v in verdicts.items():
        if not v.get("ok"):
            return False, f"{suite}: VERDICT ok!=true"
        if not v.get("pickup"):
            return False, f"{suite}: pickup!=true (hard fail; no soft pickup)"
        if v.get("refund_ok") is False:
            return False, f"{suite}: refund_ok=false"
        if v.get("soft_pickup"):
            return False, f"{suite}: soft_pickup marker present"
    return True, f"money-path proof OK ({', '.join(REQUIRED_SUITES)}) at {proof}"


def main() -> int:
    root = Path(__file__).resolve().parents[3]
    ok, detail = validate(root)
    print(("PASS: " if ok else "FAIL: ") + detail, flush=True)
    return 0 if ok else 1


if __name__ == "__main__":
    raise SystemExit(main())
