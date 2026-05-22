# Final operator verdict — HackMe 0.1.0-rc11g — 2026-05-22

**Branch:** `cursor/iso-audit-build-02a1` · **Commit:** `587f1e2`  
**PR:** https://github.com/jokeez/hackme/pull/1  
**Canonical VPS:** `132.243.112.100` (`/opt/hackme`)  
**Sweep run:** `20260522T123922Z` (chaos) · `20260522T1239Z` (prod API)

---

## Executive verdict

| Verdict | Meaning |
|---------|---------|
| **SHIP** | Public pool, site, ISO, and release artifacts are consistent and safe to advertise for test-drive mining. |
| **CARE** | Strict hybrid signing is live — unsigned QA submits fail by design; do not interpret `coordinator_matrix` red rows as regressions. |
| **OPS** | Rotate SSH key if it was ever pasted in chat; keep `.secrets/` out of git. |

**Overall: READY** — ISO on VPS, pool ~2.43 GH/s, economics invariants OK, security smokes green.

---

## Test matrix (this sweep)

| Check | Result | Notes |
|-------|--------|-------|
| `go test ./...` | **PASS** | All packages (~10s) |
| `nightly_chaos_guard.sh` | **PASS** | 5000 payouts, crypto chaos, init-worker, security pack · `reports/tests/20260522T123922Z/` |
| `init_worker_test.sh` | **PASS** | incl. `zk_empty_ini` |
| `smoke_artifacts.sh` + `verify_artifacts.sh` | **PASS** | `dist/release_0.1.0-rc11g/` |
| ISO SHA256 (local + VPS) | **PASS** | `1b7bd70e381bb0d5aee82135fe01963d27d2af43ebfba95e02dec22aabe17658` |
| `redteam_surface_smoke.sh` (https://hackme.tech) | **PASS** | Unauth mutating routes rejected |
| `hybrid_signer_smoke.sh` (prod coordinator) | **PASS** | `REQUIRE_HYBRID=1` `REQUIRE_STRICT=1` |
| `check_invariants.sh` (prod) | **PASS** | `tip_height=24484`, economics hold |
| `coordinator_matrix.sh` (prod URL) | **EXPECTED** | 3/8 legacy expects fail: strict → `signature_required` (403); QA worker banned after probe |
| `coordinator_matrix.sh` (VPS loopback) | **EXPECTED** | `qa-worker-01` **429 banned** from this sweep — rate limiter OK |
| Pool claim canary (`vps-canary-01`) | **EXPECTED** | **429** `worker_temporarily_banned` after prior abuse |
| `hybrid_signer_smoke.sh` (no token, local) | **SKIP** | No admin token in agent workspace |
| `coordinator_matrix` / redteam (127.0.0.1) | **SKIP** | No local stack in cloud agent |
| Windows installer rebuild | **SKIP** | Inno/Docker not available on agent host |

---

## Production live (2026-05-22 ~12:41 UTC)

| Metric | Value |
|--------|--------|
| Chain tip | **24484** |
| Pool hashrate | **~2.43 GH/s** (`pool_hashrate_gh_s`) |
| Online workers | **3** (desktop ~2.06, vps-62 ~0.35, vps-msk ~0.02 GH/s) |
| `hybrid_signer_strict` | **true** |
| `payout_found_only` | **false** (fair lease payouts) |
| `target_mod` | **4,972,912** |
| `reward_per_m` | **~0.00201 HMC** / 1M attempts |
| Coordinator backpressure | `claim_rate_limited`, `worker_temporarily_banned`, `signature_required` in `drop_reason_count` |

---

## HTTP / deploy smoke

| URL | Code | Latency |
|-----|------|---------|
| https://hackme.tech/ | 200 | ~0.2s |
| https://hackme.tech/index.html | 200 | ~0.4s |
| https://hackme.tech/downloads.html | 200 | ~0.3s |
| https://hackme.tech/assets/app.js | 200 | ~0.4s |
| `/pool/api/global/metrics` | 200 | ~2.1s |
| `/pool/coordinator/api/work/stats` | 200 | ~0.3s |
| `/pool/api/metrics` | 200 | **~5.2s** (slow; **not** used by public dashboard) |
| ISO range GET | **206** | OK |
| VPS: nginx, coordinator, node, news-bot | **active** | |
| VPS ISO on disk | **956 MB** | SHA256 matches |

**Site fix (PR #1):** Pool Overview uses `global/metrics` + `work/stats` in parallel (~4.5s timeout), not `/pool/api/metrics`.

---

## HackMe OS ISO

| Field | Value |
|-------|--------|
| File | `HackMe-OS-0.1.0-rc11g-amd64.iso` |
| Size | **956 MB** |
| SHA256 | `1b7bd70e381bb0d5aee82135fe01963d27d2af43ebfba95e02dec22aabe17658` |
| Pool URL | `https://hackme.tech/pool/coordinator` |
| ZK Start | Empty `hackme.ini` → `minersign` + 24-word phrase |

---

## Communications (ready to paste)

| Channel | File |
|---------|------|
| Bitcointalk (BBCode) | `docs/BITCOINTALK_UPDATE_HACKME_OS_BBCode.txt` |
| Bitcointalk (MD) | `docs/BITCOINTALK_UPDATE_HACKME_OS.md` |
| Telegram | `docs/TELEGRAM_POST_HACKME_OS.md` |

---

## Known gaps (non-blocking)

1. **Windows installer** not rebuilt in cloud agent — existing zips on site unchanged.
2. **`coordinator_matrix`** should treat **403 `signature_required`** like 409 when `hybrid_signer_strict=true` (test harness debt, not pool bug).
3. **QA worker IDs** used in matrices may stay banned for ~120s after aggressive probes — use fresh `WORKER_ID` for green matrix runs.
4. **SSH key rotation** recommended if private key was shared in chat.

---

*Experimental RC — verify SHA256 and explorer payouts before farm scale.*
