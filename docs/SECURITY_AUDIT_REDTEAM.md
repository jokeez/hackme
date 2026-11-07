# Red-team security audit (pre–open source)

**Date:** 2026-05-18 (initial) · **Re-audit:** 2026-08-28 (pre-listing)  
**Scope:** node, coordinator, settlement, dashboard, public nginx  
**Verdict:** Safe to publish source **only with production hardening below**. Default dev configs are **not** safe on a public bind.

### Re-audit 2026-08-28 (pre-listing)

| Suite | Result | Notes |
|-------|--------|-------|
| `security_full_audit.sh` | **16/16 PASS** | Ephemeral node + public `hackme.tech/pool` probes |
| `redteam_hard_mode.sh` | **12/12 PASS** | Local node + ephemeral coordinator; RUN_ID `redteam_prelisting_20260828v4` |
| `pool_fuzz_escrow_redteam_gate.sh` | **PASS** | Hybrid signer enabled on ephemeral coordinator |
| Public `redteam_surface_smoke.sh` | **PASS** | All mutating routes reject unauth (403) |

Reports: `reports/tests/security_full_20260828T190633Z/` · `reports/gates/redteam_prelisting_20260828v4/`

### Hardening 2026-08-31 (external audit follow-up, pre-D0)

| Fix | Area |
|-----|------|
| HMS `GET …/health`, `…/chunks`, order detail, `GET /api/seal/payouts` require admin/worker token | `internal/hms/http.go` |
| HMS abuse `clientIP` ignores spoofed `X-Forwarded-For` unless trusted proxy peer | `internal/hms/clientip.go` |
| `GET /api/mining/logs/stream` requires admin token (or desktop loopback) | `main.go` |
| Public node bind requires `HACKME_P2P_TOKEN` | `main.go` startup |
| Settlement state writes take exclusive lock on `*.flock` (**Unix flock + Windows LockFileEx**; fail-closed) + atomic temp+rename | `settlement_flock_*.go`, `atomicWriteFile` in `settlement_api.go` |
| Hunt harness supply-chain (report #6): binary `harness_content_sha256` attestation + coordinator-only fetch | `internal/hunt/artifact.go`, claim payload, worker materialize |
| Fleet merge payout capture (report #17): address conflict stays sticky; settle scripts skip `address_conflict` | `mergeWorkerStat`, `settle_worker_payouts.sh`, `settle_worker_sup.sh` |
| Orphan multi-GPU fleet freeze bypass (2026-11-07): M4 only guarded bare `m.worker[base]`; CUDA fleets use only `-gpuN` — mismatching sibling no longer folds into empty/other destination | `shouldExportFleetMemberSeparately`, deterministic fold order, `publicWorkersMap` clash guard |
| Empty-address unpaid accruals must not adopt stranger payout on fold (post-unbind / legacy) | `shouldExportFleetMemberSeparately` + `mergeWorkerStat` refuse address adopt onto unpaid blank dst |
| HMS cancel vs upload/complete TOCTOU: status CAS + refuse resurrect after cancel | `CancelStorageOrder` / upload UPDATE / complete UPDATE `WHERE status NOT IN (cancelled,failed)` |
| Block height `index`/`q` SQLite high-bit bind → 400 (extends from_height/before fix) | `GetBlockSummaryByIndex` + reports handlers |
| Public settlement GET is read-only and keeps `pending_settle` (report #13) | `settlement_api.go` |
| Worker payout lock persists across restart and idle prune (report #18) | `worker_payout_lock` in coordinator SQLite |
| Release lane must not create payout locks; admin unbind recovers (report #27) | `checkReleaseMinerIdentity` + `/api/work/admin/unbind-payout-lock` |
| Submit worker rate slot charged only after signature / payout-lock (report #28) | `allowSubmitPeer` + `chargeSubmitWorker` |
| Pool `target_mod` clamps to M bounds only (Blake3 v3: any M solvable); chain still nudges off multiples of 7 for pre-v3 `7n+13`; stall easing without a prior found (report #29) | `clampTargetMod` / `ClampPoHTargetMod` / `maybeRetargetPoolMod` |
| Blake3 PoH v3: algebraic `7n+13` shortcut rejected post-activation; wrong `miner_address` / stale `prev_hash` / forged eval rejected on validate + pool found | `validatePoHSubmission` / `validFoundNoncePoH` / claim lease binding |
| SUP self-transfer rejected; applier credits via SQL increment and skips self-credit (report #30 mint) | `ValidateSupTransferShape` / `applyPendingSupTransfers` |
| Release must not charge claim buckets on forgeable ids | `allowClaimPeer` on `/api/fuzz/work/release` |
| Claim binds payout lock only after successful lease; failed claim does not squat | `bindClaimPayoutFromPub` after Claim/claim ok |
| PoH submit keeps lease on signature failures; peer IP only on accept | `submit()` + handler gate |
| Fuzz submit releases lease only when payout-lock identity proven | conditional `ReleaseWorkLease` |
| Claim charges worker slot only after successful lease | `allowClaimPeer` + `chargeClaimWorker` |
| Escrow split / hunt bounty shares use integer unit math | `ComputeSplitUnits` / `HuntBountyPayoutUnits` |
| ASAN bounty requires a non-zero exit and a canonical AddressSanitizer banner (report #14) | `internal/fuzzupstream/fuzz.go` |
| CI gate crash counts use the full finding history, not the display window (report #11) | `fullCrashClassSeverityCounts` |
| Campaign progress requires a pool token and does not close escrow; finalize waits for settle pull (report #12) | `fuzz_pool.go`, `fuzz_pool_progress.go` |
| WASM check timeout closes the interpreter on context deadline (report #15) | `WithCloseOnContextDone` |
| `govulncheck` in CI; `go 1.25.13` toolchain pin | `.github/workflows/ci.yml`, `go.mod` |

---

## Executive summary

| Area | Pre-audit risk | After patches in tree |
|------|----------------|------------------------|
| Coordinator claim/submit without token | **Critical** on public bind | **Fail-closed** unless `HACKME_COORDINATOR_ALLOW_INSECURE=1` |
| `GET /api/work/stats?details=1` leak | **High** | Requires coordinator token; node forwards token |
| Public `/api/work/stats` overshare (`source` loopback URL, `target_mod` / lease knobs) | **Medium** | Public lane allowlist-style strip + opaque `source=pool`; loopback coordinator URLs redacted on `/api/status`; share_mod remains on authenticated claim |
| `POST /api/tx/send` remote auto-sign | **High** | Requires admin token (except loopback + token) |
| Desktop `/api/desktop/local-auth` token leak | **High** (loopback/desktop) | API always returns token on loopback+desktop (UX); HTML embed still gated by `HACKME_DESKTOP_EXPOSE_ADMIN_TOKEN=1`. Rotate token on shared hosts. |
| Fake attempts payout | **High** (economic) | **Operator policy** — use `PAYOUT_FOUND_ONLY` + hybrid strict |
| Settlement synthetic row | **High** (ops) | Script refuses multi-key map when `workers{}` empty |

Automated smoke: `scripts/tests/security_assertions.sh`, `scripts/tests/redteam_surface_smoke.sh` — **PASS** on loopback node with admin token set.

---

## Critical findings (must fix in production)

### C1 — Open coordinator on the internet

**Attack:** `POST /api/work/claim` + `submit` with inflated `attempts` → accrues `payout_hmc` without GPU work.

**Mitigation:**
- Set `HACKME_COORDINATOR_ADMIN_TOKEN` (strong random).
- Bind coordinator to `127.0.0.1:18081`; expose only via nginx with token on worker paths.
- Never set `HACKME_COORDINATOR_ALLOW_INSECURE=1` on VPS.
- Public pool: `HACKME_COORDINATOR_PAYOUT_FOUND_ONLY=1`, `HACKME_POOL_HYBRID_SIGNER_STRICT=1`.

### C2 — Node without admin token on `0.0.0.0`

**Attack:** All `requireAdminAuth` routes open (genesis, mining, worker, fuzz).

**Mitigation:** Keep default `HACKME_REQUIRE_ADMIN_TOKEN=1` (node **fatals** if token missing).

### C3 — Settlement pays coordinator accrual without re-proof

**Attack:** Inflated coordinator stats → `settle_worker_payouts.sh` drains treasury.

**Mitigation:** Monitor `total_payout_hmc` vs chain; payout policy above; single settlement cron + `flock`; verify `workers{}` before pay.

---

## High findings

| ID | Issue | Mitigation |
|----|--------|------------|
| H1 | Attempt-based payout trusts reported `attempts` (capped by batch) | `PAYOUT_FOUND_ONLY=1` **or** sampled missed-found recheck (`ATTEMPT_RECHECK_PCT`, default 1%) + hybrid strict |
| H2 | Hybrid signer optional | `HYBRID_SIGNER_STRICT=1` |
| H3 | Cheap `found=true` via `PohEval(n)=n*7+13` | Found-only payout + monitoring |
| H4 | Coordinator state in RAM (restart clears dedup) | Treat restart as accrual reset; reconcile settlement state |
| H5 | `WORKER_PAYOUT_MAP` misconfiguration | Ops discipline; audit map before cron |
| H6 | P2P open if `HACKME_P2P_TOKEN` unset | Always set P2P token when P2P is exposed |
| H7 | `tasks/from_code` = compiler execution | Admin-only; disable on public followers |
| H8 | Hunt harness fetch RCE (no binary attestation; arbitrary HTTPS URL) | **Fixed 2026-09-22:** claim carries `harness_content_sha256`; worker verifies before cache write/exec; fetch restricted to configured coordinator host; unattested cache quarantined |

---

## Code hardening shipped in this tree

1. **Coordinator** — `HACKME_COORDINATOR_REQUIRE_ADMIN_TOKEN` (default on); fatal if token missing on non-loopback bind.
2. **Coordinator** — `GET /api/work/stats?details=1` requires admin token.
3. **Node** — `fetchCoordinatorWorkStats` sends coordinator token for `details=1`.
4. **Node** — `POST /api/tx/send` requires admin for non-loopback callers.
5. **Desktop** — admin token is **not** embedded in dashboard HTML unless `HACKME_DESKTOP_EXPOSE_ADMIN_TOKEN=1`. Loopback desktop `local-auth` JSON returns the token for UI re-sync (by design). Orders Linux/Windows starters default `EXPOSE=0`. Full-audit live node (non-desktop) expects HTTP 404 on local-auth; desktop contract is covered by `TestDesktopLocalAuth*` / `TestDesktopAdminTokenEmbed*`.
6. **Worker ID** — `[a-zA-Z0-9._-]{1,128}` on claim/submit.
7. **worker_loop.sh** — unsigned submit JSON built with `jq`, not string concat.
8. **Coordinator** — optional `HACKME_COORDINATOR_WORKER_TOKEN` for claim/submit only; admin token required for `clear-abuse` and `stats?details=1`.

---

## Production environment variables

See `scripts/ops/public_pool_hardening.env.example`.

```bash
# Coordinator (VPS)
HACKME_COORDINATOR_ADMIN_TOKEN=<strong-secret>
HACKME_COORDINATOR_ADDR=127.0.0.1:18081
HACKME_COORDINATOR_PAYOUT_FOUND_ONLY=1
HACKME_POOL_HYBRID_SIGNER_ENABLED=1
HACKME_POOL_HYBRID_SIGNER_STRICT=1

# Node (command)
HACKME_ADMIN_TOKEN=<strong-secret>
HACKME_REQUIRE_ADMIN_TOKEN=1
HACKME_P2P_TOKEN=<strong-secret>   # if P2P enabled

# Never on public pool
# HACKME_COORDINATOR_ALLOW_INSECURE=1
# HACKME_REQUIRE_ADMIN_TOKEN=0
```

---

## Residual accepted risks (document honestly)

- No per-nonce re-verification of full batch (cost).
- No coordinator persistence (restart semantics).
- Settlement JSON state is file-based, not DB-transactional (mitigated by `flock` on one host).
- Public read APIs (`/api/status`, `/api/work/stats` without details) remain for transparency.

---

## Related docs

- [SECURITY.md](SECURITY.md) — threat model  
- [POOL_SECURITY_THREATS_VERDICT.md](archive/verdicts/POOL_SECURITY_THREATS_VERDICT.md) — payout abuse scenarios  
