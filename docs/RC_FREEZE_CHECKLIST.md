# RC Freeze Checklist

This checklist defines the minimum gate before declaring a Release Candidate (RC) freeze.

## Goal

Lock feature work, validate operational/security baseline, and produce a single PASS/FAIL artifact for go/no-go.

## 1) Feature Freeze

- Stop new features on mainline.
- Allow only:
  - bug fixes,
  - test fixes,
  - release/docs/ops corrections.

## 2) Run Unified RC Gate

```bash
ADMIN_TOKEN='<admin-token>' \
BASE='http://127.0.0.1:8080' \
COORD='http://127.0.0.1:8081' \
VPS_BASE='http://hackme-vps:18080' \
bash scripts/ops/rc_freeze_gate.sh
```

Artifacts:

- `reports/gates/rc_freeze_*/results.jsonl`
- `reports/gates/rc_freeze_*/summary.json`

## 3) Optional Deep Security/Fuzz Gates

Run with extended checks enabled:

```bash
RUN_FUZZ_RELEASE_GATE=1 RUN_FUZZ_SUPER_GATE=1 \
ADMIN_TOKEN='<admin-token>' \
bash scripts/ops/rc_freeze_gate.sh
```

## 4) Acceptance Criteria

RC freeze is considered valid only if all are true:

- `rc_freeze_gate` summary status = `PASS`
- no failing checks in `results.jsonl`
- explorer/docs/legal pages deployed on domain and verified
- no unresolved critical incidents in current test window

## 5) If Gate Fails

- Treat any fail as release blocker.
- Fix root cause.
- Re-run `rc_freeze_gate` with a fresh `RUN_ID`.
- Keep failed artifact for audit trail.

## 6) Recommended Next Step After PASS

- Start language expansion branch (TinyGo first).
- Keep RC baseline branch protected and bugfix-only.

## 7) Nightly Automation

Run every night to keep constant release readiness signal:

```bash
ADMIN_TOKEN='<admin-token>' \
BASE='http://127.0.0.1:8080' \
COORD='http://127.0.0.1:8081' \
VPS_BASE='http://hackme-vps:18080' \
bash scripts/ops/rc_freeze_nightly.sh
```

Profiles:

- `PROFILE=practical` (default): optimized for single-VPS nightly stability.
- `PROFILE=strict`: enables stricter peer/readiness expectations.

Nightly artifacts:

- `reports/gates/rc_nightly_*/nightly_report.md`
- `reports/gates/rc_nightly_*/nightly_summary.json`

### Enable as systemd timer (recommended)

```bash
sudo bash scripts/ops/setup_rc_nightly_timer.sh
```

Manual trigger:

```bash
sudo systemctl start hackme-rc-freeze-nightly.service
```

### Cron fallback (if systemd timers are unavailable)

```bash
crontab -e
```

Example (daily at 02:30):

```cron
30 2 * * * cd /opt/hackme && ADMIN_TOKEN='<admin-token>' BASE='http://127.0.0.1:18080' COORD='http://127.0.0.1:18081' VPS_BASE='http://127.0.0.1:18080' bash scripts/ops/rc_freeze_nightly.sh >> /opt/hackme/logs/rc_nightly.log 2>&1
```
