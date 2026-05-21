# HackMe testing guide

Automated coverage spans **Go unit tests**, **bash integration matrices**, **Playwright UI E2E**, and **mock miner load** scripts.

## Quick commands

| Goal | Command |
|------|---------|
| All Go tests | `go test ./... -count=1` |
| Coordinator payout + difficulty | `go test ./cmd/coordinator/... -run 'TestPayout\|TestPool\|TestMaybeRetarget' -count=1` |
| Chain retarget math | `go test ./internal/chain/... -run Retarget -count=1` |
| Bash smoke pack | `bash scripts/tests/run_test_pack.sh` |
| Coordinator API matrix | `COORD=http://127.0.0.1:8081 COORD_ADMIN_TOKEN=… bash scripts/tests/coordinator_matrix.sh` |
| Mock 15 workers load | `bash scripts/tests/mock_miners_load.sh` |
| Playwright dashboard E2E | `bash scripts/tests/run_ui_e2e.sh` |

## 1. UI / dashboard (Playwright)

Location: `tests/e2e/`

- Starts an isolated stack via `scripts/tests/e2e_stack.sh` (coordinator `:19081`, node `:19080`).
- Injects admin token into `sessionStorage` / `localStorage`.
- Covers: tab switch, **Refresh now**, **Stop mining**, Orders **Rust / Zig / C++** templates, **POST /api/tasks**, Fuzz **start** status, Hardware **Refresh** → `GET /api/hardware/tune`.

Technical strings (worker IDs, API paths, GPU names) use `notranslate` in `dashboard.html` so browser translators do not corrupt them.

```bash
bash scripts/tests/run_ui_e2e.sh
# headed debug:
cd tests/e2e && npx playwright test --headed
```

## 2. Difficulty adjustment (unit tests)

| Layer | File | What is tested |
|-------|------|----------------|
| Chain window retarget | `internal/chain/retarget_test.go` | Stable 30s windows keep `M`; fast blocks raise `M` |
| Pool hit retarget | `cmd/coordinator/work_test.go` | `maybeRetargetPoolMod` on fast solves |
| Pool load retarget | `cmd/coordinator/work_test.go` | Steady GH/s converges; miner surge raises `target_mod` |

Env knobs: `HACKME_COORDINATOR_POOL_RETARGET`, `HACKME_COORDINATOR_POOL_TARGET_MOD_{MIN,MAX}`.

## 3. Payout / shares (`payout_found_only=0`)

| Test | File |
|------|------|
| Attempt accrual when `payout_found_only=false` | `TestPayoutFoundOnlyDisabledAccruesAttempts` |
| No attempt pay when `payout_found_only=true` | `TestPayoutFoundOnlySkipsAttemptAccrual` |
| Worker ledger sum = `total_payout_hmc` | `TestPayoutSharesNoSystematicLoss` |

Formula (coordinator): `payout = (paidAttempts / 1e6) * reward_per_m` (+ found bonus if applicable).

Note: `submit()` returns `accepted == req.Found`; non-found shares still accrue payout when configured — check `payout` and stats, not only the first return value.

## 4. Mock miners load

`scripts/tests/tools/mock_miners_pool.py` — N threads claim/submit in parallel.

```bash
# coordinator must be running
COORD=http://127.0.0.1:8081 \
COORD_ADMIN_TOKEN="$(cat .secrets/hackme_coordinator_admin_token)" \
WORKERS=18 DURATION_SEC=60 \
bash scripts/tests/mock_miners_load.sh
```

Asserts: enough `claim_200` / `submit_200`, `workers_count` in stats, `accepted_attempts` grows.

## 5. CI

`.github/workflows/ci.yml` — `go test ./...` + language static.

Optional local full audit:

```bash
bash scripts/tests/run_full_audit.sh   # go test + mock miners (if coord up) + e2e if npm available
```

## NVML / GPU telemetry note

If `nvidia-smi` fails with **driver/library version mismatch**, hardware tune falls back to `/proc/driver/nvidia/gpus/*/information` so the GPU still appears in the UI (without live power/temp until NVML is fixed — usually reboot after driver update).
