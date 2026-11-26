#!/usr/bin/env python3
"""Shared fail-closed miner-pickup gate for rc20+ money-path E2E scripts.

Default: missing pickup → exit 3.
Override: ALLOW_SOFT_PICKUP=1 AND HACKME_SOFT_PICKUP_REASON (>=8 chars).
Hard refuse override when claiming a stable cut (HACKME_CLAIM_STABLE=1 or
VERSION looks like X.Y.Z with no prerelease suffix).
"""
from __future__ import annotations

import os
import re
import sys


_STABLE_VER = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+$")


def _version_claims_stable() -> bool:
    if os.environ.get("HACKME_CLAIM_STABLE", "0").strip() == "1":
        return True
    ver = (
        os.environ.get("VERSION")
        or os.environ.get("HACKME_VERSION")
        or ""
    ).strip()
    return bool(ver) and bool(_STABLE_VER.match(ver))


def soft_pickup_override_allowed() -> tuple[bool, str]:
    """Return (ok_to_warn, detail). ok_to_warn means missing pickup may WARN instead of FAIL."""
    if os.environ.get("ALLOW_SOFT_PICKUP", "0").strip() != "1":
        return False, "ALLOW_SOFT_PICKUP not set"
    if _version_claims_stable():
        return False, "soft pickup refused while claiming stable (HACKME_CLAIM_STABLE=1 or VERSION=X.Y.Z)"
    reason = os.environ.get("HACKME_SOFT_PICKUP_REASON", "").strip()
    if len(reason) < 8:
        return False, "ALLOW_SOFT_PICKUP=1 requires HACKME_SOFT_PICKUP_REASON (>=8 chars)"
    return True, reason


def exit_on_missing_pickup(pickup: bool, what: str = "miner pickup") -> None:
    if pickup:
        return
    ok, detail = soft_pickup_override_allowed()
    if ok:
        print(f"WARN: no {what} — ALLOW_SOFT_PICKUP=1 ({detail})", flush=True)
        return
    if os.environ.get("ALLOW_SOFT_PICKUP", "0").strip() == "1":
        print(f"FAIL: soft pickup misconfigured: {detail}", flush=True)
        sys.exit(4)
    print(
        f"FAIL: no {what} in window "
        "(ops triage only: ALLOW_SOFT_PICKUP=1 + HACKME_SOFT_PICKUP_REASON=…)",
        flush=True,
    )
    sys.exit(3)


if __name__ == "__main__":
    # Self-check: default refuses; reason required; stable refuses.
    os.environ.pop("ALLOW_SOFT_PICKUP", None)
    os.environ.pop("HACKME_SOFT_PICKUP_REASON", None)
    os.environ.pop("HACKME_CLAIM_STABLE", None)
    os.environ.pop("VERSION", None)
    ok, _ = soft_pickup_override_allowed()
    assert not ok, "default must refuse"
    os.environ["ALLOW_SOFT_PICKUP"] = "1"
    ok, detail = soft_pickup_override_allowed()
    assert not ok and "REASON" in detail, detail
    os.environ["HACKME_SOFT_PICKUP_REASON"] = "ops triage window"
    ok, _ = soft_pickup_override_allowed()
    assert ok, "reason + ALLOW should permit"
    os.environ["VERSION"] = "0.1.0"
    ok, detail = soft_pickup_override_allowed()
    assert not ok and "stable" in detail, detail
    os.environ["VERSION"] = "0.1.0-rc99"  # any -rc value; synthetic so it names no real cut
    ok, _ = soft_pickup_override_allowed()
    assert ok, "rc versions may soft-override with reason"
    print("soft_pickup_gate self-check PASS", flush=True)
