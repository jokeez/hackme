# Public launch - verdicts and boundaries (for operator and AI)

> **Status 2026-07:** public pool and site **already live** (rc11s). Current slice: **[STATUS.md](STATUS.md)**. Below are the product boundaries and honest “yes/no” (audit trail).

The document records **what can already be stated without reservations**, **what remains only on the operation side** and **what is not in the code** (so as not to build false expectations). Source of truth for economic strata: **`docs/ECONOMICS_DASHBOARD.md`**, public page **`/economics-model.html`**.

**Threats to the pool (fake attempts, settlement, rewardAuto, state):** honest analysis of the code - **`docs/POOL_SECURITY_THREATS_VERDICT.md`**.

---

## 1. Verdict on the product “pool + worker”

| Statement | Verdict |
|-------------|---------|
| Worker-mode (coordinator + claim/submit, limits, hybrid signer) implemented | **Yes** - `cmd/coordinator/work.go`, `POST /api/worker/start`, workerpoh / bash loop |
| Canonical overlay for follower (height/economy from command node) | **Yes** - `HACKME_PUBLIC_AUTHORITY_BASE` / canon URL, see `pool.go` |
| The dashboard guides the miner to the worker, local HTTP PoH only on the command-node with the flag | **Yes** - policy `handleMiningStart` + UI |
| API node version in status = `1.0.0-pool` | **Yes** - constant `Version` in `main.go` |
| Pool as HA/multi-master | **Not in MVP** - one coordinator, manual failover |
| Full replay of SQLite accounts via P2P | **No** - wallet truth in public mode over **canonical HTTP** |

---

## 2. Verdict on “economics in three words”

| Layer | What fixes | Who “sees the money” first |
|------|----------------|----------------------------|
| **Chain (on-chain / SQLite command node)** | PoH block, emission within the rules | **Primary wallet** producing node (reward credit), not "automatically all GPU pool" |
| **Coordinator (off-chain accrual)** | Leases, accepted attempts, `payout_hmc` using env / auto formulas from `base_reward`/`target_mod` | **Recording of the worker in the database/coordinator memory** before settlement |
| **Settlement (operator)** | Transfers to on-chain addresses from savings | **Miner's wallet** after scripts and env (`settle_worker_payouts.sh`, etc.) |

**Verdict:** the phrase “the network does not know workers, they have 0 HMC in the chain before payment” is **correct in meaning**. Splitting “exactly 100 block coins minus 1% between three balls” is **illustration**, not a universal coordinator formula (there are attempts/batches, `rewardPerM`, found bonus, options `payoutFoundOnly`).

---

## 3. What the operator must do before the “public day”

Already submitted to **`docs/OPERATOR_FINAL_CHECKLIST.md`** and **`docs/POOL_FINAL_FREEZE.md`**. Briefly:

1. **Gates:** `predeploy_gate.sh`, `fuzz_release_gate.sh`, when changing the circuit - `private_stage_gate.sh`; for the food canon - consciously `run_canonical_fuzz_gate.sh`.
2. **Automatic repo slice:** `bash scripts/ops/public_release_readiness.sh` (short tests) or full `bash scripts/ops/repo_final_selfcheck.sh`.
3. **Infra:** TLS, nginx, backups `data/*.db` + coordinator database, settlement timers / healthcheck.
4. **Secrets:** coordinator tokens, admin, `.env` / `hackme.env` for miners (do not embed in exe).
5. **Communication:** news (`web/site/assets/news.json`), Downloads (zip + SHA256SUMS), if necessary Telegram (`docs/TELEGRAM_BOT.md`, channel bot separately).

---

## 4. With one command: quick verdict on the repository

```bash
bash scripts/ops/public_release_readiness.sh
```

With full tests as before merge:

```bash
bash scripts/ops/repo_final_selfcheck.sh
```

With polling of public command node (read-only):

```bash
PUBLIC_RO_BASE=https://hackme.tech bash scripts/ops/public_release_readiness.sh
```

Full merge package (as in CI before release): `gofmt`, `go vet`, **all** `go test ./...`, `go build ./...`, `code_quality_audit`, `bash -n` on `scripts/ops` and `scripts/lib`, main assembly, optional `PUBLIC_RO_BASE`:

```bash
PUBLIC_RO_BASE=https://hackme.tech bash scripts/ops/repo_final_selfcheck.sh
```

Last run of this package in the repository: **PASS** (including `PUBLIC_RO_BASE=https://hackme.tech`).

---

## 5. When AI or a person can say “everything is ready for public release”

Only if at the same time:

- passed the **automatic** checks from step 4 (or a full selfcheck);
- closed **operator** checklist item 3 for **specific** VPS/DNS;
- **artifacts** have been recorded (zip/tar version, SHA256, which is uploaded to Downloads).

Without this, the wording “100% ready” is **not a verdict**, but marketing.

---

## 6. Advertising and “what date to start”

AI **cannot** assign one calendar date without your fact: when binary and nginx are actually on production, how many days of soak, what is the coverage of the campaign.

**Separation:**

| Activity type | Start condition |
|----------------|----------------|
| **Quiet / technical announcement** (Discord, closed list, “we are in beta”) | After deployment of UI + binary for prod + green `PROFILE=canary` for `BASE=https://hackme.tech` + 2–3 days without incident. |
| **Mass advertising** (“top pool”, paid traffic, loud promises) | Not yet green `PROFILE=top` in `scripts/ops/top_pool_readiness_gate.sh` (currently the bottleneck is the **`MIN_WORKERS=10`** threshold with actual **2** active workers on the coordinator), or until you **consciously lower** the threshold in the script / reformulate the positioning as “early pool”. |
| **Legally/Financially Significant Promises** | Separate from the code: offer texts, off-chain accrual risks, country of audience - this is not derived from `go test`. |

**Practical guideline (not a dogma):** if the product deployment is completed **this week**, a reasonable minimum before **expensive** advertising is **7 calendar days** of a stable canary gate and settlement / payout monitoring. It would be dishonest to name the date “May X” without your deployment date: fix **D0 = day of green selfcheck on production**, then **mass start ≥ D0 + 7d** (or after passing `PROFILE=top`, if you maintain the current thresholds).
